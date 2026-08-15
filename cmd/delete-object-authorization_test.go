// Copyright (c) 2015-2026 MinIO, Inc.
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
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
)

func TestAPIDeleteObjectAuthorizationAction(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteObjectAuthorizationAction,
		endpoints:         []string{"DeleteObject", "PutBucketPolicy"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPIDeleteObjectAuthorizationAction(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, t *testing.T,
) {
	putVersion := func(objectName string, versioned bool) string {
		t.Helper()
		data := []byte(objectName)
		info, err := obj.PutObject(t.Context(), bucketName, objectName,
			mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{Versioned: versioned})
		if err != nil {
			t.Fatalf("%s: put %q: %v", instanceType, objectName, err)
		}
		return info.VersionID
	}

	versionOnlyID := putVersion("version-only", true)
	objectOnlyID := putVersion("object-only", true)
	nullID := putVersion("null-version", false)
	deniedVersionID := putVersion("deny-version", true)
	if versionOnlyID == "" || objectOnlyID == "" || deniedVersionID == "" || nullID != "" {
		t.Fatalf("%s: unexpected version IDs: version=%q object=%q null=%q deny=%q",
			instanceType, versionOnlyID, objectOnlyID, nullID, deniedVersionID)
	}

	policyBytes := fmt.Appendf(nil, `{
		"Version":"2012-10-17",
		"Statement":[
			{"Effect":"Allow","Principal":"*","Action":"s3:DeleteObjectVersion","Resource":["arn:aws:s3:::%[1]s/version-only","arn:aws:s3:::%[1]s/null-version","arn:aws:s3:::%[1]s/deny-version"]},
			{"Effect":"Allow","Principal":"*","Action":"s3:DeleteObject","Resource":"arn:aws:s3:::%[1]s/object-only"},
			{"Effect":"Deny","Principal":"*","Action":"s3:DeleteObjectVersion","Resource":"arn:aws:s3:::%[1]s/deny-version","Condition":{"StringEquals":{"s3:versionid":"%[2]s"}}}
		]
	}`, bucketName, deniedVersionID)
	putAnonymousDeletePolicy(t, instanceType, bucketName, apiRouter, credentials, policyBytes)

	deleteObject := func(objectName, versionID string) *httptest.ResponseRecorder {
		t.Helper()
		values := url.Values{}
		if versionID != "" {
			values.Set(xhttp.VersionID, versionID)
		}
		req, err := newTestRequest(http.MethodDelete, makeTestTargetURL("", bucketName, objectName, values), 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	if rec := deleteObject("version-only", ""); rec.Code != http.StatusForbidden {
		t.Errorf("%s: version-only policy authorized an unversioned delete: %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if rec := deleteObject("object-only", objectOnlyID); rec.Code != http.StatusForbidden {
		t.Errorf("%s: object-only policy authorized explicit version deletion: %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if rec := deleteObject("deny-version", deniedVersionID); rec.Code != http.StatusForbidden {
		t.Errorf("%s: explicit version deny did not take precedence: %d %s", instanceType, rec.Code, rec.Body.String())
	}
	for objectName, versionID := range map[string]string{
		"version-only": versionOnlyID,
		"object-only":  objectOnlyID,
		"deny-version": deniedVersionID,
	} {
		if _, err := obj.GetObjectInfo(t.Context(), bucketName, objectName, ObjectOptions{VersionID: versionID}); err != nil {
			t.Errorf("%s: denied version %q of %q was not preserved: %v", instanceType, versionID, objectName, err)
		}
	}

	if rec := deleteObject("version-only", versionOnlyID); rec.Code != http.StatusNoContent {
		t.Errorf("%s: DeleteObjectVersion did not authorize exact version deletion: %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if rec := deleteObject("object-only", ""); rec.Code != http.StatusNoContent {
		t.Errorf("%s: DeleteObject did not authorize unversioned deletion: %d %s", instanceType, rec.Code, rec.Body.String())
	}
	if rec := deleteObject("null-version", nullVersionID); rec.Code != http.StatusNoContent {
		t.Errorf("%s: DeleteObjectVersion did not authorize explicit null-version deletion: %d %s", instanceType, rec.Code, rec.Body.String())
	}
}

func TestAPIDeleteMultipleObjectsAuthorizationAction(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteMultipleObjectsAuthorizationAction,
		endpoints:         []string{"DeleteMultipleObjects", "PutBucketPolicy"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPIDeleteMultipleObjectsAuthorizationAction(obj ObjectLayer, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, t *testing.T,
) {
	versionIDs := make(map[string]string, 4)
	for _, objectName := range []string{"object-current", "object-version", "version-current", "version-exact"} {
		data := []byte(objectName)
		info, err := obj.PutObject(t.Context(), bucketName, objectName,
			mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatalf("%s: put %q: %v", instanceType, objectName, err)
		}
		versionIDs[objectName] = info.VersionID
	}
	nullData := []byte("null-exact")
	if _, err := obj.PutObject(t.Context(), bucketName, "null-exact",
		mustGetPutObjReader(t, bytes.NewReader(nullData), int64(len(nullData)), "", ""), ObjectOptions{}); err != nil {
		t.Fatalf("%s: put null version: %v", instanceType, err)
	}

	policyBytes := fmt.Appendf(nil, `{
		"Version":"2012-10-17",
		"Statement":[
			{"Effect":"Allow","Principal":"*","Action":"s3:DeleteObject","Resource":["arn:aws:s3:::%[1]s/object-current","arn:aws:s3:::%[1]s/object-version"]},
			{"Effect":"Allow","Principal":"*","Action":"s3:DeleteObjectVersion","Resource":["arn:aws:s3:::%[1]s/version-current","arn:aws:s3:::%[1]s/version-exact","arn:aws:s3:::%[1]s/null-exact"]}
		]
	}`, bucketName)
	putAnonymousDeletePolicy(t, instanceType, bucketName, apiRouter, credentials, policyBytes)

	deleteBody := encodeResponse(DeleteObjectsRequest{Objects: []ObjectToDelete{
		{ObjectV: ObjectV{ObjectName: "object-current"}},
		{ObjectV: ObjectV{ObjectName: "object-version", VersionID: versionIDs["object-version"]}},
		{ObjectV: ObjectV{ObjectName: "version-current"}},
		{ObjectV: ObjectV{ObjectName: "version-exact", VersionID: versionIDs["version-exact"]}},
		{ObjectV: ObjectV{ObjectName: "null-exact", VersionID: nullVersionID}},
	}})
	deleteReq, err := newTestRequest(http.MethodPost, getDeleteMultipleObjectsURL("", bucketName),
		int64(len(deleteBody)), bytes.NewReader(deleteBody))
	if err != nil {
		t.Fatal(err)
	}
	deleteRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("%s: delete returned %d: %s", instanceType, deleteRec.Code, deleteRec.Body.String())
	}

	var response DeleteObjectsResponse
	if err = xml.Unmarshal(deleteRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("%s: decode response: %v: %s", instanceType, err, deleteRec.Body.String())
	}
	deleted := make(map[string]DeletedObject, len(response.DeletedObjects))
	for _, object := range response.DeletedObjects {
		deleted[object.ObjectName] = object
	}
	for _, objectName := range []string{"object-current", "version-exact", "null-exact"} {
		if _, ok := deleted[objectName]; !ok {
			t.Errorf("%s: expected %q to be deleted: %+v", instanceType, objectName, response.DeletedObjects)
		}
	}
	if len(deleted) != 3 {
		t.Errorf("%s: unexpected deleted objects: %+v", instanceType, response.DeletedObjects)
	}

	errorsByKey := make(map[string]DeleteError, len(response.Errors))
	for _, deleteErr := range response.Errors {
		errorsByKey[deleteErr.Key] = deleteErr
	}
	for _, objectName := range []string{"object-version", "version-current"} {
		if deleteErr, ok := errorsByKey[objectName]; !ok || deleteErr.Code != errorCodes[ErrAccessDenied].Code {
			t.Errorf("%s: %q did not return AccessDenied: %+v", instanceType, objectName, response.Errors)
		}
	}
	if len(errorsByKey) != 2 {
		t.Errorf("%s: unexpected delete errors: %+v", instanceType, response.Errors)
	}
}

func putAnonymousDeletePolicy(t *testing.T, instanceType, bucketName string, apiRouter http.Handler,
	credentials auth.Credentials, policyBytes []byte,
) {
	t.Helper()
	policyReq, err := newTestSignedRequestV4(http.MethodPut, getPutPolicyURL("", bucketName), int64(len(policyBytes)),
		bytes.NewReader(policyBytes), credentials.AccessKey, credentials.SecretKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	policyRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(policyRec, policyReq)
	if policyRec.Code != http.StatusNoContent {
		t.Fatalf("%s: put policy returned %d: %s", instanceType, policyRec.Code, policyRec.Body.String())
	}
}
