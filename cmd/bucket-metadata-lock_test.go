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
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/auth"
	"github.com/minio/minio/internal/bucket/lifecycle"
	"github.com/minio/minio/internal/bucket/versioning"
)

type metadataRMWWriterKey struct{}

type metadataRMWBarrierObjectLayer struct {
	ObjectLayer
	bucket       string
	aReady       chan struct{}
	aRelease     chan struct{}
	bLockAttempt chan struct{}
	aReadyOnce   sync.Once
	bLockOnce    sync.Once
	cancelOnce   sync.Once
	cancelOnPut  context.CancelFunc
	reads        atomic.Int64
}

func (o *metadataRMWBarrierObjectLayer) metadataObject() string {
	return pathJoin(bucketMetaPrefix, o.bucket, bucketMetadataFile)
}

func (o *metadataRMWBarrierObjectLayer) metadataLock() string {
	return pathJoin(bucketMetaPrefix, o.bucket, "metadata.lock")
}

func (o *metadataRMWBarrierObjectLayer) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, opts ObjectOptions) (*GetObjectReader, error) {
	if bucket == minioMetaBucket && object == o.metadataObject() {
		o.reads.Add(1)
	}
	return o.ObjectLayer.GetObjectNInfo(ctx, bucket, object, rs, h, opts)
}

func (o *metadataRMWBarrierObjectLayer) PutObject(ctx context.Context, bucket, object string, data *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	if bucket == minioMetaBucket && object == o.metadataObject() && o.cancelOnPut != nil {
		o.cancelOnce.Do(o.cancelOnPut)
	}
	if bucket == minioMetaBucket && object == o.metadataObject() && ctx.Value(metadataRMWWriterKey{}) == "A" {
		o.aReadyOnce.Do(func() { close(o.aReady) })
		select {
		case <-o.aRelease:
		case <-ctx.Done():
			return ObjectInfo{}, ctx.Err()
		}
	}
	return o.ObjectLayer.PutObject(ctx, bucket, object, data, opts)
}

func (o *metadataRMWBarrierObjectLayer) NewNSLock(bucket string, objects ...string) RWLocker {
	lock := o.ObjectLayer.NewNSLock(bucket, objects...)
	if bucket != minioMetaBucket || len(objects) != 1 || objects[0] != o.metadataLock() {
		return lock
	}
	return metadataObservedRWLocker{RWLocker: lock, onLock: func(ctx context.Context) {
		if ctx.Value(metadataRMWWriterKey{}) == "B" {
			o.bLockOnce.Do(func() { close(o.bLockAttempt) })
		}
	}}
}

type metadataObservedRWLocker struct {
	RWLocker
	onLock func(context.Context)
}

func (l metadataObservedRWLocker) GetLock(ctx context.Context, timeout *dynamicTimeout) (LockContext, error) {
	l.onLock(ctx)
	return l.RWLocker.GetLock(ctx, timeout)
}

func TestBucketMetadataLockPreservesPolicyAndCORS(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketMetadataLockPreservesPolicyAndCORS,
	})
}

func testBucketMetadataLockPreservesPolicyAndCORS(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	policyJSON := fmt.Appendf(nil, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	corsXML := []byte(testSiteReplicationCORSDoc)
	runBucketMetadataRMWConflict(t, obj, bucket,
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Update(ctx, bucket, bucketPolicyConfig, policyJSON)
			return err
		},
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := updateLocalBucketCORSMetadata(ctx, objectAPI, bucket, corsXML)
			return err
		},
		func(meta BucketMetadata) bool {
			return bytes.Equal(meta.PolicyConfigJSON, policyJSON) && bytes.Equal(meta.CorsConfigXML, corsXML)
		}, instanceType+": policy+CORS")
}

func TestBucketMetadataLockPreservesTaggingAndSSE(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketMetadataLockPreservesTaggingAndSSE,
	})
}

func TestBucketMetadataLockPreservesPeerBulkAndLocalUpdate(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketMetadataLockPreservesPeerBulkAndLocalUpdate,
	})
}

func testBucketMetadataLockPreservesPeerBulkAndLocalUpdate(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	meta, err := readBucketMetadata(t.Context(), obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	policyJSON := fmt.Appendf(nil, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	tagXML := []byte(`<Tagging><TagSet><Tag><Key>local</Key><Value>tag</Value></Tag></TagSet></Tagging>`)
	runBucketMetadataRMWConflict(t, obj, bucket,
		func(ctx context.Context, objectAPI ObjectLayer) error {
			return globalSiteReplicationSys.PeerBucketMetadataUpdateHandler(ctx, madmin.SRBucketMeta{
				Bucket: bucket, Policy: policyJSON, UpdatedAt: meta.Created.Add(time.Second),
			})
		},
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Update(ctx, bucket, bucketTaggingConfig, tagXML)
			return err
		},
		func(meta BucketMetadata) bool {
			return bytes.Equal(meta.PolicyConfigJSON, policyJSON) && bytes.Equal(meta.TaggingConfigXML, tagXML)
		}, instanceType+": peer bulk+local tagging")
}

func TestBucketMetadataLockPreservesLifecycleDeleteAndSSE(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketMetadataLockPreservesLifecycleDeleteAndSSE,
	})
}

func testBucketMetadataLockPreservesLifecycleDeleteAndSSE(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	lifecycleXML := []byte(`<LifecycleConfiguration><Rule><ID>expire</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(t.Context(), bucket, bucketLifecycleConfig, lifecycleXML); err != nil {
		t.Fatal(err)
	}
	sseXML := []byte(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
	runBucketMetadataRMWConflict(t, obj, bucket,
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Delete(ctx, bucket, bucketLifecycleConfig)
			return err
		},
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Update(ctx, bucket, bucketSSEConfig, sseXML)
			return err
		},
		func(meta BucketMetadata) bool {
			cfg, err := lifecycle.ParseLifecycleConfig(bytes.NewReader(meta.LifecycleConfigXML))
			return err == nil && cfg.ExpiryUpdatedAt != nil && len(cfg.Rules) == 0 && bytes.Equal(meta.EncryptionConfigXML, sseXML)
		}, instanceType+": lifecycle delete+SSE")
}

func TestMakeBucketForceCreatePreservesMetadata(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testMakeBucketForceCreatePreservesMetadata,
	})
}

func testMakeBucketForceCreatePreservesMetadata(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	ctx := t.Context()
	policyJSON := fmt.Appendf(nil, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	corsXML := []byte(testSiteReplicationCORSDoc)
	if _, err := globalBucketMetadataSys.Update(ctx, bucket, bucketPolicyConfig, policyJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalBucketCORSMetadata(ctx, obj, bucket, corsXML); err != nil {
		t.Fatal(err)
	}
	before, err := readBucketMetadata(ctx, obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if err = obj.MakeBucket(ctx, bucket, MakeBucketOptions{ForceCreate: true}); err != nil {
		t.Fatalf("%s: ForceCreate existing bucket: %v", instanceType, err)
	}
	after, err := readBucketMetadata(ctx, obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Created.Equal(before.Created) || !bytes.Equal(after.PolicyConfigJSON, policyJSON) || !bytes.Equal(after.CorsConfigXML, corsXML) {
		t.Fatalf("%s: ForceCreate replaced metadata: before=%+v after=%+v", instanceType, before, after)
	}
}

func TestApplyImportedBucketMetadataPreservesUnspecifiedFields(t *testing.T) {
	policyJSON := []byte(`{"Version":"2012-10-17","Statement":[]}`)
	tagXML := []byte(`<Tagging><TagSet><Tag><Key>existing</Key><Value>tag</Value></Tag></TagSet></Tagging>`)
	src := newBucketMetadata("bucket")
	src.PolicyConfigJSON = policyJSON
	src.PolicyConfigUpdatedAt = UTCNow()
	dst := newBucketMetadata("bucket")
	dst.TaggingConfigXML = bytes.Clone(tagXML)

	applyImportedBucketMetadata(&dst, src, importMetadataFields{bucketPolicyConfig: {}})
	if !bytes.Equal(dst.PolicyConfigJSON, policyJSON) || !bytes.Equal(dst.TaggingConfigXML, tagXML) {
		t.Fatalf("import patch overwrote unspecified metadata: %+v", dst)
	}
	src.PolicyConfigJSON[0] = '!'
	if dst.PolicyConfigJSON[0] == '!' {
		t.Fatal("import patch retained the source byte slice")
	}
}

func TestMakeBucketDoesNotAdoptGhostMetadata(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testMakeBucketDoesNotAdoptGhostMetadata,
	})
}

func testMakeBucketDoesNotAdoptGhostMetadata(obj ObjectLayer, instanceType, _ string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	ctx := t.Context()
	bucket := getRandomBucketName()
	if err := obj.MakeBucket(ctx, bucket, MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	policyJSON := fmt.Appendf(nil, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	if _, err := globalBucketMetadataSys.Update(ctx, bucket, bucketPolicyConfig, policyJSON); err != nil {
		t.Fatal(err)
	}
	oldMeta, err := readBucketMetadata(ctx, obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	z, ok := obj.(*erasureServerPools)
	if !ok {
		t.Fatalf("%s: object layer is %T, want *erasureServerPools", instanceType, obj)
	}
	if err = z.s3Peer.DeleteBucket(ctx, bucket, DeleteBucketOptions{Force: true}); err != nil {
		t.Fatalf("%s: delete bucket volume only: %v", instanceType, err)
	}
	if err = obj.MakeBucket(ctx, bucket, MakeBucketOptions{}); err != nil {
		t.Fatalf("%s: recreate bucket: %v", instanceType, err)
	}
	newMeta, err := readBucketMetadata(ctx, obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(newMeta.PolicyConfigJSON, policyJSON) || newMeta.Created.Equal(oldMeta.Created) {
		t.Fatalf("%s: new bucket adopted ghost metadata: old=%+v new=%+v", instanceType, oldMeta, newMeta)
	}
}

func TestMakeBucketForceCreateLockEnablesVersioning(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testMakeBucketForceCreateLockEnablesVersioning,
	})
}

func TestPeerBucketMetadataSaveSurvivesCallerCancellation(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testPeerBucketMetadataSaveSurvivesCallerCancellation,
	})
}

func testPeerBucketMetadataSaveSurvivesCallerCancellation(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	previousObjectAPI := newObjectLayerFn()
	ctx, cancel := context.WithCancel(t.Context())
	barrier := &metadataRMWBarrierObjectLayer{
		ObjectLayer: obj,
		bucket:      bucket,
		cancelOnPut: cancel,
	}
	setObjectLayer(barrier)
	defer setObjectLayer(previousObjectAPI)

	err := globalSiteReplicationSys.PeerBucketMakeWithVersioningHandler(ctx, bucket, MakeBucketOptions{VersioningEnabled: true})
	if err != nil {
		t.Fatalf("%s: peer metadata save failed after caller cancellation: %v", instanceType, err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("%s: metadata write did not trigger caller cancellation", instanceType)
	}
	meta, err := readBucketMetadata(t.Context(), obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := versioning.ParseConfig(bytes.NewReader(meta.VersioningConfigXML))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled() {
		t.Fatalf("%s: peer metadata save lost versioning after cancellation", instanceType)
	}
}

func testMakeBucketForceCreateLockEnablesVersioning(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	ctx := t.Context()
	suspended := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Suspended</Status></VersioningConfiguration>`)
	if _, err := globalBucketMetadataSys.Update(ctx, bucket, bucketVersioningConfig, suspended); err != nil {
		t.Fatal(err)
	}
	if err := obj.MakeBucket(ctx, bucket, MakeBucketOptions{ForceCreate: true, LockEnabled: true}); err != nil {
		t.Fatalf("%s: ForceCreate with object lock: %v", instanceType, err)
	}
	meta, err := readBucketMetadata(ctx, obj, bucket)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := versioning.ParseConfig(bytes.NewReader(meta.VersioningConfigXML))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled() || len(meta.ObjectLockConfigXML) == 0 {
		t.Fatalf("%s: object lock state lacks enabled versioning: metadata=%+v", instanceType, meta)
	}
}

func testBucketMetadataLockPreservesTaggingAndSSE(obj ObjectLayer, instanceType, bucket string,
	_ http.Handler, _ auth.Credentials, t *testing.T,
) {
	tagXML := []byte(`<Tagging><TagSet><Tag><Key>key</Key><Value>value</Value></Tag></TagSet></Tagging>`)
	sseXML := []byte(`<ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
	runBucketMetadataRMWConflict(t, obj, bucket,
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Update(ctx, bucket, bucketTaggingConfig, tagXML)
			return err
		},
		func(ctx context.Context, objectAPI ObjectLayer) error {
			_, err := globalBucketMetadataSys.Update(ctx, bucket, bucketSSEConfig, sseXML)
			return err
		},
		func(meta BucketMetadata) bool {
			return bytes.Equal(meta.TaggingConfigXML, tagXML) && bytes.Equal(meta.EncryptionConfigXML, sseXML)
		}, instanceType+": tagging+SSE")
}

func runBucketMetadataRMWConflict(t *testing.T, obj ObjectLayer, bucket string,
	writerA, writerB func(context.Context, ObjectLayer) error,
	complete func(BucketMetadata) bool, name string,
) {
	t.Helper()
	previousObjectAPI := newObjectLayerFn()
	barrier := &metadataRMWBarrierObjectLayer{
		ObjectLayer:  obj,
		bucket:       bucket,
		aReady:       make(chan struct{}),
		aRelease:     make(chan struct{}),
		bLockAttempt: make(chan struct{}),
	}
	setObjectLayer(barrier)
	defer setObjectLayer(previousObjectAPI)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	aCtx := context.WithValue(ctx, metadataRMWWriterKey{}, "A")
	bCtx := context.WithValue(ctx, metadataRMWWriterKey{}, "B")
	aDone := make(chan error, 1)
	bDone := make(chan error, 1)
	go func() { aDone <- writerA(aCtx, barrier) }()

	select {
	case <-barrier.aReady:
	case <-ctx.Done():
		t.Fatalf("%s: writer A did not reach metadata save: %v", name, ctx.Err())
	}
	go func() { bDone <- writerB(bCtx, barrier) }()

	var (
		bErr      error
		bFinished bool
	)
	select {
	case <-barrier.bLockAttempt:
		if got := barrier.reads.Load(); got != 1 {
			t.Fatalf("%s: writer B read metadata before acquiring metadata.lock: reads=%d", name, got)
		}
	case bErr = <-bDone:
		bFinished = true
	case <-ctx.Done():
		t.Fatalf("%s: writer B neither completed nor attempted metadata.lock: %v", name, ctx.Err())
	}
	close(barrier.aRelease)
	if err := <-aDone; err != nil {
		t.Fatalf("%s: writer A failed: %v", name, err)
	}
	if !bFinished {
		select {
		case bErr = <-bDone:
		case <-ctx.Done():
			t.Fatalf("%s: writer B did not finish: %v", name, ctx.Err())
		}
	}
	if bErr != nil {
		t.Fatalf("%s: writer B failed: %v", name, bErr)
	}

	disk, err := readBucketMetadata(ctx, barrier, bucket)
	if err != nil {
		t.Fatalf("%s: read disk metadata: %v", name, err)
	}
	resident, err := globalBucketMetadataSys.Get(bucket)
	if err != nil {
		t.Fatalf("%s: read resident metadata: %v", name, err)
	}
	if !complete(disk) || !complete(resident) {
		t.Fatalf("%s: concurrent updates lost a field: disk=%+v resident=%+v", name, disk, resident)
	}
}
