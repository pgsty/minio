// Copyright (c) 2015-2021 MinIO, Inc.
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
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/set"
	"github.com/minio/minio-go/v7/pkg/tags"
	"github.com/minio/minio/internal/bucket/cors"
	bucketsse "github.com/minio/minio/internal/bucket/encryption"
	"github.com/minio/minio/internal/bucket/lifecycle"
	objectlock "github.com/minio/minio/internal/bucket/object/lock"
	"github.com/minio/minio/internal/bucket/replication"
	"github.com/minio/minio/internal/bucket/versioning"
	"github.com/minio/minio/internal/event"
	"github.com/minio/minio/internal/kms"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/pkg/v3/policy"
	"github.com/minio/pkg/v3/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// BucketMetadataSys captures all bucket metadata for a given cluster.
type BucketMetadataSys struct {
	objAPI ObjectLayer

	sync.RWMutex
	initialized bool
	group       *singleflight.Group
	metadataMap map[string]BucketMetadata
	// loadFailed tracks real buckets whose metadata could not be loaded at
	// startup (concurrentLoad) or during a refresh. Such buckets are NOT
	// resident in metadataMap even though the subsystem is Initialized, so a
	// plain map miss cannot distinguish "not a bucket" from "known bucket whose
	// config we could not read". Callers that must fail closed for a real but
	// unreadable bucket (e.g. per-bucket CORS) consult this set. It is bounded
	// by the number of load failures and is empty in normal operation.
	loadFailed map[string]struct{}
}

// Count returns number of bucket metadata map entries.
func (sys *BucketMetadataSys) Count() int {
	sys.RLock()
	defer sys.RUnlock()

	return len(sys.metadataMap)
}

// Remove bucket metadata from memory.
func (sys *BucketMetadataSys) Remove(buckets ...string) {
	sys.Lock()
	for _, bucket := range buckets {
		sys.group.Forget(bucket)
		delete(sys.metadataMap, bucket)
		delete(sys.loadFailed, bucket)
		globalBucketMonitor.DeleteBucket(bucket)
	}
	sys.Unlock()
}

// RemoveStaleBuckets removes all stale buckets in memory that are not on disk.
func (sys *BucketMetadataSys) RemoveStaleBuckets(diskBuckets set.StringSet) {
	sys.Lock()
	defer sys.Unlock()

	for bucket := range sys.metadataMap {
		if diskBuckets.Contains(bucket) {
			continue
		} // doesn't exist on disk remove from memory.
		delete(sys.metadataMap, bucket)
		globalBucketMonitor.DeleteBucket(bucket)
	}
	for bucket := range sys.loadFailed {
		if !diskBuckets.Contains(bucket) {
			delete(sys.loadFailed, bucket)
		}
	}
}

// Set - sets a new metadata in-memory.
// Only a shallow copy is saved and fields with references
// cannot be modified without causing a race condition,
// so they should be replaced atomically and not appended to, etc.
// Data is not persisted to disk.
func (sys *BucketMetadataSys) Set(bucket string, meta BucketMetadata) {
	if !isMinioMetaBucketName(bucket) {
		sys.Lock()
		sys.metadataMap[bucket] = meta
		delete(sys.loadFailed, bucket)
		sys.Unlock()
	}
}

func (sys *BucketMetadataSys) updateAndParse(ctx context.Context, bucket string, configFile string, configData []byte, parse, lifecycleDelete bool) (updatedAt time.Time, err error) {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return updatedAt, errServerNotInitialized
	}

	if isMinioMetaBucketName(bucket) {
		return updatedAt, errInvalidArgument
	}
	ctx, unlock, err := lockBucketMetadata(ctx, objAPI, bucket)
	if err != nil {
		return updatedAt, err
	}

	err = func() error {
		defer unlock()
		meta, err := loadBucketMetadataParse(ctx, objAPI, bucket, parse)
		if err != nil {
			if !globalIsErasure && !globalIsDistErasure && errors.Is(err, errVolumeNotFound) {
				// Only single drive mode needs this fallback.
				meta = newBucketMetadata(bucket)
			} else {
				return err
			}
		}
		if lifecycleDelete {
			configData, err = lifecycleDeleteConfig(meta.LifecycleConfigXML)
			if err != nil {
				return err
			}
		}
		updatedAt = UTCNow()
		switch configFile {
		case bucketPolicyConfig:
			meta.PolicyConfigJSON = configData
			meta.PolicyConfigUpdatedAt = updatedAt
		case bucketNotificationConfig:
			meta.NotificationConfigXML = configData
			meta.NotificationConfigUpdatedAt = updatedAt
		case bucketLifecycleConfig:
			meta.LifecycleConfigXML = configData
			meta.LifecycleConfigUpdatedAt = updatedAt
		case bucketSSEConfig:
			meta.EncryptionConfigXML = configData
			meta.EncryptionConfigUpdatedAt = updatedAt
		case bucketTaggingConfig:
			meta.TaggingConfigXML = configData
			meta.TaggingConfigUpdatedAt = updatedAt
		case bucketCorsConfig:
			meta.CorsConfigXML = configData
			meta.CorsConfigUpdatedAt = updatedAt
		case bucketQuotaConfigFile:
			meta.QuotaConfigJSON = configData
			meta.QuotaConfigUpdatedAt = updatedAt
		case objectLockConfig:
			meta.ObjectLockConfigXML = configData
			meta.ObjectLockConfigUpdatedAt = updatedAt
		case bucketVersioningConfig:
			meta.VersioningConfigXML = configData
			meta.VersioningConfigUpdatedAt = updatedAt
		case bucketReplicationConfig:
			meta.ReplicationConfigXML = configData
			meta.ReplicationConfigUpdatedAt = updatedAt
		case bucketTargetsFile:
			meta.BucketTargetsConfigJSON, meta.BucketTargetsConfigMetaJSON, err = encryptBucketMetadata(ctx, meta.Name, configData, kms.Context{
				bucket:            meta.Name,
				bucketTargetsFile: bucketTargetsFile,
			})
			if err != nil {
				return fmt.Errorf("Error encrypting bucket target metadata %w", err)
			}
			meta.BucketTargetsConfigUpdatedAt = updatedAt
			meta.BucketTargetsConfigMetaUpdatedAt = updatedAt
		default:
			return fmt.Errorf("Unknown bucket %s metadata update requested %s", bucket, configFile)
		}
		return sys.saveMetadata(ctx, objAPI, meta)
	}()
	if err != nil {
		return updatedAt, err
	}
	globalNotificationSys.LoadBucketMetadata(bgContext(ctx), bucket) // Do not use caller context here
	return updatedAt, nil
}

func (sys *BucketMetadataSys) save(ctx context.Context, meta BucketMetadata) error {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return errServerNotInitialized
	}

	if isMinioMetaBucketName(meta.Name) {
		return errInvalidArgument
	}

	if err := sys.saveMetadata(ctx, objAPI, meta); err != nil {
		return err
	}

	globalNotificationSys.LoadBucketMetadata(bgContext(ctx), meta.Name) // Do not use caller context here
	return nil
}

// saveMetadata persists and publishes metadata locally. Callers performing a
// read-modify-write must hold metadata.lock and release it before peer fan-out.
func (sys *BucketMetadataSys) saveMetadata(ctx context.Context, objAPI ObjectLayer, meta BucketMetadata) error {
	if err := meta.Save(ctx, objAPI); err != nil {
		return err
	}
	sys.Set(meta.Name, meta)
	return nil
}

func lockBucketMetadata(ctx context.Context, objectAPI ObjectLayer, bucket string) (context.Context, func(), error) {
	lock := objectAPI.NewNSLock(minioMetaBucket, pathJoin(bucketMetaPrefix, bucket, "metadata.lock"))
	lkctx, err := lock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return nil, nil, err
	}
	ctx = context.WithValue(lkctx.Context(), bucketMetadataLockContextKey{}, bucket)
	return ctx, func() { lock.Unlock(lkctx) }, nil
}

type bucketMetadataLockContextKey struct{}

func bucketMetadataLockHeld(ctx context.Context, bucket string) bool {
	lockedBucket, _ := ctx.Value(bucketMetadataLockContextKey{}).(string)
	return lockedBucket == bucket
}

// Delete delete the bucket metadata for the specified bucket.
// must be used by all callers instead of using Update() with nil configData.
func (sys *BucketMetadataSys) Delete(ctx context.Context, bucket string, configFile string) (updatedAt time.Time, err error) {
	return sys.updateAndParse(ctx, bucket, configFile, nil, false, configFile == bucketLifecycleConfig)
}

func lifecycleDeleteConfig(current []byte) ([]byte, error) {
	var expiryRuleRemoved bool
	if len(current) > 0 {
		var lcCfg lifecycle.Lifecycle
		if err := xml.Unmarshal(current, &lcCfg); err != nil {
			return nil, err
		}
		for _, rl := range lcCfg.Rules {
			if !rl.Expiration.IsNull() || !rl.NoncurrentVersionExpiration.IsNull() {
				expiryRuleRemoved = true
				break
			}
		}
	}
	if !expiryRuleRemoved {
		return nil, nil
	}
	var lcCfg lifecycle.Lifecycle
	currtime := time.Now()
	lcCfg.ExpiryUpdatedAt = &currtime
	return xml.Marshal(lcCfg)
}

// Update update bucket metadata for the specified bucket.
// The configData data should not be modified after being sent here.
func (sys *BucketMetadataSys) Update(ctx context.Context, bucket string, configFile string, configData []byte) (updatedAt time.Time, err error) {
	return sys.updateAndParse(ctx, bucket, configFile, configData, true, false)
}

// Get metadata for a bucket.
// If no metadata exists errConfigNotFound is returned and a new metadata is returned.
// Only a shallow copy is returned, so referenced data should not be modified,
// but can be replaced atomically.
//
// This function should only be used with
// - GetBucketInfo
// - ListBuckets
// For all other bucket specific metadata, use the relevant
// calls implemented specifically for each of those features.
func (sys *BucketMetadataSys) Get(bucket string) (BucketMetadata, error) {
	if isMinioMetaBucketName(bucket) {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	sys.RLock()
	defer sys.RUnlock()

	meta, ok := sys.metadataMap[bucket]
	if !ok {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	return meta, nil
}

// GetVersioningConfig returns configured versioning config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetVersioningConfig(bucket string) (*versioning.Versioning, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return &versioning.Versioning{XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/"}, meta.Created, nil
		}
		return &versioning.Versioning{XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/"}, time.Time{}, err
	}
	return meta.versioningConfig, meta.VersioningConfigUpdatedAt, nil
}

// GetBucketPolicy returns configured bucket policy
func (sys *BucketMetadataSys) GetBucketPolicy(bucket string) (*policy.BucketPolicy, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketPolicyNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	if meta.policyConfig == nil {
		return nil, time.Time{}, BucketPolicyNotFound{Bucket: bucket}
	}
	return meta.policyConfig, meta.PolicyConfigUpdatedAt, nil
}

// GetTaggingConfig returns configured tagging config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetTaggingConfig(bucket string) (*tags.Tags, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketTaggingNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	if meta.taggingConfig == nil {
		return nil, time.Time{}, BucketTaggingNotFound{Bucket: bucket}
	}
	return meta.taggingConfig, meta.TaggingConfigUpdatedAt, nil
}

// GetObjectLockConfig returns configured object lock config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetObjectLockConfig(bucket string) (*objectlock.Config, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketObjectLockConfigNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	if meta.objectLockConfig == nil {
		return nil, time.Time{}, BucketObjectLockConfigNotFound{Bucket: bucket}
	}
	return meta.objectLockConfig, meta.ObjectLockConfigUpdatedAt, nil
}

// GetLifecycleConfig returns configured lifecycle config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetLifecycleConfig(bucket string) (*lifecycle.Lifecycle, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketLifecycleNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	// there could be just `ExpiryUpdatedAt` field populated as part
	// of last delete all. Treat this situation as not lifecycle configuration
	// available
	if meta.lifecycleConfig == nil || len(meta.lifecycleConfig.Rules) == 0 {
		return nil, time.Time{}, BucketLifecycleNotFound{Bucket: bucket}
	}
	return meta.lifecycleConfig, meta.LifecycleConfigUpdatedAt, nil
}

// GetNotificationConfig returns configured notification config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetNotificationConfig(bucket string) (*event.Config, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		return nil, err
	}
	return meta.notificationConfig, nil
}

// GetSSEConfig returns configured SSE config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetSSEConfig(bucket string) (*bucketsse.BucketSSEConfig, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketSSEConfigNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	if meta.sseConfig == nil {
		return nil, time.Time{}, BucketSSEConfigNotFound{Bucket: bucket}
	}
	return meta.sseConfig, meta.EncryptionConfigUpdatedAt, nil
}

// GetCorsConfig returns the CORS configuration for the given bucket.
// The returned object must not be modified.
func (sys *BucketMetadataSys) GetCorsConfig(bucket string) (*cors.Config, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		return nil, time.Time{}, err
	}
	if meta.corsConfigErr != nil {
		return nil, meta.CorsConfigUpdatedAt, meta.corsConfigErr
	}
	if meta.corsConfig == nil {
		return nil, time.Time{}, errConfigNotFound
	}
	return meta.corsConfig, meta.CorsConfigUpdatedAt, nil
}

// GetResidentCorsConfig returns the CORS configuration for the given bucket
// using only bucket metadata that is already resident in memory. Unlike
// GetCorsConfig it never loads metadata from disk and never caches a new
// entry.
//
// The per-request CORS middleware runs before authentication, for every
// Origin-bearing request, using the validated first path segment as the bucket
// name.
// Routing that through GetCorsConfig (which loads and caches) let an
// unauthenticated client grow metadataMap without bound and trigger an
// erasure metadata probe for every distinct, attacker-controlled,
// non-existent name it sent with an Origin header (e.g. /minio/... , /api/... ,
// or random buckets). Every bucket that can carry a CORS document is made
// resident when the document is written (Set) and when metadata is loaded at
// startup (Init/concurrentLoad), so a resident-only read is complete for real
// buckets while costing only an in-memory map lookup for everything else.
//
// While bucket metadata is still loading (not yet Initialized) a non-resident
// bucket returns errBucketMetadataNotInitialized so the caller fails closed
// rather than answering with the permissive global policy for a bucket whose
// restrictive CORS document may simply not be loaded yet.
func (sys *BucketMetadataSys) GetResidentCorsConfig(bucket string) (*cors.Config, time.Time, error) {
	if isMinioMetaBucketName(bucket) {
		// Preserve GetConfig's semantics for the internal namespace: this is
		// not a real bucket, and returning a non-errConfigNotFound error makes
		// the CORS middleware fail closed rather than answer for .minio.sys
		// with the permissive global policy.
		return nil, time.Time{}, errInvalidArgument
	}
	if isReservedOrInvalidBucket(bucket, true) {
		return nil, time.Time{}, errConfigNotFound
	}
	sys.RLock()
	meta, ok := sys.metadataMap[bucket]
	_, failed := sys.loadFailed[bucket]
	initialized := sys.initialized
	sys.RUnlock()
	if ok {
		if meta.corsConfigErr != nil {
			return nil, meta.CorsConfigUpdatedAt, meta.corsConfigErr
		}
		if meta.corsConfig == nil {
			return nil, time.Time{}, errConfigNotFound
		}
		return meta.corsConfig, meta.CorsConfigUpdatedAt, nil
	}
	// Not resident. Two cases must not be conflated:
	//   - metadata is still loading (!initialized), or this is a real bucket
	//     whose metadata failed to load: we cannot rule out a restrictive CORS
	//     config, so fail closed rather than answer with the global policy.
	//   - a fully initialized subsystem with no record of the name: it is not a
	//     bucket that can carry CORS, so fall back to the global policy without
	//     loading or caching metadata for an arbitrary, client-supplied name.
	if !initialized || failed {
		return nil, time.Time{}, errBucketMetadataNotInitialized
	}
	return nil, time.Time{}, errConfigNotFound
}

// GetCorsConfigXML returns the raw stored CORS configuration XML for the
// given bucket, preserving the document exactly as it was PUT (including
// the S3 xmlns and any unmodeled elements).
func (sys *BucketMetadataSys) GetCorsConfigXML(bucket string) ([]byte, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		return nil, time.Time{}, err
	}
	if meta.corsConfigErr != nil {
		return nil, meta.CorsConfigUpdatedAt, meta.corsConfigErr
	}
	if len(meta.CorsConfigXML) == 0 {
		return nil, time.Time{}, errConfigNotFound
	}
	return meta.CorsConfigXML, meta.CorsConfigUpdatedAt, nil
}

// CreatedAt returns the time of creation of bucket
func (sys *BucketMetadataSys) CreatedAt(bucket string) (time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		return time.Time{}, err
	}
	return meta.Created.UTC(), nil
}

// GetPolicyConfig returns configured bucket policy
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetPolicyConfig(bucket string) (*policy.BucketPolicy, time.Time, error) {
	meta, _, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketPolicyNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	if meta.policyConfig == nil {
		return nil, time.Time{}, BucketPolicyNotFound{Bucket: bucket}
	}
	return meta.policyConfig, meta.PolicyConfigUpdatedAt, nil
}

// GetQuotaConfig returns configured bucket quota
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetQuotaConfig(ctx context.Context, bucket string) (*madmin.BucketQuota, time.Time, error) {
	meta, _, err := sys.GetConfig(ctx, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketQuotaConfigNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}
	return meta.quotaConfig, meta.QuotaConfigUpdatedAt, nil
}

// GetReplicationConfig returns configured bucket replication config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetReplicationConfig(ctx context.Context, bucket string) (*replication.Config, time.Time, error) {
	meta, reloaded, err := sys.GetConfig(ctx, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, time.Time{}, BucketReplicationConfigNotFound{Bucket: bucket}
		}
		return nil, time.Time{}, err
	}

	if meta.replicationConfig == nil {
		return nil, time.Time{}, BucketReplicationConfigNotFound{Bucket: bucket}
	}
	if reloaded {
		globalBucketTargetSys.set(bucket, meta)
	}
	return meta.replicationConfig, meta.ReplicationConfigUpdatedAt, nil
}

// GetBucketTargetsConfig returns configured bucket targets for this bucket
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetBucketTargetsConfig(bucket string) (*madmin.BucketTargets, error) {
	meta, reloaded, err := sys.GetConfig(GlobalContext, bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketRemoteTargetNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.bucketTargetConfig == nil {
		return nil, BucketRemoteTargetNotFound{Bucket: bucket}
	}
	if reloaded {
		globalBucketTargetSys.set(bucket, meta)
	}
	return meta.bucketTargetConfig, nil
}

// GetConfigFromDisk read bucket metadata config from disk.
func (sys *BucketMetadataSys) GetConfigFromDisk(ctx context.Context, bucket string) (BucketMetadata, error) {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return newBucketMetadata(bucket), errServerNotInitialized
	}

	if isMinioMetaBucketName(bucket) {
		return newBucketMetadata(bucket), errInvalidArgument
	}

	return loadBucketMetadata(ctx, objAPI, bucket)
}

var errBucketMetadataNotInitialized = errors.New("bucket metadata not initialized yet")

// GetConfig returns a specific configuration from the bucket metadata.
// The returned object may not be modified.
// reloaded will be true if metadata refreshed from disk
func (sys *BucketMetadataSys) GetConfig(ctx context.Context, bucket string) (meta BucketMetadata, reloaded bool, err error) {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return newBucketMetadata(bucket), reloaded, errServerNotInitialized
	}

	if isMinioMetaBucketName(bucket) {
		return newBucketMetadata(bucket), reloaded, errInvalidArgument
	}

	sys.RLock()
	meta, ok := sys.metadataMap[bucket]
	sys.RUnlock()
	if ok {
		return meta, reloaded, nil
	}

	val, err, _ := sys.group.Do(bucket, func() (val any, err error) {
		meta, err = loadBucketMetadata(ctx, objAPI, bucket)
		if err != nil {
			if !sys.Initialized() {
				// bucket metadata not yet initialized
				return newBucketMetadata(bucket), errBucketMetadataNotInitialized
			}
		}
		return meta, err
	})
	meta, _ = val.(BucketMetadata)
	if err != nil {
		return meta, false, err
	}
	sys.Lock()
	sys.metadataMap[bucket] = meta
	sys.Unlock()

	return meta, true, nil
}

// Init - initializes bucket metadata system for all buckets.
func (sys *BucketMetadataSys) Init(ctx context.Context, buckets []string, objAPI ObjectLayer) error {
	if objAPI == nil {
		return errServerNotInitialized
	}

	sys.objAPI = objAPI

	// Load bucket metadata sys.
	sys.init(ctx, buckets)
	return nil
}

// concurrently load bucket metadata to speed up loading bucket metadata.
func (sys *BucketMetadataSys) concurrentLoad(ctx context.Context, buckets []string) {
	g := errgroup.WithNErrs(len(buckets))
	bucketMetas := make([]BucketMetadata, len(buckets))
	for index := range buckets {
		g.Go(func() error {
			// Sleep and stagger to avoid blocked CPU and thundering
			// herd upon start up sequence.
			time.Sleep(25*time.Millisecond + time.Duration(rand.Int63n(int64(100*time.Millisecond))))

			_, _ = sys.objAPI.HealBucket(ctx, buckets[index], madmin.HealOpts{Recreate: true})
			meta, err := loadBucketMetadata(ctx, sys.objAPI, buckets[index])
			if err != nil {
				return err
			}
			bucketMetas[index] = meta
			return nil
		}, index)
	}

	errs := g.Wait()
	for index, err := range errs {
		if err != nil {
			internalLogOnceIf(ctx, fmt.Errorf("Unable to load bucket metadata, will be retried: %w", err),
				"load-bucket-metadata-"+buckets[index], logger.WarningKind)
		}
	}

	// Hold lock here to update in-memory map at once,
	// instead of serializing the Go routines.
	sys.Lock()
	for i, meta := range bucketMetas {
		if errs[i] != nil {
			// Real bucket whose metadata could not be loaded: record it so
			// consumers that must fail closed (per-bucket CORS) can tell it
			// apart from a name that is not a bucket at all.
			sys.loadFailed[buckets[i]] = struct{}{}
			continue
		}
		delete(sys.loadFailed, buckets[i])
		sys.metadataMap[buckets[i]] = meta
	}
	sys.Unlock()

	for i, meta := range bucketMetas {
		if errs[i] != nil {
			continue
		}
		globalEventNotifier.set(buckets[i], meta)   // set notification targets
		globalBucketTargetSys.set(buckets[i], meta) // set remote replication targets
	}
}

func (sys *BucketMetadataSys) refreshBucketsMetadataLoop(ctx context.Context) {
	const bucketMetadataRefresh = 15 * time.Minute

	sleeper := newDynamicSleeper(2, 150*time.Millisecond, false)

	t := time.NewTimer(bucketMetadataRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			buckets, err := sys.objAPI.ListBuckets(ctx, BucketOptions{NoMetadata: true})
			if err != nil {
				internalLogIf(ctx, err, logger.WarningKind)
				break
			}

			// Handle if we have some buckets in-memory those are stale.
			// first delete them and then replace the newer state()
			// from disk.
			diskBuckets := set.CreateStringSet()
			for _, bucket := range buckets {
				diskBuckets.Add(bucket.Name)
			}
			sys.RemoveStaleBuckets(diskBuckets)

			for i := range buckets {
				wait := sleeper.Timer(ctx)

				bucket := buckets[i].Name
				updated := false

				meta, err := loadBucketMetadata(ctx, sys.objAPI, bucket)
				if err != nil {
					internalLogIf(ctx, err, logger.WarningKind)
					sys.Lock()
					sys.loadFailed[bucket] = struct{}{}
					sys.Unlock()
					wait() // wait to proceed to next entry.
					continue
				}

				sys.Lock()
				// Update if the bucket metadata in the memory is older than on-disk one
				if lu := sys.metadataMap[bucket].lastUpdate(); lu.Before(meta.lastUpdate()) {
					updated = true
					sys.metadataMap[bucket] = meta
				}
				// A successful (re)load clears any earlier load failure.
				delete(sys.loadFailed, bucket)
				sys.Unlock()

				if updated {
					globalEventNotifier.set(bucket, meta)
					globalBucketTargetSys.set(bucket, meta)
				}

				wait() // wait to proceed to next entry.
			}
		}
		t.Reset(bucketMetadataRefresh)
	}
}

// Initialized indicates if bucket metadata sys is initialized atleast once.
func (sys *BucketMetadataSys) Initialized() bool {
	sys.RLock()
	defer sys.RUnlock()

	return sys.initialized
}

// Loads bucket metadata for all buckets into BucketMetadataSys.
func (sys *BucketMetadataSys) init(ctx context.Context, buckets []string) {
	count := globalEndpoints.ESCount() * 10
	for {
		if len(buckets) < count {
			sys.concurrentLoad(ctx, buckets)
			break
		}
		sys.concurrentLoad(ctx, buckets[:count])
		buckets = buckets[count:]
	}

	sys.Lock()
	sys.initialized = true
	sys.Unlock()

	if globalIsDistErasure {
		go sys.refreshBucketsMetadataLoop(ctx)
	}
}

// Reset the state of the BucketMetadataSys.
func (sys *BucketMetadataSys) Reset() {
	sys.Lock()
	clear(sys.metadataMap)
	clear(sys.loadFailed)
	sys.Unlock()
}

// NewBucketMetadataSys - creates new policy system.
func NewBucketMetadataSys() *BucketMetadataSys {
	return &BucketMetadataSys{
		metadataMap: make(map[string]BucketMetadata),
		loadFailed:  make(map[string]struct{}),
		group:       &singleflight.Group{},
	}
}
