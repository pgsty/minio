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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio/internal/auth"
	"github.com/pgsty/silo-pkg/v3/policy"
)

// These tests pin the IAM bucket/object resource boundary end to end, through
// the real handlers rather than the matcher alone. An object-only resource
// pattern ("arn:aws:s3:::bucket/*") must not authorize the bucket-level writes
// that hand a caller something its object access does not already provide —
// upstream minio/minio issue #20449 — while every shape the fix deliberately
// leaves alone keeps working. Both directions are asserted, because a change
// here that only removes permissions is correct and one that adds any is not.

// A bucket-level write reached only through an object-only grant must be
// refused, and refusing it must not destroy the bucket. A grant that names the
// bucket must still succeed, and succeeding must actually remove it.
func assertBucketDelete(ctx context.Context, c *check, admin, client *minio.Client, bucket string, wantAllowed bool) {
	c.Helper()
	err := client.RemoveBucket(ctx, bucket)
	if wantAllowed {
		if err != nil {
			c.Fatalf("RemoveBucket(%s) denied, want allowed: %v", bucket, err)
		}
		exists, existsErr := admin.BucketExists(ctx, bucket)
		if existsErr != nil {
			c.Fatalf("check removed bucket %s: %v", bucket, existsErr)
		}
		if exists {
			c.Fatalf("RemoveBucket(%s) returned nil but bucket still exists", bucket)
		}
		return
	}

	if err == nil {
		c.Fatalf("RemoveBucket(%s) returned nil, want AccessDenied", bucket)
	}
	if response := minio.ToErrorResponse(err); response.Code != "AccessDenied" {
		c.Fatalf("RemoveBucket(%s) error code=%q, want AccessDenied (err=%v)", bucket, response.Code, err)
	}
	exists, existsErr := admin.BucketExists(ctx, bucket)
	if existsErr != nil {
		c.Fatalf("check protected bucket %s: %v", bucket, existsErr)
	}
	if !exists {
		c.Fatalf("RemoveBucket(%s) returned AccessDenied but bucket disappeared", bucket)
	}
	if err := admin.RemoveBucket(ctx, bucket); err != nil {
		c.Fatalf("cleanup protected bucket %s: %v", bucket, err)
	}
}

func createUserWithPolicy(ctx context.Context, c *check, s *TestSuiteIAM, policyJSON []byte) (*minio.Client, string) {
	c.Helper()
	accessKey, secretKey := mustGenerateCredentials(c)
	if err := s.adm.SetUser(ctx, accessKey, secretKey, madmin.AccountEnabled); err != nil {
		c.Fatalf("set boundary user: %v", err)
	}
	policyName := "boundary-" + mustGetUUID()
	if err := s.adm.AddCannedPolicy(ctx, policyName, policyJSON); err != nil {
		c.Fatalf("add boundary policy: %v", err)
	}
	if _, err := s.adm.AttachPolicy(ctx, madmin.PolicyAssociationReq{
		Policies: []string{policyName},
		User:     accessKey,
	}); err != nil {
		c.Fatalf("attach boundary policy: %v", err)
	}
	return s.getUserClient(c, accessKey, secretKey, ""), accessKey
}

func TestBucketResourceBoundaryEndToEnd(t *testing.T) {
	if runtime.GOOS == globalWindowsOSName {
		t.Skip("IAM integration harness is disabled on Windows")
	}
	suite := newTestSuiteIAM(TestSuiteCommon{serverType: "ErasureSD", signer: signerV4}, false)
	c := &check{t, suite.serverType}
	suite.SetUpSuite(c)
	defer suite.TearDownSuite(c)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// The #20449 reproduction: "s3:*" on "bucket/*" and nothing else.
	coreBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, coreBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create core bucket: %v", err)
	}
	corePolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::%s/*"}]
}`, coreBucket)
	coreClient, _ := createUserWithPolicy(ctx, c, suite, corePolicy)
	assertBucketDelete(ctx, c, suite.client, coreClient, coreBucket, false)

	// The conventional pairing of bucket and object ARNs stays authorized.
	pairedBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, pairedBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create paired bucket: %v", err)
	}
	pairedPolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":[
  "arn:aws:s3:::%s","arn:aws:s3:::%s/*"
 ]}]
}`, pairedBucket, pairedBucket)
	pairedClient, _ := createUserWithPolicy(ctx, c, suite, pairedPolicy)
	assertBucketDelete(ctx, c, suite.client, pairedClient, pairedBucket, true)

	// CreateBucket and ListBucket keep the historical object-pattern matching.
	compatBucket := getRandomBucketName()
	compatPolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::%s/*"}]
}`, compatBucket)
	compatClient, _ := createUserWithPolicy(ctx, c, suite, compatPolicy)
	if err := compatClient.MakeBucket(ctx, compatBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("CreateBucket compatibility path denied: %v", err)
	}
	for item := range compatClient.ListObjects(ctx, compatBucket, minio.ListObjectsOptions{}) {
		if item.Err != nil {
			c.Fatalf("ListBucket compatibility path denied: %v", item.Err)
		}
	}
	if err := suite.client.RemoveBucket(ctx, compatBucket); err != nil {
		c.Fatalf("cleanup compatibility bucket: %v", err)
	}

	// Withholding the trailing slash changes the string patterns match against,
	// so a fixed-width wildcard can match the bare bucket name without ever
	// having matched "bucket/". Honoring it would GRANT a delete the historical
	// matcher refused, which the hardening must never do.
	wildcardBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, wildcardBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create fixed-width wildcard bucket: %v", err)
	}
	wildcardPolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:DeleteBucket","Resource":"arn:aws:s3:::%s?"}]
}`, wildcardBucket[:len(wildcardBucket)-1])
	wildcardClient, _ := createUserWithPolicy(ctx, c, suite, wildcardPolicy)
	assertBucketDelete(ctx, c, suite.client, wildcardClient, wildcardBucket, false)

	// A NotResource exclusion keeps its historical reach, so narrowing it — and
	// thereby broadening the Allow it qualifies — cannot happen unnoticed.
	notResourceBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, notResourceBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create NotResource bucket: %v", err)
	}
	notResourcePolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","NotResource":"arn:aws:s3:::%s/*"}]
}`, notResourceBucket)
	notResourceClient, _ := createUserWithPolicy(ctx, c, suite, notResourcePolicy)
	assertBucketDelete(ctx, c, suite.client, notResourceClient, notResourceBucket, false)

	// A Deny written against "bucket/*" keeps covering the bucket-level request.
	denyBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, denyBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create Deny bucket: %v", err)
	}
	denyPolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[
  {"Effect":"Allow","Action":"s3:*","Resource":"*"},
  {"Effect":"Deny","Action":"s3:DeleteBucket","Resource":"arn:aws:s3:::%s/*"}
 ]
}`, denyBucket)
	denyClient, _ := createUserWithPolicy(ctx, c, suite, denyPolicy)
	assertBucketDelete(ctx, c, suite.client, denyClient, denyBucket, false)

	// A service account whose parent policy allows everything but whose inline
	// policy is object-only exercises the nested AND evaluation path, where the
	// boundary has to hold on the inline side.
	serviceBucket := getRandomBucketName()
	if err := suite.client.MakeBucket(ctx, serviceBucket, minio.MakeBucketOptions{}); err != nil {
		c.Fatalf("create service-account bucket: %v", err)
	}
	parentPolicy := []byte(`{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]
}`)
	_, parentUser := createUserWithPolicy(ctx, c, suite, parentPolicy)
	servicePolicy := fmt.Appendf(nil, `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::%s/*"}]
}`, serviceBucket)
	serviceAccess, serviceSecret := mustGenerateCredentials(c)
	serviceAccount, err := suite.adm.AddServiceAccount(ctx, madmin.AddServiceAccountReq{
		TargetUser: parentUser,
		AccessKey:  serviceAccess,
		SecretKey:  serviceSecret,
		Policy:     bytes.Clone(servicePolicy),
	})
	if err != nil {
		c.Fatalf("create restricted service account: %v", err)
	}
	serviceClient := suite.getUserClient(c, serviceAccount.AccessKey, serviceAccount.SecretKey, "")
	assertBucketDelete(ctx, c, suite.client, serviceClient, serviceBucket, false)
}

// The same boundary, asserted against an inline session policy evaluated
// directly, so a regression in the STS and service-account paths is caught even
// if the integration harness above is skipped.
func TestBucketResourceBoundaryInlineSessionPolicy(t *testing.T) {
	inline := `{
 "Version": "2012-10-17",
 "Statement": [{
  "Effect": "Allow",
  "Action": "s3:*",
  "Resource": "arn:aws:s3:::mybucket/*"
 }]
}`
	args := policy.Args{
		Action:     policy.DeleteBucketAction,
		BucketName: "mybucket",
		Claims: map[string]any{
			sessionPolicyNameExtracted: inline,
		},
	}

	for name, evaluate := range map[string]func(policy.Args) (bool, bool){
		"STS inline policy":             isAllowedBySessionPolicy,
		"service-account inline policy": isAllowedBySessionPolicyForServiceAccount,
	} {
		hasPolicy, allowed := evaluate(args)
		if !hasPolicy {
			t.Errorf("%s was not detected", name)
			continue
		}
		if allowed {
			t.Errorf("%s authorized DeleteBucket through an object-only resource", name)
		}
	}
}

// The boundary driven through the real S3 router and DeleteBucket handler,
// without opening a TCP listener.
func TestBucketResourceBoundaryHandler(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:         t,
		endpoints: nil, // Register the full router; the focused list omits DeleteBucket.
		objAPITest: func(obj ObjectLayer, instanceType, _ string, apiRouter http.Handler, _ auth.Credentials, t *testing.T) {
			ctx := t.Context()
			if !globalReplicationPool.IsSet() {
				// Match initTestServerWithBackend: DeleteBucket calls through this
				// singleton after the object-layer deletion, and a nil receiver is safe.
				globalReplicationPool.Set(nil)
			}

			newPolicyClient := func(policyJSON string) auth.Credentials {
				t.Helper()
				accessKey, secretKey, err := auth.GenerateCredentials()
				if err != nil {
					t.Fatal(err)
				}
				credentials := auth.Credentials{AccessKey: accessKey, SecretKey: secretKey}
				if _, err = globalIAMSys.CreateUser(ctx, credentials.AccessKey, madmin.AddOrUpdateUserReq{
					SecretKey: credentials.SecretKey,
					Status:    madmin.AccountEnabled,
				}); err != nil {
					t.Fatalf("%s: create boundary user: %v", instanceType, err)
				}
				parsed, err := policy.ParseConfig(strings.NewReader(policyJSON))
				if err != nil {
					t.Fatalf("%s: parse boundary policy: %v", instanceType, err)
				}
				policyName := "boundary-" + mustGetUUID()
				if _, err = globalIAMSys.SetPolicy(ctx, policyName, *parsed); err != nil {
					t.Fatalf("%s: install boundary policy: %v", instanceType, err)
				}
				if _, err = globalIAMSys.PolicyDBSet(ctx, credentials.AccessKey, policyName, regUser, false); err != nil {
					t.Fatalf("%s: attach boundary policy: %v", instanceType, err)
				}
				return credentials
			}

			deleteCase := func(label, policyJSON string, wantAllowed bool) {
				t.Helper()
				bucket := getRandomBucketName()
				if err := obj.MakeBucket(ctx, bucket, MakeBucketOptions{}); err != nil {
					t.Fatalf("%s/%s: create bucket: %v", instanceType, label, err)
				}
				policyJSON = strings.ReplaceAll(policyJSON, "BUCKET_WILDCARD", bucket[:len(bucket)-1]+"?")
				policyJSON = strings.ReplaceAll(policyJSON, "BUCKET", bucket)
				credentials := newPolicyClient(policyJSON)
				req, err := newTestSignedRequestV4(http.MethodDelete, getDeleteBucketURL("", bucket),
					0, nil, credentials.AccessKey, credentials.SecretKey, nil)
				if err != nil {
					t.Fatalf("%s/%s: sign DeleteBucket: %v", instanceType, label, err)
				}
				recorder := httptest.NewRecorder()
				apiRouter.ServeHTTP(recorder, req)

				if wantAllowed {
					if recorder.Code != http.StatusNoContent {
						t.Fatalf("%s/%s: DeleteBucket status=%d body=%s, want 204",
							instanceType, label, recorder.Code, recorder.Body.String())
					}
					if _, err = obj.GetBucketInfo(ctx, bucket, BucketOptions{}); err == nil {
						t.Fatalf("%s/%s: handler returned 204 but bucket still exists", instanceType, label)
					}
					return
				}

				if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "AccessDenied") {
					t.Fatalf("%s/%s: DeleteBucket status=%d body=%s, want AccessDenied",
						instanceType, label, recorder.Code, recorder.Body.String())
				}
				if _, err = obj.GetBucketInfo(ctx, bucket, BucketOptions{}); err != nil {
					t.Fatalf("%s/%s: denied bucket disappeared: %v", instanceType, label, err)
				}
				if err = obj.DeleteBucket(ctx, bucket, DeleteBucketOptions{}); err != nil {
					t.Fatalf("%s/%s: cleanup bucket: %v", instanceType, label, err)
				}
			}

			deleteCase("object-only s3:*", `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::BUCKET/*"}]
}`, false)

			deleteCase("paired resource", `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","Resource":[
  "arn:aws:s3:::BUCKET","arn:aws:s3:::BUCKET/*"
 ]}]
}`, true)

			deleteCase("fixed-width bucket wildcard", `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:DeleteBucket","Resource":"arn:aws:s3:::BUCKET_WILDCARD"}]
}`, false)

			deleteCase("NotResource exclusion", `{
 "Version":"2012-10-17",
 "Statement":[{"Effect":"Allow","Action":"s3:*","NotResource":"arn:aws:s3:::BUCKET/*"}]
}`, false)

			deleteCase("Deny coverage", `{
 "Version":"2012-10-17",
 "Statement":[
  {"Effect":"Allow","Action":"s3:*","Resource":"*"},
  {"Effect":"Deny","Action":"s3:DeleteBucket","Resource":"arn:aws:s3:::BUCKET/*"}
 ]
}`, false)
		},
	})
}
