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
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dustin/go-humanize"
	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
)

// TestAPIDeleteObjectHandlerIfMatch verifies conditional DeleteObject behavior
// for the If-Match request header (AWS S3 conditional deletes):
//   - a non-matching ETag must return 412 Precondition Failed and preserve the object,
//   - a matching ETag (or "*") must delete the object and return 204,
//   - a request without If-Match must be unaffected,
//   - If-Match against a missing key must return 404 NoSuchKey (not the idempotent 204).
func TestAPIDeleteObjectHandlerIfMatch(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{t: t, objAPITest: testAPIDeleteObjectHandlerIfMatch, endpoints: []string{"DeleteObject"}})
}

func testAPIDeleteObjectHandlerIfMatch(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, t *testing.T,
) {
	// putObject (re)creates an object and returns its ETag.
	putObject := func(object string) string {
		data := generateBytesData(1 * humanize.MiByte)
		oi, err := obj.PutObject(context.Background(), bucketName, object,
			mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{})
		if err != nil {
			t.Fatalf("%s: failed to put object %q: %v", instanceType, object, err)
		}
		return oi.ETag
	}

	// exists reports whether the object is still present.
	exists := func(object string) bool {
		_, err := obj.GetObjectInfo(context.Background(), bucketName, object, ObjectOptions{})
		return err == nil
	}

	// doDelete issues a signed DELETE, optionally with an If-Match header.
	doDelete := func(object, ifMatch string) *httptest.ResponseRecorder {
		var hdrs map[string]string
		if ifMatch != "" {
			hdrs = map[string]string{xhttp.IfMatch: ifMatch}
		}
		req, err := newTestSignedRequestV4(http.MethodDelete, getDeleteObjectURL("", bucketName, object),
			0, nil, credentials.AccessKey, credentials.SecretKey, hdrs)
		if err != nil {
			t.Fatalf("%s: failed to create DELETE request: %v", instanceType, err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	// (1) If-Match with a wrong ETag: 412 Precondition Failed, object preserved.
	t.Run("wrong-etag-412", func(t *testing.T) {
		object := "precond-wrong-etag"
		putObject(object)
		rec := doDelete(object, `"non-matching-etag"`)
		if rec.Code != http.StatusPreconditionFailed {
			t.Fatalf("%s: expected %d, got %d (body: %s)", instanceType, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
		}
		if !exists(object) {
			t.Fatalf("%s: object must still exist after a failed conditional delete", instanceType)
		}
	})

	// (2) If-Match with the correct ETag: 204 No Content, object removed.
	t.Run("correct-etag-204", func(t *testing.T) {
		object := "precond-correct-etag"
		etag := putObject(object)
		rec := doDelete(object, `"`+etag+`"`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: expected %d, got %d (body: %s)", instanceType, http.StatusNoContent, rec.Code, rec.Body.String())
		}
		if exists(object) {
			t.Fatalf("%s: object must be removed after a matching conditional delete", instanceType)
		}
	})

	// (3) If-Match "*" on an existing object matches any ETag: 204, object removed.
	t.Run("wildcard-204", func(t *testing.T) {
		object := "precond-wildcard"
		putObject(object)
		rec := doDelete(object, "*")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: expected %d, got %d (body: %s)", instanceType, http.StatusNoContent, rec.Code, rec.Body.String())
		}
		if exists(object) {
			t.Fatalf("%s: object must be removed after a wildcard conditional delete", instanceType)
		}
	})

	// (4) No If-Match header: unchanged behavior, 204, object removed.
	t.Run("no-ifmatch-204", func(t *testing.T) {
		object := "precond-none"
		putObject(object)
		rec := doDelete(object, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: expected %d, got %d (body: %s)", instanceType, http.StatusNoContent, rec.Code, rec.Body.String())
		}
		if exists(object) {
			t.Fatalf("%s: object must be removed after an unconditional delete", instanceType)
		}
	})

	// (5) If-Match against a non-existent key: 404 NoSuchKey, not the idempotent 204.
	t.Run("missing-key-404", func(t *testing.T) {
		rec := doDelete("precond-missing-key", `"some-etag"`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected %d, got %d (body: %s)", instanceType, http.StatusNotFound, rec.Code, rec.Body.String())
		}
		var apiErr APIErrorResponse
		if err := xml.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
			t.Fatalf("%s: failed to parse error response: %v", instanceType, err)
		}
		if apiErr.Code != "NoSuchKey" {
			t.Fatalf("%s: expected error code NoSuchKey, got %q", instanceType, apiErr.Code)
		}
	})
}
