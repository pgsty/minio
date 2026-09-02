// Copyright (c) 2015-2026 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
)

func TestSiteReplicationStatusAccountsPerSiteAndSurvivesMalformedConfig(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testSiteReplicationStatusAccountsPerSiteAndSurvivesMalformedConfig,
	})
}

func testSiteReplicationStatusAccountsPerSiteAndSurvivesMalformedConfig(obj ObjectLayer, instanceType, localBucket string,
	_ http.Handler, credentials auth.Credentials, t *testing.T,
) {
	ctx := t.Context()
	remoteBucket := getRandomBucketName()
	if err := obj.MakeBucket(ctx, remoteBucket, MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	remoteBucketMeta, err := loadBucketMetadata(ctx, obj, remoteBucket)
	if err != nil {
		t.Fatal(err)
	}
	globalBucketMetadataSys.Set(remoteBucket, remoteBucketMeta)
	globalNotificationSys.LoadBucketMetadata(ctx, remoteBucket)

	tagXML := []byte(`<Tagging><TagSet><Tag><Key>key</Key><Value>value</Value></Tag></TagSet></Tagging>`)
	versioningXML := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
	objectLockXML := []byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`)
	sseXML := []byte(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
	quotaJSON, err := json.Marshal(madmin.BucketQuota{Type: madmin.HardQuota, Quota: 1024})
	if err != nil {
		t.Fatal(err)
	}
	policyJSON := []byte(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, localBucket))

	for configFile, data := range map[string][]byte{
		bucketTaggingConfig:    tagXML,
		bucketVersioningConfig: versioningXML,
		objectLockConfig:       objectLockXML,
		bucketSSEConfig:        sseXML,
		bucketQuotaConfigFile:  quotaJSON,
		bucketPolicyConfig:     policyJSON,
	} {
		if _, err := globalBucketMetadataSys.Update(ctx, localBucket, configFile, data); err != nil {
			t.Fatalf("%s: update %s: %v", instanceType, configFile, err)
		}
	}
	if _, err := updateLocalBucketCORSMetadata(ctx, obj, localBucket, []byte(testSiteReplicationCORSDoc)); err != nil {
		t.Fatalf("%s: update %s: %v", instanceType, bucketCorsConfig, err)
	}

	encode := func(data []byte) *string {
		encoded := base64.StdEncoding.EncodeToString(data)
		return &encoded
	}
	remotePolicy := []byte(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, remoteBucket))
	localMeta, err := globalBucketMetadataSys.Get(localBucket)
	if err != nil {
		t.Fatal(err)
	}
	remoteInfo := madmin.SRInfo{
		DeploymentID: "remote-status-accounting",
		Buckets: map[string]madmin.SRBucketInfo{
			localBucket: {
				Bucket:    localBucket,
				CreatedAt: localMeta.Created,
			},
			remoteBucket: {
				Bucket:              remoteBucket,
				CreatedAt:           remoteBucketMeta.Created,
				Tags:                encode(tagXML),
				Versioning:          encode(versioningXML),
				ObjectLockConfig:    encode(objectLockXML),
				SSEConfig:           encode(sseXML),
				QuotaConfig:         encode(quotaJSON),
				Policy:              remotePolicy,
				CorsConfig:          encode([]byte(testSiteReplicationCORSDoc)),
				CorsConfigUpdatedAt: remoteBucketMeta.Created,
			},
		},
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(remoteInfo); err != nil {
			t.Errorf("%s: encode remote metadata: %v", instanceType, err)
		}
	}))
	defer remote.Close()

	serviceCred, err := auth.CreateCredentials("status-accounting-svc", "status-accounting-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	serviceCred.ParentUser = credentials.AccessKey
	if _, err = globalIAMSys.store.AddServiceAccount(ctx, serviceCred); err != nil {
		t.Fatal(err)
	}
	defer globalIAMSys.DeleteServiceAccount(ctx, serviceCred.AccessKey, false)

	localID := globalDeploymentID()
	remoteID := remoteInfo.DeploymentID
	globalSiteReplicationSys.Lock()
	oldEnabled := globalSiteReplicationSys.enabled
	oldState := globalSiteReplicationSys.state
	globalSiteReplicationSys.enabled = true
	globalSiteReplicationSys.state = srState{
		Name:                    "status-accounting-test",
		ServiceAccountAccessKey: serviceCred.AccessKey,
		Peers: map[string]madmin.PeerInfo{
			localID:  {Name: "local", DeploymentID: localID},
			remoteID: {Name: "remote", DeploymentID: remoteID, Endpoint: remote.URL},
		},
	}
	globalSiteReplicationSys.Unlock()
	defer func() {
		globalSiteReplicationSys.Lock()
		globalSiteReplicationSys.enabled = oldEnabled
		globalSiteReplicationSys.state = oldState
		globalSiteReplicationSys.Unlock()
	}()

	check := func(name string, wantRemoteTags, wantRemoteQuota int) {
		t.Helper()
		status, err := globalSiteReplicationSys.siteReplicationStatus(ctx, obj, madmin.SRStatusOptions{Buckets: true})
		if err != nil {
			t.Fatal(err)
		}
		local := status.StatsSummary[localID]
		remote := status.StatsSummary[remoteID]
		if local.TotalBucketsCount != 2 || remote.TotalBucketsCount != 2 {
			t.Fatalf("%s: bucket totals = local:%d remote:%d", name, local.TotalBucketsCount, remote.TotalBucketsCount)
		}
		if local.TotalTagsCount != 1 || remote.TotalTagsCount != wantRemoteTags ||
			local.TotalLockConfigCount != 1 || remote.TotalLockConfigCount != 1 ||
			local.TotalSSEConfigCount != 1 || remote.TotalSSEConfigCount != 1 ||
			local.TotalVersioningConfigCount != 1 || remote.TotalVersioningConfigCount != 1 ||
			local.TotalBucketPoliciesCount != 1 || remote.TotalBucketPoliciesCount != 1 ||
			local.TotalQuotaConfigCount != 1 || remote.TotalQuotaConfigCount != wantRemoteQuota ||
			local.TotalCorsConfigCount != 1 || remote.TotalCorsConfigCount != 1 {
			t.Fatalf("%s: site totals = local:%+v remote:%+v", name, local, remote)
		}
		if local.ReplicatedTags != 0 || remote.ReplicatedTags != 0 ||
			local.ReplicatedBucketPolicies != 0 || remote.ReplicatedBucketPolicies != 0 ||
			local.ReplicatedQuotaConfig != 0 || remote.ReplicatedQuotaConfig != 0 {
			t.Fatalf("%s: asymmetric configs counted as replicated: local:%+v remote:%+v", name, local, remote)
		}
		remoteBucketStatus := status.BucketStats[remoteBucket][remoteID]
		if remoteBucketStatus.HasTagsSet != (wantRemoteTags != 0) || remoteBucketStatus.HasQuotaCfgSet != (wantRemoteQuota != 0) {
			t.Fatalf("%s: remote bucket presence = tags:%v quota:%v", name, remoteBucketStatus.HasTagsSet, remoteBucketStatus.HasQuotaCfgSet)
		}
	}

	check("valid asymmetric configs", 1, 1)
	invalidTags := "not-base64"
	info := remoteInfo.Buckets[remoteBucket]
	info.Tags = &invalidTags
	remoteInfo.Buckets[remoteBucket] = info
	check("malformed tags do not drop site", 0, 1)

	emptyQuota := base64.StdEncoding.EncodeToString([]byte(`{}`))
	info = remoteInfo.Buckets[remoteBucket]
	info.QuotaConfig = &emptyQuota
	remoteInfo.Buckets[remoteBucket] = info
	check("empty quota is absent", 0, 0)
}
