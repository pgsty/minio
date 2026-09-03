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
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/pgsty/silo-pkg/v3/policy"
)

func TestDeleteObjectAction(t *testing.T) {
	for _, test := range []struct {
		versionID string
		want      policy.Action
	}{
		{want: policy.DeleteObjectAction},
		{versionID: nullVersionID, want: policy.DeleteObjectVersionAction},
		{versionID: mustGetUUID(), want: policy.DeleteObjectVersionAction},
		{versionID: " ", want: policy.DeleteObjectVersionAction},
	} {
		if got := deleteObjectAction(test.versionID); got != test.want {
			t.Errorf("deleteObjectAction(%q) = %s, want %s", test.versionID, got, test.want)
		}
	}
}

func TestAPIDeleteObjectVersionAuthorization(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteObjectVersionAuthorization,
		endpoints:         []string{"DeleteObject"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func TestAPIDeleteMultipleObjectsVersionAuthorization(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteMultipleObjectsVersionAuthorization,
		endpoints:         []string{"DeleteMultipleObjects"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPIDeleteMultipleObjectsVersionAuthorization(obj ObjectLayer, instanceType, bucket string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T,
) {
	deleteOnly := newObjectAttributesAuthzUser(t, instanceType, bucket, `"s3:DeleteObject"`)
	versionOnly := newObjectAttributesAuthzUser(t, instanceType, bucket, `"s3:DeleteObjectVersion"`)
	payload := []byte("multi delete version authorization")

	put := func(t *testing.T, object string, versioned bool) string {
		t.Helper()
		info, err := obj.PutObject(t.Context(), bucket, object,
			mustGetPutObjReader(t, bytes.NewReader(payload), int64(len(payload)), "", ""), ObjectOptions{Versioned: versioned})
		if err != nil {
			t.Fatal(err)
		}
		return info.VersionID
	}
	request := func(t *testing.T, prefix string, creds auth.Credentials) (DeleteObjectsResponse, map[string]string) {
		t.Helper()
		versions := map[string]string{
			prefix + "simple":   put(t, prefix+"simple", true),
			prefix + "explicit": put(t, prefix+"explicit", true),
			prefix + "null":     put(t, prefix+"null", false),
		}
		body := encodeResponse(DeleteObjectsRequest{Objects: []ObjectToDelete{
			{ObjectV: ObjectV{ObjectName: prefix + "simple"}},
			{ObjectV: ObjectV{ObjectName: prefix + "explicit", VersionID: versions[prefix+"explicit"]}},
			{ObjectV: ObjectV{ObjectName: prefix + "null", VersionID: nullVersionID}},
			{ObjectV: ObjectV{ObjectName: prefix + "bad", VersionID: "not-a-uuid"}},
		}})
		target := getDeleteMultipleObjectsURL("", bucket) + "&versionId=query-level-decoy"
		req, err := newTestSignedRequestV4(http.MethodPost, target, int64(len(body)), bytes.NewReader(body),
			creds.AccessKey, creds.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: multi-delete status %d: %s", instanceType, rec.Code, rec.Body.String())
		}
		var response DeleteObjectsResponse
		if err = xml.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v: %s", err, rec.Body.String())
		}
		return response, versions
	}
	responseMap := func(response DeleteObjectsResponse) (map[string]DeletedObject, map[string]DeleteError) {
		deleted := make(map[string]DeletedObject, len(response.DeletedObjects))
		for _, object := range response.DeletedObjects {
			deleted[object.ObjectName] = object
		}
		errs := make(map[string]DeleteError, len(response.Errors))
		for _, deleteErr := range response.Errors {
			errs[deleteErr.Key] = deleteErr
		}
		return deleted, errs
	}

	t.Run("DeleteObject only", func(t *testing.T) {
		prefix := "multi-delete-only/"
		response, versions := request(t, prefix, deleteOnly)
		deleted, errs := responseMap(response)
		if object, ok := deleted[prefix+"simple"]; !ok || !object.DeleteMarker {
			t.Fatalf("simple delete did not create a marker: %+v", response)
		}
		for _, object := range []string{"explicit", "null", "bad"} {
			if got := errs[prefix+object].Code; got != errorCodes[ErrAccessDenied].Code {
				t.Errorf("%s error = %q, want AccessDenied", object, got)
			}
		}
		if _, err := obj.GetObjectInfo(t.Context(), bucket, prefix+"explicit", ObjectOptions{VersionID: versions[prefix+"explicit"]}); err != nil {
			t.Fatalf("denied explicit delete removed its version: %v", err)
		}
	})

	t.Run("DeleteObjectVersion only", func(t *testing.T) {
		prefix := "multi-version-only/"
		response, _ := request(t, prefix, versionOnly)
		deleted, errs := responseMap(response)
		for _, object := range []string{"explicit", "null"} {
			if _, ok := deleted[prefix+object]; !ok {
				t.Errorf("%s was not deleted: %+v", object, response)
			}
		}
		if got := errs[prefix+"simple"].Code; got != errorCodes[ErrAccessDenied].Code {
			t.Errorf("simple error = %q, want AccessDenied", got)
		}
		if got := errs[prefix+"bad"].Code; got != errorCodes[ErrNoSuchVersion].Code {
			t.Errorf("bad UUID error = %q, want NoSuchVersion", got)
		}
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
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); !isErrObjectNotFound(err) && !isErrVersionNotFound(err) {
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
		if _, err := obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: nullVersionID}); !isErrObjectNotFound(err) && !isErrVersionNotFound(err) {
			t.Fatalf("null version still exists: %v", err)
		}
	})

	t.Run("authorization precedes invalid version parsing", func(t *testing.T) {
		object := "delete-authz/invalid-version"
		if rec := remove(t, object, "not-a-uuid", deleteOnly); rec.Code != http.StatusForbidden {
			t.Fatalf("delete-only status %d, want 403: %s", rec.Code, rec.Body.String())
		}
		if rec := remove(t, object, "not-a-uuid", versionOnly); rec.Code != http.StatusBadRequest {
			t.Fatalf("version-only status %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("padded version uses the effective ID", func(t *testing.T) {
		object := "delete-authz/padded-version"
		versionID := put(t, object, true)
		if rec := remove(t, object, versionID+" ", versionOnly); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestAPIDeleteObjectVersionDenyAndReplicationCompatibility(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPIDeleteObjectVersionDenyAndReplicationCompatibility,
		endpoints:         []string{"DeleteObject"},
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPIDeleteObjectVersionDenyAndReplicationCompatibility(obj ObjectLayer, instanceType, bucket string,
	apiRouter http.Handler, _ auth.Credentials, t *testing.T,
) {
	payload := []byte("delete version deny compatibility")
	put := func(t *testing.T, object string) string {
		t.Helper()
		info, err := obj.PutObject(t.Context(), bucket, object,
			mustGetPutObjReader(t, bytes.NewReader(payload), int64(len(payload)), "", ""), ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatal(err)
		}
		return info.VersionID
	}
	request := func(t *testing.T, object, versionID string, creds auth.Credentials, replicationRequest bool) *httptest.ResponseRecorder {
		t.Helper()
		target := getDeleteObjectURL("", bucket, object)
		if versionID != "" {
			target += "?" + url.Values{xhttp.VersionID: {versionID}}.Encode()
		}
		var headers map[string]string
		if replicationRequest {
			headers = map[string]string{
				xhttp.MinIOSourceReplicationRequest: "true",
				xhttp.AmzBucketReplicationStatus:    "REPLICA",
				xhttp.MinIOSourceDeleteMarker:       "false",
				xhttp.MinIOSourceMTime:              UTCNow().Format(time.RFC3339Nano),
			}
		}
		req, err := newTestSignedRequestV4(http.MethodDelete, target, 0, nil, creds.AccessKey, creds.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	minimal := newDeleteAuthzPolicyUser(t, instanceType, bucket, `[
		{"Effect":"Allow","Action":["s3:DeleteObject","s3:ReplicateDelete"],"Resource":["arn:aws:s3:::`+bucket+`/*"]}
	]`)
	denied := newDeleteAuthzPolicyUser(t, instanceType, bucket, `[
		{"Effect":"Allow","Action":["s3:DeleteObject","s3:DeleteObjectVersion","s3:ReplicateDelete"],"Resource":["arn:aws:s3:::`+bucket+`/*"]},
		{"Effect":"Deny","Action":"s3:DeleteObjectVersion","Resource":"arn:aws:s3:::`+bucket+`/deny/*"}
	]`)
	deleteOnly := newObjectAttributesAuthzUser(t, instanceType, bucket, `"s3:DeleteObject"`)

	t.Run("ordinary explicit deny wins", func(t *testing.T) {
		object := "deny/ordinary"
		versionID := put(t, object)
		if rec := request(t, object, versionID, denied, false); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("version deny does not block a simple delete", func(t *testing.T) {
		object := "deny/simple"
		put(t, object)
		if rec := request(t, object, "", denied, false); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("replication keeps minimal target policy", func(t *testing.T) {
		object := "replication/minimal"
		versionID := put(t, object)
		if rec := request(t, object, versionID, minimal, true); rec.Code != http.StatusNoContent {
			t.Fatalf("status %d, want 204: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("replication preserves explicit version deny", func(t *testing.T) {
		object := "deny/replication"
		versionID := put(t, object)
		if rec := request(t, object, versionID, denied, true); rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("marker alone cannot enter the replication path", func(t *testing.T) {
		object := "replication/fake-marker"
		versionID := put(t, object)
		target := getDeleteObjectURL("", bucket, object) + "?" + url.Values{xhttp.VersionID: {versionID}}.Encode()
		req, err := newTestSignedRequestV4(http.MethodDelete, target, 0, nil, deleteOnly.AccessKey, deleteOnly.SecretKey,
			map[string]string{xhttp.MinIOSourceReplicationRequest: "true"})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
		}
		if _, err = obj.GetObjectInfo(t.Context(), bucket, object, ObjectOptions{VersionID: versionID}); err != nil {
			t.Fatalf("fake marker removed the version: %v", err)
		}
	})
}

func newDeleteAuthzPolicyUser(t *testing.T, instanceType, bucket, statements string) auth.Credentials {
	t.Helper()
	accessKey, secretKey, err := auth.GenerateCredentials()
	if err != nil {
		t.Fatalf("%s: generate credentials: %v", instanceType, err)
	}
	creds := auth.Credentials{AccessKey: accessKey, SecretKey: secretKey}
	if _, err = globalIAMSys.CreateUser(t.Context(), accessKey, madmin.AddOrUpdateUserReq{
		SecretKey: secretKey,
		Status:    madmin.AccountEnabled,
	}); err != nil {
		t.Fatalf("%s: create delete authz user: %v", instanceType, err)
	}
	policyJSON := `{"Version":"2012-10-17","Statement":` + statements + `}`
	parsed, err := policy.ParseConfig(strings.NewReader(policyJSON))
	if err != nil {
		t.Fatalf("%s: parse delete authz policy: %v", instanceType, err)
	}
	policyName := "delete-version-authz-" + mustGetUUID()
	if _, err = globalIAMSys.SetPolicy(t.Context(), policyName, *parsed); err != nil {
		t.Fatalf("%s: install delete authz policy: %v", instanceType, err)
	}
	if _, err = globalIAMSys.PolicyDBSet(t.Context(), accessKey, policyName, regUser, false); err != nil {
		t.Fatalf("%s: attach delete authz policy: %v", instanceType, err)
	}
	return creds
}
