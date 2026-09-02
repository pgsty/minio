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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
	"github.com/minio/mux"
)

func TestSRBucketObjectLockMetadata(t *testing.T) {
	updatedAt := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	current := "current"
	legacy := "legacy"

	event := newSRBucketObjectLockMeta("bucket", &current, updatedAt)
	if event.Type != madmin.SRBucketMetaTypeObjectLockConfig || event.Bucket != "bucket" ||
		event.ObjectLockConfig == nil || *event.ObjectLockConfig != current || event.Tags != nil || !event.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected Object Lock event: %#v", event)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip madmin.SRBucketMeta
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.ObjectLockConfig == nil || *roundTrip.ObjectLockConfig != current || roundTrip.Tags != nil {
		t.Fatalf("unexpected JSON round trip: %#v", roundTrip)
	}

	for _, test := range []struct {
		name string
		item madmin.SRBucketMeta
		want *string
	}{
		{name: "current", item: madmin.SRBucketMeta{ObjectLockConfig: &current}, want: &current},
		{name: "legacy", item: madmin.SRBucketMeta{Tags: &legacy}, want: &legacy},
		{name: "current wins", item: madmin.SRBucketMeta{ObjectLockConfig: &current, Tags: &legacy}, want: &current},
		{name: "missing", item: madmin.SRBucketMeta{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := srObjectLockPayload(test.item)
			if test.want == nil {
				if got != nil {
					t.Fatalf("payload = %q, want nil", *got)
				}
				return
			}
			if got == nil || *got != *test.want {
				t.Fatalf("payload = %v, want %q", got, *test.want)
			}
		})
	}
}

func TestPeerBucketObjectLockMetadataCurrentAndLegacyPayloads(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testPeerBucketObjectLockMetadataCurrentAndLegacyPayloads,
		makeBucketOptions: MakeBucketOptions{LockEnabled: true},
	})
}

func applySRBucketMetaViaAdmin(t *testing.T, credentials auth.Credentials, item madmin.SRBucketMeta) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	adminRouter := mux.NewRouter()
	registerAdminRouter(adminRouter, true)
	path := adminPathPrefix + adminAPIVersionPrefix + "/site-replication/peer/bucket-meta"
	req, err := newTestSignedRequestV4(http.MethodPut, path, int64(len(body)), bytes.NewReader(body),
		credentials.AccessKey, credentials.SecretKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	adminRouter.ServeHTTP(rec, req)
	return rec
}

func testPeerBucketObjectLockMetadataCurrentAndLegacyPayloads(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, credentials auth.Credentials, t *testing.T,
) {
	apply := func(item madmin.SRBucketMeta, wantDays uint64) {
		t.Helper()
		rec := applySRBucketMetaViaAdmin(t, credentials, item)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: admin Object Lock apply returned %d: %s", instanceType, rec.Code, rec.Body.String())
		}
		config, _, err := globalBucketMetadataSys.GetObjectLockConfig(bucketName)
		if err != nil {
			t.Fatal(err)
		}
		if config.Rule == nil || config.Rule.DefaultRetention.Mode != "GOVERNANCE" ||
			config.Rule.DefaultRetention.Days == nil || *config.Rule.DefaultRetention.Days != wantDays {
			t.Fatalf("%s: persisted Object Lock config = %s, want GOVERNANCE/%d days", instanceType, config, wantDays)
		}
	}

	config30 := base64.StdEncoding.EncodeToString([]byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`))
	apply(newSRBucketObjectLockMeta(bucketName, &config30, UTCNow().Add(time.Hour)), 30)

	config45 := base64.StdEncoding.EncodeToString([]byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>45</Days></DefaultRetention></Rule></ObjectLockConfiguration>`))
	apply(madmin.SRBucketMeta{
		Type:      madmin.SRBucketMetaTypeObjectLockConfig,
		Bucket:    bucketName,
		Tags:      &config45,
		UpdatedAt: UTCNow().Add(2 * time.Hour),
	}, 45)
}

func TestPeerBucketObjectLockMetadataWithoutLockEnabled(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testPeerBucketObjectLockMetadataWithoutLockEnabled,
	})
}

func testPeerBucketObjectLockMetadataWithoutLockEnabled(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, credentials auth.Credentials, t *testing.T,
) {
	config := base64.StdEncoding.EncodeToString([]byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`))
	item := newSRBucketObjectLockMeta(bucketName, &config, UTCNow().Add(time.Hour))
	rec := applySRBucketMetaViaAdmin(t, credentials, item)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: admin Object Lock apply returned %d: %s", instanceType, rec.Code, rec.Body.String())
	}
	meta, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	// A lock configuration implies versioning: the bucket was created without
	// lock, so receiving the configuration turns plain Enabled versioning on.
	if meta.objectLockConfig == nil || !bytes.Equal(meta.VersioningConfigXML, enabledBucketVersioningConfig) {
		t.Fatalf("%s: bucket metadata = objectLock:%v versioning:%q", instanceType, meta.objectLockConfig, meta.VersioningConfigXML)
	}
}

func TestHealObjectLockMetadataUsesObjectLockField(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testHealObjectLockMetadataUsesObjectLockField,
	})
}

func testHealObjectLockMetadataUsesObjectLockField(obj ObjectLayer, instanceType, bucketName string,
	_ http.Handler, credentials auth.Credentials, t *testing.T,
) {
	ctx := t.Context()
	localID := globalDeploymentID()
	remoteID := "remote-object-lock-heal"
	updatedAt := UTCNow().Add(time.Hour)
	createdAt := updatedAt.Add(-time.Hour)
	config := base64.StdEncoding.EncodeToString([]byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`))

	remoteApplies := make(chan madmin.SRBucketMeta, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var applied madmin.SRBucketMeta
		if err := json.NewDecoder(r.Body).Decode(&applied); err != nil {
			t.Errorf("%s: decode remote apply: %v", instanceType, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		remoteApplies <- applied
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()

	serviceCred, err := auth.CreateCredentials("object-lock-heal-svc", "object-lock-heal-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	serviceCred.ParentUser = credentials.AccessKey
	if _, err = globalIAMSys.store.AddServiceAccount(ctx, serviceCred); err != nil {
		t.Fatal(err)
	}
	defer globalIAMSys.DeleteServiceAccount(ctx, serviceCred.AccessKey, false)

	globalSiteReplicationSys.Lock()
	oldEnabled := globalSiteReplicationSys.enabled
	oldState := globalSiteReplicationSys.state
	globalSiteReplicationSys.enabled = true
	globalSiteReplicationSys.state = srState{
		Name:                    "object-lock-heal-test",
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

	status := srStatusInfo{
		Sites: map[string]madmin.PeerInfo{
			localID:  {Name: "local", DeploymentID: localID},
			remoteID: {Name: "remote", DeploymentID: remoteID, Endpoint: remote.URL},
		},
		BucketStats: map[string]map[string]srBucketStatsSummary{
			bucketName: {
				localID: {
					SRBucketStatsSummary: madmin.SRBucketStatsSummary{OLockConfigMismatch: true},
					meta: srBucketMetaInfo{SRBucketInfo: madmin.SRBucketInfo{
						Bucket:                    bucketName,
						CreatedAt:                 createdAt,
						ObjectLockConfig:          &config,
						ObjectLockConfigUpdatedAt: updatedAt,
					}, DeploymentID: localID},
				},
				remoteID: {
					SRBucketStatsSummary: madmin.SRBucketStatsSummary{OLockConfigMismatch: true},
					meta: srBucketMetaInfo{SRBucketInfo: madmin.SRBucketInfo{
						Bucket:    bucketName,
						CreatedAt: createdAt,
					}, DeploymentID: remoteID},
				},
			},
		},
	}
	if err := globalSiteReplicationSys.healOLockConfigMetadata(ctx, obj, bucketName, status); err != nil {
		t.Fatal(err)
	}
	select {
	case applied := <-remoteApplies:
		if applied.Type != madmin.SRBucketMetaTypeObjectLockConfig || applied.Bucket != bucketName ||
			applied.ObjectLockConfig == nil || *applied.ObjectLockConfig != config || applied.Tags != nil || !applied.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("%s: remote heal apply = %#v", instanceType, applied)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: remote heal did not dispatch Object Lock metadata", instanceType)
	}
}
