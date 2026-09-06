// Copyright (c) 2015-2025 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"context"
	"net/http"
	"testing"
)

// TestDeleteObjectConditional verifies that a conditional DeleteObject
// (If-Match, wired through opts.CheckPrecondFn) is evaluated atomically at the
// object layer: a non-matching ETag must fail with PreConditionFailed and leave
// the object intact, a matching ETag must delete it, and an If-Match against a
// missing object must return a not-found error rather than silently succeeding.
func TestDeleteObjectConditional(t *testing.T) {
	ctx := context.Background()

	obj, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(context.Background())
	defer removeRoots(fsDirs)

	bucket := "test-bucket"
	object := "test-object"

	if err = obj.MakeBucket(ctx, bucket, MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err = obj.PutObject(ctx, bucket, object,
		mustGetPutObjReader(t, bytes.NewReader([]byte("test-value")),
			int64(len("test-value")), "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	objInfo, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	existingETag := objInfo.ETag

	// If-Match with a wrong ETag must fail and preserve the object.
	t.Run("wrong-etag-precondition-failed", func(t *testing.T) {
		opts := ObjectOptions{
			HasIfMatch: true,
			CheckPrecondFn: func(oi ObjectInfo) bool {
				return !isETagEqual(oi.ETag, "wrong-etag")
			},
		}
		if _, err := obj.DeleteObject(ctx, bucket, object, opts); !isErrPreconditionFailed(err) {
			t.Errorf("expected PreConditionFailed, got: %v", err)
		}
		if _, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{}); err != nil {
			t.Errorf("object must still exist after a failed conditional delete, got: %v", err)
		}
	})

	// If-Match against a missing object must return a not-found error.
	t.Run("missing-object-not-found", func(t *testing.T) {
		opts := ObjectOptions{
			HasIfMatch: true,
			CheckPrecondFn: func(oi ObjectInfo) bool {
				return !isETagEqual(oi.ETag, existingETag)
			},
		}
		_, err := obj.DeleteObject(ctx, bucket, "does-not-exist", opts)
		if !isErrObjectNotFound(err) && !isErrVersionNotFound(err) {
			t.Errorf("expected ObjectNotFound/VersionNotFound, got: %v", err)
		}
	})

	// If-Match with the correct ETag must delete the object (run last).
	t.Run("correct-etag-succeeds", func(t *testing.T) {
		opts := ObjectOptions{
			HasIfMatch: true,
			CheckPrecondFn: func(oi ObjectInfo) bool {
				return !isETagEqual(oi.ETag, existingETag)
			},
		}
		if _, err := obj.DeleteObject(ctx, bucket, object, opts); err != nil {
			t.Errorf("expected a successful delete with matching ETag, got: %v", err)
		}
		if _, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{}); !isErrObjectNotFound(err) {
			t.Errorf("object must be removed after a matching conditional delete, got: %v", err)
		}
	})
}

// TestDeleteObjectConditionalWithReadQuorumFailure verifies that a conditional
// (If-Match) DeleteObject does NOT proceed when the object's current state
// cannot be read due to read-quorum loss: without a verified ETag the delete
// must fail rather than remove the object blindly.
func TestDeleteObjectConditionalWithReadQuorumFailure(t *testing.T) {
	ctx := context.Background()

	obj, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(context.Background())
	defer removeRoots(fsDirs)

	z := obj.(*erasureServerPools)
	xl := z.serverPools[0].sets[0]

	bucket := "test-bucket"
	object := "test-object"

	if err = obj.MakeBucket(ctx, bucket, MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err = obj.PutObject(ctx, bucket, object,
		mustGetPutObjReader(t, bytes.NewReader([]byte("test-value")),
			int64(len("test-value")), "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	objInfo, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	existingETag := objInfo.ETag

	// Simulate read-quorum loss by taking 8 of 16 disks offline (EC 8+8).
	erasureDisks := xl.getDisks()
	z.serverPools[0].erasureDisksMu.Lock()
	xl.getDisks = func() []StorageAPI {
		for i := range erasureDisks[:8] {
			erasureDisks[i] = nil
		}
		return erasureDisks
	}
	z.serverPools[0].erasureDisksMu.Unlock()

	// Even with the correct ETag we must not delete: the current state (hence the
	// ETag) cannot be verified under read-quorum loss.
	opts := ObjectOptions{
		HasIfMatch: true,
		CheckPrecondFn: func(oi ObjectInfo) bool {
			return !isETagEqual(oi.ETag, existingETag)
		},
	}
	if _, err := obj.DeleteObject(ctx, bucket, object, opts); err == nil {
		t.Error("expected an error for a conditional delete under read-quorum loss, got nil (object may have been deleted without ETag verification)")
	}
}

// TestDeleteObjectConditionalVersioned verifies conditional DeleteObject on a
// versioned bucket, where the precondition is evaluated at the server-pool layer
// against the version that will actually be removed:
//   - If-Match "*" when the latest version is a delete marker must fail (412),
//     because there is no live object to match.
//   - An explicit versionId If-Match is evaluated against the addressed version,
//     not the latest one (match deletes it, mismatch is refused).
//   - An If-Match against a missing version returns VersionNotFound, which the
//     handler maps to NoSuchVersion.
func TestDeleteObjectConditionalVersioned(t *testing.T) {
	ctx := context.Background()

	obj, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(context.Background())
	defer removeRoots(fsDirs)

	bucket := "test-bucket"

	if err = obj.MakeBucket(ctx, bucket, MakeBucketOptions{VersioningEnabled: true}); err != nil {
		t.Fatal(err)
	}
	versioned := globalBucketVersioningSys.PrefixEnabled(bucket, "any")
	if !versioned {
		t.Fatalf("expected versioning to be enabled on %q", bucket)
	}

	put := func(object, content string) ObjectInfo {
		oi, perr := obj.PutObject(ctx, bucket, object,
			mustGetPutObjReader(t, bytes.NewReader([]byte(content)), int64(len(content)), "", ""),
			ObjectOptions{Versioned: versioned})
		if perr != nil {
			t.Fatalf("put %q: %v", object, perr)
		}
		return oi
	}
	ifMatch := func(value string) CheckPreconditionFn {
		return func(oi ObjectInfo) bool {
			return deleteIfMatchPreconditionFailed(http.Header{}, value, oi)
		}
	}

	// If-Match "*" against a delete-marker-latest must fail with 412.
	t.Run("wildcard-on-delete-marker-latest", func(t *testing.T) {
		object := "dm-object"
		put(object, "v1")
		// Create a delete marker (unconditional), making the latest a delete marker.
		if _, derr := obj.DeleteObject(ctx, bucket, object, ObjectOptions{Versioned: versioned}); derr != nil {
			t.Fatalf("create delete marker: %v", derr)
		}
		opts := ObjectOptions{Versioned: versioned, HasIfMatch: true, CheckPrecondFn: ifMatch("*")}
		if _, derr := obj.DeleteObject(ctx, bucket, object, opts); !isErrPreconditionFailed(derr) {
			t.Errorf("expected PreConditionFailed for If-Match:* on a delete-marker-latest, got: %v", derr)
		}
	})

	// Explicit versionId is evaluated against the addressed (older) version.
	t.Run("explicit-version-selection", func(t *testing.T) {
		object := "ver-object"
		v1 := put(object, "first")
		v2 := put(object, "second-longer") // v2 is now the latest with a different ETag
		if v1.ETag == v2.ETag {
			t.Fatalf("test setup: versions must have distinct ETags")
		}

		// Mismatch: delete v2 with v1's ETag must be refused, v2 preserved.
		mismatch := ObjectOptions{Versioned: versioned, VersionID: v2.VersionID, HasIfMatch: true, CheckPrecondFn: ifMatch(v1.ETag)}
		if _, derr := obj.DeleteObject(ctx, bucket, object, mismatch); !isErrPreconditionFailed(derr) {
			t.Errorf("expected PreConditionFailed deleting v2 with v1 ETag, got: %v", derr)
		}
		if _, gerr := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: v2.VersionID}); gerr != nil {
			t.Errorf("v2 must still exist after a refused conditional delete, got: %v", gerr)
		}

		// Match: delete v1 with v1's ETag must succeed even though v1 is not latest.
		match := ObjectOptions{Versioned: versioned, VersionID: v1.VersionID, HasIfMatch: true, CheckPrecondFn: ifMatch(v1.ETag)}
		if _, derr := obj.DeleteObject(ctx, bucket, object, match); derr != nil {
			t.Errorf("expected the addressed version to be deleted, got: %v", derr)
		}
		if _, gerr := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: v1.VersionID}); !isErrVersionNotFound(gerr) {
			t.Errorf("v1 must be gone after a matching conditional delete, got: %v", gerr)
		}
		if _, gerr := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: v2.VersionID}); gerr != nil {
			t.Errorf("v2 must remain after deleting v1, got: %v", gerr)
		}
	})

	// If-Match against a missing version on an EXISTING key returns VersionNotFound.
	t.Run("missing-version", func(t *testing.T) {
		object := "missing-version-object"
		put(object, "only")
		opts := ObjectOptions{Versioned: versioned, VersionID: mustGetUUID(), HasIfMatch: true, CheckPrecondFn: ifMatch("anything")}
		if _, derr := obj.DeleteObject(ctx, bucket, object, opts); !isErrVersionNotFound(derr) {
			t.Errorf("expected VersionNotFound for If-Match on a missing version, got: %v", derr)
		}
	})

	// If-Match against a missing version on an ABSENT key must also return
	// VersionNotFound (NoSuchVersion), not NoSuchKey: the request addresses a
	// specific version, which does not exist regardless of the key.
	t.Run("missing-version-absent-key", func(t *testing.T) {
		opts := ObjectOptions{Versioned: versioned, VersionID: mustGetUUID(), HasIfMatch: true, CheckPrecondFn: ifMatch("anything")}
		if _, derr := obj.DeleteObject(ctx, bucket, "never-existed", opts); !isErrVersionNotFound(derr) {
			t.Errorf("expected VersionNotFound for If-Match on a version of an absent key, got: %v", derr)
		}
	})

	// If-Match "*" addressing a delete-marker VERSION by id must fail with 412,
	// not 405: a delete marker has no entity-tag to match. getObjectInfo returns
	// the marker alongside MethodNotAllowed; the precondition runs on the marker.
	t.Run("wildcard-on-explicit-delete-marker-version", func(t *testing.T) {
		object := "explicit-dm-object"
		put(object, "live")
		dm, derr := obj.DeleteObject(ctx, bucket, object, ObjectOptions{Versioned: versioned})
		if derr != nil {
			t.Fatalf("create delete marker: %v", derr)
		}
		if !dm.DeleteMarker || dm.VersionID == "" {
			t.Fatalf("expected a delete-marker version, got DeleteMarker=%v VersionID=%q", dm.DeleteMarker, dm.VersionID)
		}
		opts := ObjectOptions{Versioned: versioned, VersionID: dm.VersionID, HasIfMatch: true, CheckPrecondFn: ifMatch("*")}
		if _, derr := obj.DeleteObject(ctx, bucket, object, opts); !isErrPreconditionFailed(derr) {
			t.Errorf("expected PreConditionFailed for If-Match:* on an addressed delete-marker version, got: %v", derr)
		}
	})
}
