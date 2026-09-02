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
	"net/http"
	"testing"
	"time"

	"github.com/minio/minio/internal/auth"
)

func TestPeerBucketAdoptionPreservesLockAndVersioningConfigs(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testPeerBucketAdoptionPreservesLockAndVersioningConfigs,
		makeBucketOptions: MakeBucketOptions{LockEnabled: true},
	})
}

func testPeerBucketAdoptionPreservesLockAndVersioningConfigs(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	objectLockXML := []byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`)
	// A locked bucket carries plain Enabled versioning; adoption must keep the
	// existing document and its timestamp rather than rewrite them.
	versioningXML := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, objectLockConfig, objectLockXML); err != nil {
		t.Fatal(err)
	}
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, bucketVersioningConfig, versioningXML); err != nil {
		t.Fatal(err)
	}
	before, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}

	if err := globalSiteReplicationSys.PeerBucketMakeWithVersioningHandler(t.Context(), bucketName, MakeBucketOptions{
		CreatedAt:   before.Created.Add(-time.Hour),
		LockEnabled: true,
	}); err != nil {
		t.Fatalf("%s: adopting existing bucket failed: %v", instanceType, err)
	}
	after, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after.ObjectLockConfigXML, before.ObjectLockConfigXML) || !after.ObjectLockConfigUpdatedAt.Equal(before.ObjectLockConfigUpdatedAt) {
		t.Fatalf("%s: Object Lock config changed during adoption", instanceType)
	}
	if !bytes.Equal(after.VersioningConfigXML, before.VersioningConfigXML) || !after.VersioningConfigUpdatedAt.Equal(before.VersioningConfigUpdatedAt) {
		t.Fatalf("%s: versioning config changed during adoption", instanceType)
	}
}

func TestPeerBucketAdoptionBootstrapsMissingConfigs(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testPeerBucketAdoptionBootstrapsMissingConfigs,
	})
}

func TestPeerBucketAdoptionNormalizesVersioningWhenEnablingLock(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testPeerBucketAdoptionNormalizesVersioningWhenEnablingLock,
	})
}

func testPeerBucketAdoptionNormalizesVersioningWhenEnablingLock(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	versioningXML := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status><ExcludeFolders>true</ExcludeFolders><ExcludedPrefixes><Prefix>temporary/</Prefix></ExcludedPrefixes></VersioningConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, bucketVersioningConfig, versioningXML); err != nil {
		t.Fatal(err)
	}
	before, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if err := globalSiteReplicationSys.PeerBucketMakeWithVersioningHandler(t.Context(), bucketName, MakeBucketOptions{
		CreatedAt:   before.Created,
		LockEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after.VersioningConfigXML, enabledBucketVersioningConfig) || !after.VersioningConfigUpdatedAt.After(before.VersioningConfigUpdatedAt) {
		t.Fatalf("%s: prefix-excluded versioning survived enabling Object Lock: %q", instanceType, after.VersioningConfigXML)
	}
	if !bytes.Equal(after.ObjectLockConfigXML, enabledBucketObjectLockConfig) {
		t.Fatalf("%s: Object Lock was not bootstrapped", instanceType)
	}
}

func TestPeerBucketAdoptionEnablesSuspendedVersioning(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testPeerBucketAdoptionEnablesSuspendedVersioning,
	})
}

func testPeerBucketAdoptionEnablesSuspendedVersioning(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	suspended := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Suspended</Status></VersioningConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, bucketVersioningConfig, suspended); err != nil {
		t.Fatal(err)
	}
	before, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if err := globalSiteReplicationSys.PeerBucketMakeWithVersioningHandler(t.Context(), bucketName, MakeBucketOptions{CreatedAt: before.Created}); err != nil {
		t.Fatal(err)
	}
	after, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if after.versioningConfig == nil || !after.versioningConfig.Enabled() {
		t.Fatalf("%s: versioning remained disabled: %q", instanceType, after.VersioningConfigXML)
	}
	if !after.VersioningConfigUpdatedAt.After(before.VersioningConfigUpdatedAt) {
		t.Fatalf("%s: versioning update time = %v, want after %v", instanceType, after.VersioningConfigUpdatedAt, before.VersioningConfigUpdatedAt)
	}
}

func TestEnablePeerBucketVersioningRepairsInvalidConfig(t *testing.T) {
	meta := newBucketMetadata("bucket")
	meta.Created = time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	meta.VersioningConfigXML = []byte(`<VersioningConfiguration>`)
	if err := enablePeerBucketVersioning(&meta, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(meta.VersioningConfigXML, enabledBucketVersioningConfig) || meta.VersioningConfigUpdatedAt.IsZero() {
		t.Fatalf("invalid versioning was not repaired: xml=%q updatedAt=%v", meta.VersioningConfigXML, meta.VersioningConfigUpdatedAt)
	}
}

func testPeerBucketAdoptionBootstrapsMissingConfigs(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	before, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.ObjectLockConfigXML) != 0 || len(before.VersioningConfigXML) != 0 {
		t.Fatalf("%s: invalid bootstrap precondition", instanceType)
	}
	if err := globalSiteReplicationSys.PeerBucketMakeWithVersioningHandler(t.Context(), bucketName, MakeBucketOptions{
		CreatedAt:   before.Created,
		LockEnabled: true,
	}); err != nil {
		t.Fatalf("%s: adopting existing bucket failed: %v", instanceType, err)
	}
	after, err := globalBucketMetadataSys.Get(bucketName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after.ObjectLockConfigXML, enabledBucketObjectLockConfig) || !bytes.Equal(after.VersioningConfigXML, enabledBucketVersioningConfig) {
		t.Fatalf("%s: missing bootstrap configs: objectLock=%q versioning=%q", instanceType, after.ObjectLockConfigXML, after.VersioningConfigXML)
	}
	if !after.ObjectLockConfigUpdatedAt.Equal(before.Created) || !after.VersioningConfigUpdatedAt.Equal(before.Created) {
		t.Fatalf("%s: bootstrap timestamps = (%v, %v), want %v", instanceType,
			after.ObjectLockConfigUpdatedAt, after.VersioningConfigUpdatedAt, before.Created)
	}
}

// TestLockedBucketNormalizesVersioningOnSave covers the metadata boundary
// itself: whatever writer stores a suspended or prefix-excluded versioning
// document on a bucket that carries an Object Lock configuration, including
// one with a default retention rule, Save replaces it with plain Enabled
// versioning.
func TestLockedBucketNormalizesVersioningOnSave(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testLockedBucketNormalizesVersioningOnSave,
		makeBucketOptions: MakeBucketOptions{LockEnabled: true},
	})
}

func testLockedBucketNormalizesVersioningOnSave(_ ObjectLayer, instanceType, bucketName string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	lockWithRule := []byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, objectLockConfig, lockWithRule); err != nil {
		t.Fatal(err)
	}
	for name, versioningXML := range map[string][]byte{
		"prefix-excluded": []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status><ExcludeFolders>true</ExcludeFolders><ExcludedPrefixes><Prefix>temporary/</Prefix></ExcludedPrefixes></VersioningConfiguration>`),
		"suspended":       []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Suspended</Status></VersioningConfiguration>`),
	} {
		if _, err := globalBucketMetadataSys.Update(t.Context(), bucketName, bucketVersioningConfig, versioningXML); err != nil {
			t.Fatal(err)
		}
		meta, err := globalBucketMetadataSys.Get(bucketName)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(meta.VersioningConfigXML, enabledBucketVersioningConfig) {
			t.Fatalf("%s/%s: locked bucket kept versioning %q", instanceType, name, meta.VersioningConfigXML)
		}
		reloaded, err := loadBucketMetadata(t.Context(), newObjectLayerFn(), bucketName)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reloaded.VersioningConfigXML, enabledBucketVersioningConfig) {
			t.Fatalf("%s/%s: locked bucket persisted versioning %q", instanceType, name, reloaded.VersioningConfigXML)
		}
	}
}
