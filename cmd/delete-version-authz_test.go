// Copyright (c) 2015-2026 MinIO, Inc.
// Copyright (c) 2026 PGSTY
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
)

func TestAPIDeleteObjectVersionAuthorization(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteObjectVersionAuthorization,
		endpoints:         []string{"DeleteObject"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPIDeleteObjectVersionAuthorization(obj ObjectLayer, instanceType, bucket string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T,
) {
	deleteOnly := newObjectAttributesAuthzUser(t, instanceType, bucket, `"s3:DeleteObject"`)
	versionOnly := newObjectAttributesAuthzUser(t, instanceType, bucket, `"s3:DeleteObjectVersion"`)
	payload := []byte("delete version authorization")

	put := func(t *testing.T, object string, versioned bool) string {
		t.Helper()
		info, err := obj.PutObject(t.Context(), bucket, object,
			mustGetPutObjReader(t, bytes.NewReader(payload), int64(len(payload)), "", ""), ObjectOptions{Versioned: versioned})
		if err != nil {
			t.Fatal(err)
		}
		if versioned && info.VersionID == "" {
			t.Fatalf("%s: versioned PUT returned an empty version ID", instanceType)
		}
		return info.VersionID
	}
	remove := func(t *testing.T, object, versionID string, creds auth.Credentials) *httptest.ResponseRecorder {
		t.Helper()
		target := getDeleteObjectURL("", bucket, object)
		if versionID != "" {
			target += "?" + url.Values{xhttp.VersionID: {versionID}}.Encode()
		}
		req, err := newTestSignedRequestV4(http.MethodDelete, target, 0, nil, creds.AccessKey, creds.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	t.Run("version permission deletes an explicit version", func(t *testing.T) {
		object := "delete-authz/version-only-explicit"
		versionID := put(t, object, true)
		if rec := remove(t, object, versionID, versionOnly); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); !isErrObjectNotFound(err) {
			t.Fatalf("explicit version still exists: %v", err)
		}
	})

	t.Run("version permission cannot create a delete marker", func(t *testing.T) {
		object := "delete-authz/version-only-simple"
		versionID := put(t, object, true)
		if rec := remove(t, object, "", versionOnly); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); err != nil {
			t.Fatalf("denied simple delete removed the version: %v", err)
		}
	})

	t.Run("object permission cannot delete an explicit version", func(t *testing.T) {
		object := "delete-authz/delete-only-explicit"
		versionID := put(t, object, true)
		if rec := remove(t, object, versionID, deleteOnly); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); err != nil {
			t.Fatalf("denied version delete removed the version: %v", err)
		}
	})

	t.Run("object permission creates a delete marker", func(t *testing.T) {
		object := "delete-authz/delete-only-simple"
		versionID := put(t, object, true)
		if rec := remove(t, object, "", deleteOnly); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); err != nil {
			t.Fatalf("simple delete removed the old version: %v", err)
		}
	})

	t.Run("null is an explicit version", func(t *testing.T) {
		object := "delete-authz/null-version"
		if versionID := put(t, object, false); versionID != "" {
			t.Fatalf("unversioned PUT returned version ID %q", versionID)
		}
		if rec := remove(t, object, nullVersionID, versionOnly); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: nullVersionID}); !isErrObjectNotFound(err) {
			t.Fatalf("null version still exists: %v", err)
		}
	})
}
