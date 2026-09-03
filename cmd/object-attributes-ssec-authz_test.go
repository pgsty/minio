// Copyright (c) 2015-2025 MinIO, Inc.
// Copyright (c) 2025-2026 PGSTY
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
	"crypto/md5"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/pgsty/silo-pkg/v3/policy"
)

// GetObjectAttributes lets a replication peer read SSE-C attributes without
// presenting the customer key. The X-Minio-Source-Replication-Request header
// that marks such a request is client controlled, so the carve-out has to be
// gated on the caller actually holding s3:ReplicateObject. Root credentials
// hold every action, so a root-only test cannot tell the gate apart from an
// ungated header check; these cases drive it with least-privilege users.
func TestAPIGetObjectAttributesSSECReplicationAuthz(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testAPIGetObjectAttributesSSECReplicationAuthz,
	})
}

func testAPIGetObjectAttributesSSECReplicationAuthz(_ ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	previousTLS := globalIsTLS
	globalIsTLS = true
	defer func() { globalIsTLS = previousTLS }()

	key := bytes.Repeat([]byte{0x11}, 32)
	keyMD5 := md5.Sum(key)
	sseHeaders := map[string]string{
		xhttp.AmzServerSideEncryptionCustomerAlgorithm: xhttp.AmzEncryptionAES,
		xhttp.AmzServerSideEncryptionCustomerKey:       base64.StdEncoding.EncodeToString(key),
		xhttp.AmzServerSideEncryptionCustomerKeyMD5:    base64.StdEncoding.EncodeToString(keyMD5[:]),
	}
	replicationHeader := map[string]string{xhttp.MinIOSourceReplicationRequest: "true"}

	object := "attributes/ssec-replication-authz"
	putCopyChecksumSource(t, apiRouter, credentials, bucketName, object,
		bytes.Repeat([]byte("attributes-authz-"), 512), sseHeaders)

	// s3:GetObjectAttributes and s3:GetObject reach the handler; only the
	// second policy adds the replication action the carve-out requires.
	readerOnly := newObjectAttributesAuthzUser(t, instanceType, bucketName,
		`"s3:GetObject","s3:GetObjectAttributes"`)
	replicator := newObjectAttributesAuthzUser(t, instanceType, bucketName,
		`"s3:GetObject","s3:GetObjectAttributes","s3:ReplicateObject"`)

	for _, test := range []struct {
		name    string
		creds   auth.Credentials
		headers map[string]string
		want    int
	}{
		// The header alone must not stand in for s3:ReplicateObject.
		{name: "reader/replication-header-only", creds: readerOnly, headers: replicationHeader, want: http.StatusBadRequest},
		// A caller that may replicate this object keeps the carve-out.
		{name: "replicator/replication-header-only", creds: replicator, headers: replicationHeader, want: http.StatusOK},
		// Root holds s3:ReplicateObject, so its carve-out is unchanged.
		{name: "root/replication-header-only", creds: credentials, headers: replicationHeader, want: http.StatusOK},
		// The ordinary key-bearing path is unaffected for every caller.
		{name: "reader/correct-key", creds: readerOnly, headers: sseHeaders, want: http.StatusOK},
		{name: "reader/no-key", creds: readerOnly, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := objectAttributesSSECRequest(t, apiRouter, test.creds, bucketName, object, test.headers)
			if rec.Code != test.want {
				t.Fatalf("%s: status %d, want %d: %s", instanceType, rec.Code, test.want, rec.Body.String())
			}
		})
	}
}

// newObjectAttributesAuthzUser installs a least-privilege user whose policy
// grants exactly the listed actions on bucketName's objects.
func newObjectAttributesAuthzUser(t *testing.T, instanceType, bucketName, actions string) auth.Credentials {
	t.Helper()
	ctx := t.Context()

	accessKey, secretKey, err := auth.GenerateCredentials()
	if err != nil {
		t.Fatalf("%s: generate credentials: %v", instanceType, err)
	}
	creds := auth.Credentials{AccessKey: accessKey, SecretKey: secretKey}
	if _, err = globalIAMSys.CreateUser(ctx, creds.AccessKey, madmin.AddOrUpdateUserReq{
		SecretKey: creds.SecretKey,
		Status:    madmin.AccountEnabled,
	}); err != nil {
		t.Fatalf("%s: create attributes user: %v", instanceType, err)
	}

	policyJSON := `{
 "Version": "2012-10-17",
 "Statement": [{
  "Effect": "Allow",
  "Action": [` + actions + `],
  "Resource": ["arn:aws:s3:::` + bucketName + `/*"]
 }]
}`
	parsed, err := policy.ParseConfig(strings.NewReader(policyJSON))
	if err != nil {
		t.Fatalf("%s: parse attributes policy: %v", instanceType, err)
	}
	policyName := "attributes-authz-" + mustGetUUID()
	if _, err = globalIAMSys.SetPolicy(ctx, policyName, *parsed); err != nil {
		t.Fatalf("%s: install attributes policy: %v", instanceType, err)
	}
	if _, err = globalIAMSys.PolicyDBSet(ctx, creds.AccessKey, policyName, regUser, false); err != nil {
		t.Fatalf("%s: attach attributes policy: %v", instanceType, err)
	}
	return creds
}
