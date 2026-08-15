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
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/minio/pkg/v3/policy"
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
	_ auth.Credentials, t *testing.T,
) {
	versionIDs := make(map[string]string, 6)
	for _, objectName := range []string{"object-current", "object-version", "version-current", "version-exact", "version-denied"} {
		data := []byte(objectName)
		info, err := obj.PutObject(t.Context(), bucketName, objectName,
			mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatalf("%s: put %q: %v", instanceType, objectName, err)
		}
		versionIDs[objectName] = info.VersionID
	}
	markerData := []byte("delete-marker-exact")
	markerObject := "delete-marker-exact"
	markerBase, err := obj.PutObject(t.Context(), bucketName, markerObject,
		mustGetPutObjReader(t, bytes.NewReader(markerData), int64(len(markerData)), "", ""), ObjectOptions{Versioned: true})
	if err != nil {
		t.Fatalf("%s: put delete-marker base: %v", instanceType, err)
	}
	marker, err := obj.DeleteObject(t.Context(), bucketName, markerObject, ObjectOptions{Versioned: true})
	if err != nil {
		t.Fatalf("%s: create delete marker: %v", instanceType, err)
	}
	if !marker.DeleteMarker || marker.VersionID == "" {
		t.Fatalf("%s: delete did not create a versioned marker: %+v", instanceType, marker)
	}
	versionIDs[markerObject] = marker.VersionID
	nullData := []byte("null-exact")
	if _, err := obj.PutObject(t.Context(), bucketName, "null-exact",
		mustGetPutObjReader(t, bytes.NewReader(nullData), int64(len(nullData)), "", ""), ObjectOptions{}); err != nil {
		t.Fatalf("%s: put null version: %v", instanceType, err)
	}

	policyBytes := fmt.Appendf(nil, `{
		"Version":"2012-10-17",
		"Statement":[
			{"Effect":"Allow","Action":"s3:DeleteObject","Resource":["arn:aws:s3:::%[1]s/object-current","arn:aws:s3:::%[1]s/object-version"]},
			{"Effect":"Allow","Action":"s3:DeleteObjectVersion","Resource":["arn:aws:s3:::%[1]s/version-current","arn:aws:s3:::%[1]s/version-exact","arn:aws:s3:::%[1]s/version-denied","arn:aws:s3:::%[1]s/null-exact","arn:aws:s3:::%[1]s/delete-marker-exact"]},
			{"Effect":"Deny","Action":"s3:DeleteObjectVersion","Resource":"arn:aws:s3:::%[1]s/version-denied","Condition":{"StringEquals":{"s3:versionid":"%[2]s"}}}
		]
	}`, bucketName, versionIDs["version-denied"])
	identity := putDeleteIdentityPolicy(t, instanceType, policyBytes)

	deleteBody := encodeResponse(DeleteObjectsRequest{Objects: []ObjectToDelete{
		{ObjectV: ObjectV{ObjectName: "object-current"}},
		{ObjectV: ObjectV{ObjectName: "object-version", VersionID: versionIDs["object-version"]}},
		{ObjectV: ObjectV{ObjectName: "version-current"}},
		{ObjectV: ObjectV{ObjectName: "version-exact", VersionID: versionIDs["version-exact"]}},
		{ObjectV: ObjectV{ObjectName: "version-denied", VersionID: versionIDs["version-denied"]}},
		{ObjectV: ObjectV{ObjectName: "null-exact", VersionID: nullVersionID}},
		{ObjectV: ObjectV{ObjectName: markerObject, VersionID: versionIDs[markerObject]}},
	}})
	deleteReq, err := newTestSignedRequestV4(http.MethodPost, getDeleteMultipleObjectsURL("", bucketName),
		int64(len(deleteBody)), bytes.NewReader(deleteBody), identity.AccessKey, identity.SecretKey, nil)
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
	for _, objectName := range []string{"object-current", "version-exact", "null-exact", markerObject} {
		if _, ok := deleted[objectName]; !ok {
			t.Errorf("%s: expected %q to be deleted: %+v", instanceType, objectName, response.DeletedObjects)
		}
	}
	if len(deleted) != 4 {
		t.Errorf("%s: unexpected deleted objects: %+v", instanceType, response.DeletedObjects)
	}

	errorsByKey := make(map[string]DeleteError, len(response.Errors))
	for _, deleteErr := range response.Errors {
		errorsByKey[deleteErr.Key] = deleteErr
	}
	for _, objectName := range []string{"object-version", "version-current", "version-denied"} {
		if deleteErr, ok := errorsByKey[objectName]; !ok || deleteErr.Code != errorCodes[ErrAccessDenied].Code {
			t.Errorf("%s: %q did not return AccessDenied: %+v", instanceType, objectName, response.Errors)
		}
	}
	if len(errorsByKey) != 3 {
		t.Errorf("%s: unexpected delete errors: %+v", instanceType, response.Errors)
	}

	for _, objectName := range []string{"object-version", "version-current", "version-denied"} {
		versionID := versionIDs[objectName]
		if _, err = obj.GetObjectInfo(t.Context(), bucketName, objectName, ObjectOptions{VersionID: versionID}); err != nil {
			t.Errorf("%s: denied version %q of %q was not preserved: %v", instanceType, versionID, objectName, err)
		}
	}
	if _, err = obj.GetObjectInfo(t.Context(), bucketName, markerObject, ObjectOptions{VersionID: markerBase.VersionID}); err != nil {
		t.Errorf("%s: deleting marker %q also removed its underlying version %q: %v",
			instanceType, versionIDs[markerObject], markerBase.VersionID, err)
	}
	for objectName, versionID := range map[string]string{
		"version-exact": versionIDs["version-exact"],
		"null-exact":    nullVersionID,
		markerObject:    versionIDs[markerObject],
	} {
		if _, err = obj.GetObjectInfo(t.Context(), bucketName, objectName, ObjectOptions{VersionID: versionID}); !isErrVersionNotFound(err) {
			t.Errorf("%s: authorized version %q of %q still exists: %v", instanceType, versionID, objectName, err)
		}
	}
}

func putDeleteIdentityPolicy(t *testing.T, instanceType string, policyBytes []byte) auth.Credentials {
	t.Helper()
	accessKey, secretKey, err := auth.GenerateCredentials()
	if err != nil {
		t.Fatal(err)
	}
	credentials := auth.Credentials{AccessKey: accessKey, SecretKey: secretKey}
	if _, err = globalIAMSys.CreateUser(t.Context(), credentials.AccessKey, madmin.AddOrUpdateUserReq{
		SecretKey: credentials.SecretKey,
		Status:    madmin.AccountEnabled,
	}); err != nil {
		t.Fatalf("%s: create delete-policy user: %v", instanceType, err)
	}
	parsed, err := policy.ParseConfig(strings.NewReader(string(policyBytes)))
	if err != nil {
		t.Fatalf("%s: parse delete identity policy: %v", instanceType, err)
	}
	policyName := "delete-action-" + mustGetUUID()
	if _, err = globalIAMSys.SetPolicy(t.Context(), policyName, *parsed); err != nil {
		t.Fatalf("%s: install delete identity policy: %v", instanceType, err)
	}
	if _, err = globalIAMSys.PolicyDBSet(t.Context(), credentials.AccessKey, policyName, regUser, false); err != nil {
		t.Fatalf("%s: attach delete identity policy: %v", instanceType, err)
	}
	return credentials
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
