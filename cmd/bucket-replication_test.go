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
	"fmt"
	"net/http"
	"path"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio/internal/bucket/replication"
	xhttp "github.com/minio/minio/internal/http"
)

var configs = []replication.Config{
	{ // Config0 - Replication config has no filters, existing object replication enabled
		Rules: []replication.Rule{
			{
				Status:                    replication.Enabled,
				Priority:                  1,
				DeleteMarkerReplication:   replication.DeleteMarkerReplication{Status: replication.Enabled},
				DeleteReplication:         replication.DeleteReplication{Status: replication.Enabled},
				Filter:                    replication.Filter{},
				ExistingObjectReplication: replication.ExistingObjectReplication{Status: replication.Enabled},
				SourceSelectionCriteria: replication.SourceSelectionCriteria{
					ReplicaModifications: replication.ReplicaModifications{Status: replication.Enabled},
				},
			},
		},
	},
}

var replicationConfigTests = []struct {
	info         ObjectInfo
	name         string
	rcfg         replicationConfig
	dsc          ReplicateDecision
	tgtStatuses  map[string]replication.StatusType
	expectedSync bool
}{
	{ // 1. no replication config
		name:         "no replication config",
		info:         ObjectInfo{Size: 100},
		rcfg:         replicationConfig{Config: nil},
		expectedSync: false,
	},
	{ // 2. existing object replication config enabled, no versioning
		name:         "existing object replication config enabled, no versioning",
		info:         ObjectInfo{Size: 100},
		rcfg:         replicationConfig{Config: &configs[0]},
		expectedSync: false,
	},
	{ // 3. existing object replication config enabled, versioning suspended
		name:         "existing object replication config enabled, versioning suspended",
		info:         ObjectInfo{Size: 100, VersionID: nullVersionID},
		rcfg:         replicationConfig{Config: &configs[0]},
		expectedSync: false,
	},
	{ // 4. existing object replication enabled, versioning enabled; no reset in progress
		name: "existing object replication enabled, versioning enabled; no reset in progress",
		info: ObjectInfo{
			Size:              100,
			ReplicationStatus: replication.Completed,
			VersionID:         "a3348c34-c352-4498-82f0-1098e8b34df9",
		},
		rcfg:         replicationConfig{Config: &configs[0]},
		expectedSync: false,
	},
}

func TestReplicationResync(t *testing.T) {
	ctx := t.Context()
	for i, test := range replicationConfigTests {
		if sync := test.rcfg.Resync(ctx, test.info, test.dsc, test.tgtStatuses); sync.mustResync() != test.expectedSync {
			t.Errorf("Test%d (%s): Resync  got %t , want %t", i+1, test.name, sync.mustResync(), test.expectedSync)
		}
	}
}

var (
	start                   = UTCNow().AddDate(0, 0, -1)
	replicationConfigTests2 = []struct {
		info         ObjectInfo
		name         string
		rcfg         replicationConfig
		dsc          ReplicateDecision
		tgtStatuses  map[string]replication.StatusType
		expectedSync bool
	}{
		{ // Cases 1-4: existing object replication enabled, versioning enabled, no reset - replication status varies
			// 1: Pending replication
			name: "existing object replication on object in Pending replication status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:PENDING;",
				ReplicationStatus:         replication.Pending,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
			},
			rcfg: replicationConfig{remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
				Arn: "arn1",
			}}}},
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			expectedSync: true,
		},

		{ // 2. replication status Failed
			name: "existing object replication on object in Failed replication status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:FAILED",
				ReplicationStatus:         replication.Failed,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
			},
			dsc: ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
				Arn: "arn1",
			}}}},
			expectedSync: true,
		},
		{ // 3. replication status unset
			name: "existing object replication on pre-existing unreplicated object",
			info: ObjectInfo{
				Size:              100,
				ReplicationStatus: replication.StatusType(""),
				VersionID:         "a3348c34-c352-4498-82f0-1098e8b34df9",
			},
			rcfg: replicationConfig{remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
				Arn: "arn1",
			}}}},
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			expectedSync: true,
		},
		{ // 4. replication status Complete
			name: "existing object replication on object in Completed replication status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:COMPLETED",
				ReplicationStatus:         replication.Completed,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
			},
			dsc: ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", false, false)}},
			rcfg: replicationConfig{remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
				Arn: "arn1",
			}}}},
			expectedSync: false,
		},
		{ // 5. existing object replication enabled, versioning enabled, replication status Pending & reset ID present
			name: "existing object replication with reset in progress and object in Pending status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:PENDING;",
				ReplicationStatus:         replication.Pending,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
				UserDefined:               map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;abc", UTCNow().AddDate(0, -1, 0).String())},
			},
			expectedSync: true,
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: UTCNow(),
				}}},
			},
		},
		{ // 6. existing object replication enabled, versioning enabled, replication status Failed & reset ID present
			name: "existing object replication with reset in progress and object in Failed status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:FAILED;",
				ReplicationStatus:         replication.Failed,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
				UserDefined:               map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;abc", UTCNow().AddDate(0, -1, 0).String())},
			},
			dsc: ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: UTCNow(),
				}}},
			},
			expectedSync: true,
		},
		{ // 7. existing object replication enabled, versioning enabled, replication status unset & reset ID present
			name: "existing object replication with reset in progress and object never replicated before",
			info: ObjectInfo{
				Size:              100,
				ReplicationStatus: replication.StatusType(""),
				VersionID:         "a3348c34-c352-4498-82f0-1098e8b34df9",
				UserDefined:       map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;abc", UTCNow().AddDate(0, -1, 0).String())},
			},
			dsc: ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: UTCNow(),
				}}},
			},

			expectedSync: true,
		},

		{ // 8. existing object replication enabled, versioning enabled, replication status Complete & reset ID present
			name: "existing object replication enabled - reset in progress for an object in Completed status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:COMPLETED;",
				ReplicationStatus:         replication.Completed,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df8",
				UserDefined:               map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;abc", UTCNow().AddDate(0, -1, 0).String())},
			},
			expectedSync: true,
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: UTCNow(),
				}}},
			},
		},
		{ // 9. existing object replication enabled, versioning enabled, replication status Pending & reset ID different
			name: "existing object replication enabled, newer reset in progress on object in Pending replication status",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:PENDING;",

				ReplicationStatus: replication.Pending,
				VersionID:         "a3348c34-c352-4498-82f0-1098e8b34df9",
				UserDefined:       map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;%s", UTCNow().AddDate(0, 0, -1).Format(http.TimeFormat), "abc")},
				ModTime:           UTCNow().AddDate(0, 0, -2),
			},
			expectedSync: true,
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: UTCNow(),
				}}},
			},
		},
		{ // 10. existing object replication enabled, versioning enabled, replication status Complete & reset done
			name: "reset done on object in Completed Status - ineligbile for re-replication",
			info: ObjectInfo{
				Size:                      100,
				ReplicationStatusInternal: "arn1:COMPLETED;",
				ReplicationStatus:         replication.Completed,
				VersionID:                 "a3348c34-c352-4498-82f0-1098e8b34df9",
				UserDefined:               map[string]string{xhttp.MinIOReplicationResetStatus: fmt.Sprintf("%s;%s", start.Format(http.TimeFormat), "xyz")},
			},
			expectedSync: false,
			dsc:          ReplicateDecision{targetsMap: map[string]replicateTargetDecision{"arn1": newReplicateTargetDecision("arn1", true, false)}},
			rcfg: replicationConfig{
				remotes: &madmin.BucketTargets{Targets: []madmin.BucketTarget{{
					Arn:             "arn1",
					ResetID:         "xyz",
					ResetBeforeDate: start,
				}}},
			},
		},
	}
)

func TestReplicationResyncwrapper(t *testing.T) {
	for i, test := range replicationConfigTests2 {
		if sync := test.rcfg.resync(test.info, test.dsc, test.tgtStatuses); sync.mustResync() != test.expectedSync {
			t.Errorf("%s (%s): Replicationresync  got %t , want %t", fmt.Sprintf("Test%d - %s", i+1, time.Now().Format(http.TimeFormat)), test.name, sync.mustResync(), test.expectedSync)
		}
	}
}

func TestReplicationValidationObjectUsesRulePrefix(t *testing.T) {
	tests := []struct {
		name string
		rule replication.Rule
		want string
	}{
		{name: "empty prefix", rule: replication.Rule{}, want: path.Join(minioReservedBucket, globalLocalNodeNameHex, "deleteme")},
		{name: "filter prefix", rule: replication.Rule{Filter: replication.Filter{Prefix: "data/"}}, want: path.Join("data", minioReservedBucket, globalLocalNodeNameHex, "deleteme")},
		{name: "and prefix", rule: replication.Rule{Filter: replication.Filter{And: replication.And{Prefix: "archive/"}}}, want: path.Join("archive", minioReservedBucket, globalLocalNodeNameHex, "deleteme")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := replicationValidationObject(test.rule); got != test.want {
				t.Fatalf("replicationValidationObject() = %q, want %q", got, test.want)
			}
		})
	}
}

// The resync-finalization tests below exercise the real result sink, the
// finish() shutdown ordering, and the sendResyncResult / finalResyncStatus
// helpers, plus (for the persistence cases) markStatus with on-disk
// round-tripping. resyncBucket cannot be driven end to end in a unit test
// because its workers call a live remote target (StatObject), so the helpers it
// uses are exercised directly. The blocking-order assertions run under
// testing/synctest so a removed wait fails deterministically, with no timing
// windows.

func newTestResyncer(bucket, arn string) (*replicationResyncer, resyncOpts) {
	s := &replicationResyncer{
		statusMap:      map[string]BucketReplicationResyncStatus{},
		resyncCancelCh: make(chan struct{}, resyncWorkerCnt),
	}
	brs := newBucketResyncStatus(bucket)
	brs.TargetsMap[arn] = TargetReplicationResyncStatus{ResyncStatus: ResyncStarted}
	s.statusMap[bucket] = brs
	return s, resyncOpts{bucket: bucket, arn: arn, resyncID: "reset-" + bucket}
}

// TestResyncBucketFinalize round-trips the terminal status through a real
// ObjectLayer: a clean run persists Completed with every result, while a run
// whose parent context was canceled during the drain, or in which a worker
// dropped a result on the cancel signal, is downgraded to Failed so a persisted
// Completed never misrepresents an incomplete resync.
func TestResyncBucketFinalize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objAPI, fsDirs, err := prepareErasure16(ctx)
	if err != nil {
		t.Fatalf("prepare erasure backend: %v", err)
	}
	defer removeRoots(fsDirs)

	// persistTerminal applies resyncBucket's finalizer logic (finalResyncStatus
	// then markStatus, which persists) and reads the status back the way the
	// resync status API does.
	persistTerminal := func(t *testing.T, s *replicationResyncer, opts resyncOpts, status ResyncStatusType, ctxErr error, aborted bool) TargetReplicationResyncStatus {
		t.Helper()
		s.markStatus(finalResyncStatus(status, ctxErr, aborted), opts, objAPI)
		brs, err := loadBucketResyncMetadata(ctx, opts.bucket, objAPI)
		if err != nil {
			t.Fatalf("load persisted resync metadata: %v", err)
		}
		return brs.TargetsMap[opts.arn]
	}

	// 1. Clean completion: every result - including the failed object - is folded
	//    into the persisted status, which stays Completed.
	t.Run("persists complete counts", func(t *testing.T) {
		s, opts := newTestResyncer("finalize-counts", "arn1")
		results := s.newResyncResults(opts)
		results.ch <- TargetReplicationResyncStatus{Object: "ok-1", ReplicatedCount: 1, ReplicatedSize: 100}
		results.ch <- TargetReplicationResyncStatus{Object: "ok-2", ReplicatedCount: 1, ReplicatedSize: 200}
		results.ch <- TargetReplicationResyncStatus{Object: "bad", FailedCount: 1, FailedSize: 300}

		var wg sync.WaitGroup // no producer workers for this case
		results.finish(nil, &wg)

		st := persistTerminal(t, s, opts, ResyncCompleted, nil, false)
		if st.ResyncStatus != ResyncCompleted {
			t.Fatalf("persisted status = %s, want Completed", st.ResyncStatus)
		}
		if st.ReplicatedCount != 2 || st.ReplicatedSize != 300 || st.FailedCount != 1 || st.FailedSize != 300 {
			t.Fatalf("persisted counts = {replicated:%d/%d failed:%d/%d}, want {2/300 1/300}",
				st.ReplicatedCount, st.ReplicatedSize, st.FailedCount, st.FailedSize)
		}
	})

	// 2. Parent context canceled during the drain -> Completed downgraded to
	//    Failed (markStatus persists under its own context, so nothing else stops
	//    a bare Completed from being recorded).
	t.Run("parent cancel during drain downgrades to failed", func(t *testing.T) {
		s, opts := newTestResyncer("finalize-parent-cancel", "arn1")
		results := s.newResyncResults(opts)
		results.ch <- TargetReplicationResyncStatus{Object: "ok-1", ReplicatedCount: 1, ReplicatedSize: 100}
		var wg sync.WaitGroup
		results.finish(nil, &wg)

		cctx, ccancel := context.WithCancel(context.Background())
		ccancel()
		st := persistTerminal(t, s, opts, ResyncCompleted, cctx.Err(), false)
		if st.ResyncStatus != ResyncFailed {
			t.Fatalf("persisted status = %s, want Failed (parent canceled during drain)", st.ResyncStatus)
		}
	})

	// 3. A worker dropped a computed result on the resync-cancel token (parent
	//    still alive) -> sendResyncResult records the abort and Completed is
	//    downgraded to Failed.
	t.Run("worker abort downgrades to failed", func(t *testing.T) {
		s, opts := newTestResyncer("finalize-worker-abort", "arn1")
		s.resyncCancelCh <- struct{}{}                 // cancel token waiting
		ch := make(chan TargetReplicationResyncStatus) // no reader: the send would block
		var aborted atomic.Bool
		if s.sendResyncResult(context.Background(), ch, TargetReplicationResyncStatus{Object: "dropped", ReplicatedCount: 1}, &aborted) {
			t.Fatal("sendResyncResult reported success despite the cancel token")
		}
		if !aborted.Load() {
			t.Fatal("worker abort was not recorded")
		}
		st := persistTerminal(t, s, opts, ResyncCompleted, nil, aborted.Load())
		if st.ResyncStatus != ResyncFailed {
			t.Fatalf("persisted status = %s, want Failed (worker dropped a result)", st.ResyncStatus)
		}
	})
}

// TestResyncFinishDrainsResults asserts finish() does not return until the
// consumer has applied the final result (the #136 defect). A gated apply holds
// the last result unapplied; under synctest finish() must stay durably blocked
// until it is released - if rr.wg.Wait() is removed, finish() returns early and
// the test fails deterministically.
func TestResyncFinishDrainsResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, opts := newTestResyncer("drain", "arn1")
		reachedFinal := make(chan struct{})
		release := make(chan struct{})
		results := startResyncResults(func(r TargetReplicationResyncStatus) {
			if r.Object == "final" {
				close(reachedFinal)
				<-release
			}
			s.incStats(r, opts)
		})
		results.ch <- TargetReplicationResyncStatus{Object: "ok-1", ReplicatedCount: 1, ReplicatedSize: 100}
		results.ch <- TargetReplicationResyncStatus{Object: "final", FailedCount: 1, FailedSize: 200}
		<-reachedFinal // consumer received "final" but is gated before incStats(final)

		var wg sync.WaitGroup
		finishDone := make(chan struct{})
		go func() {
			results.finish(nil, &wg)
			close(finishDone)
		}()

		synctest.Wait()
		select {
		case <-finishDone:
			close(release)
			synctest.Wait()
			t.Fatal("finish() returned before the final result was drained (drain wait missing)")
		default:
			// finish() is durably blocked in rr.wg.Wait() - correct.
		}

		close(release)
		synctest.Wait()
		<-finishDone
		st := s.statusMap[opts.bucket].TargetsMap[opts.arn]
		if st.ReplicatedCount != 1 || st.FailedCount != 1 || st.FailedSize != 200 {
			t.Fatalf("status after finish = {replicated:%d failed:%d/%d}, want {1 1/200}",
				st.ReplicatedCount, st.FailedCount, st.FailedSize)
		}
	})
}

// TestResyncFinishWaitsForInflightWorker asserts finish() stops the producer
// workers before it closes the result channel, so an in-flight worker (as on an
// early-return path) never sends on a closed channel and its result is not lost.
// A gated worker stays in flight past the shutdown request; under synctest
// finish() must stay durably blocked until the worker is released - if
// workerWg.Wait() is removed, finish() returns early and the test fails
// deterministically.
func TestResyncFinishWaitsForInflightWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, opts := newTestResyncer("workers", "arn1")
		results := startResyncResults(func(r TargetReplicationResyncStatus) { s.incStats(r, opts) })

		workers := []chan ReplicateObjectInfo{make(chan ReplicateObjectInfo, 1)}
		var wg sync.WaitGroup
		gotRoi := make(chan struct{})
		release := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for roi := range workers[0] {
				close(gotRoi)
				<-release
				// Mirror the real worker's send; recover so that if finish()
				// wrongly closed the result channel first, the test fails via the
				// assertion below instead of crashing on send-on-closed.
				func() {
					defer func() { _ = recover() }()
					results.ch <- TargetReplicationResyncStatus{Object: roi.Name, ReplicatedCount: 1, ReplicatedSize: 500}
				}()
			}
		}()
		workers[0] <- ReplicateObjectInfo{Name: "inflight"}
		<-gotRoi // worker holds a result in flight, not yet delivered

		finishDone := make(chan struct{})
		go func() {
			results.finish(workers, &wg)
			close(finishDone)
		}()

		synctest.Wait()
		select {
		case <-finishDone:
			close(release)
			synctest.Wait()
			t.Fatal("finish() closed the result channel before the in-flight worker finished (worker wait missing)")
		default:
			// finish() is durably blocked in workerWg.Wait() - correct.
		}

		close(release)
		synctest.Wait()
		<-finishDone
		st := s.statusMap[opts.bucket].TargetsMap[opts.arn]
		if st.ReplicatedCount != 1 || st.ReplicatedSize != 500 {
			t.Fatalf("status after finish = {replicated:%d/%d}, want {1/500}", st.ReplicatedCount, st.ReplicatedSize)
		}
	})
}

// TestResyncResultFor asserts the resync worker classifies a target from the
// actual replication outcome, not from whether the target version merely exists.
// The key regression is the "failed update over an existing version" case: a
// quota-rejected update leaves the old version in place, and counting existence
// (the previous behavior) would score it a success. It also checks a genuine
// success, an errored-but-Completed result, a delete failure, a delete-marker
// success (zero bytes), and an ARN that was never attempted.
func TestResyncResultFor(t *testing.T) {
	const arn = "arn:minio:replication::id:bucket"
	obj := ReplicateObjectInfo{Name: "obj", Bucket: "bucket", Size: 196608}
	deleteMarker := ReplicateObjectInfo{Name: "dm", Bucket: "bucket", Size: 0, DeleteMarker: true}

	tests := []struct {
		name                                           string
		roi                                            ReplicateObjectInfo
		rinfos                                         replicatedInfos
		wantRepl, wantReplSize, wantFail, wantFailSize int64
	}{
		{
			name: "completed update",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Completed, Size: 196608},
			}},
			wantRepl: 1, wantReplSize: 196608,
		},
		{
			name: "failed update over existing version",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Failed, Err: fmt.Errorf("quota exceeded"), Size: 196608},
			}},
			wantFail: 1, wantFailSize: 196608,
		},
		{
			name: "completed but errored is a failure",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Completed, Err: fmt.Errorf("boom"), Size: 196608},
			}},
			wantFail: 1, wantFailSize: 196608,
		},
		{
			name: "delete failed",
			roi:  deleteMarker,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Failed},
			}},
			wantFail: 1, wantFailSize: 0,
		},
		{
			name: "delete marker replicated counts zero bytes",
			roi:  deleteMarker,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Completed},
			}},
			wantRepl: 1, wantReplSize: 0,
		},
		{
			name: "arn not attempted is a failure",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: "arn:minio:replication::id2:bucket", ReplicationStatus: replication.Completed, Size: 196608},
			}},
			wantFail: 1, wantFailSize: 196608,
		},
		{
			name: "completed with zero size falls back to object size",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, ReplicationStatus: replication.Completed, Size: 0},
			}},
			wantRepl: 1, wantReplSize: 196608,
		},
		{
			name: "version purge complete is a success",
			roi:  ReplicateObjectInfo{Name: "purge", Bucket: "bucket", Size: 196608, VersionPurgeStatus: replication.VersionPurgePending},
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				// a successful purge sets only VersionPurgeStatus; ReplicationStatus stays empty.
				{Arn: arn, VersionPurgeStatus: replication.VersionPurgeComplete},
			}},
			wantRepl: 1, wantReplSize: 196608,
		},
		{
			name: "version purge failed is a failure",
			roi:  ReplicateObjectInfo{Name: "purge", Bucket: "bucket", Size: 196608, VersionPurgeStatus: replication.VersionPurgePending},
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				{Arn: arn, VersionPurgeStatus: replication.VersionPurgeFailed, Err: fmt.Errorf("quota exceeded")},
			}},
			wantFail: 1, wantFailSize: 196608,
		},
		{
			name: "benign duplicate 412 is a success",
			roi:  obj,
			rinfos: replicatedInfos{Targets: []replicatedTargetInfo{
				// the destination answers PreconditionFailed for an exact duplicate;
				// replicateAll keeps Completed but retains the error.
				{Arn: arn, ReplicationStatus: replication.Completed, Err: minio.ErrorResponse{Code: "PreconditionFailed"}, Size: 196608},
			}},
			wantRepl: 1, wantReplSize: 196608,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := resyncResultFor(tc.rinfos, arn, tc.roi)
			if st.Object != tc.roi.Name || st.Bucket != tc.roi.Bucket {
				t.Fatalf("object/bucket = %s/%s, want %s/%s", st.Object, st.Bucket, tc.roi.Name, tc.roi.Bucket)
			}
			if st.ReplicatedCount != tc.wantRepl || st.ReplicatedSize != tc.wantReplSize ||
				st.FailedCount != tc.wantFail || st.FailedSize != tc.wantFailSize {
				t.Fatalf("resyncResultFor = {replicated:%d/%d failed:%d/%d}, want {%d/%d %d/%d}",
					st.ReplicatedCount, st.ReplicatedSize, st.FailedCount, st.FailedSize,
					tc.wantRepl, tc.wantReplSize, tc.wantFail, tc.wantFailSize)
			}
		})
	}
}

// TestObjectNeedsResyncForARN asserts the resync dispatch is scoped to the
// target being resynced. The worker pool runs for a single target (opts.arn),
// so an object that only qualifies for a different target must be skipped: with
// A/B rules and a resync of A, an object that needs replication only for B must
// not be admitted to A's worker. Otherwise (after outcome-based classification)
// A would be absent from that object's result and miscounted as an A failure.
func TestObjectNeedsResyncForARN(t *testing.T) {
	const (
		arnA = "arn:minio:replication::id:bucket"
		arnB = "arn:minio:replication::id2:bucket"
	)
	tests := []struct {
		name     string
		decision ResyncDecision
		arn      string
		want     bool
	}{
		{
			name:     "target must resync",
			decision: ResyncDecision{targets: map[string]ResyncTargetDecision{arnA: {Replicate: true}}},
			arn:      arnA,
			want:     true,
		},
		{
			name:     "object qualifies for B only, resyncing A",
			decision: ResyncDecision{targets: map[string]ResyncTargetDecision{arnB: {Replicate: true}}},
			arn:      arnA,
			want:     false,
		},
		{
			name: "A present but not replicating, B replicating, resyncing A",
			decision: ResyncDecision{targets: map[string]ResyncTargetDecision{
				arnA: {Replicate: false},
				arnB: {Replicate: true},
			}},
			arn:  arnA,
			want: false,
		},
		{
			name: "object qualifies for both, resyncing A",
			decision: ResyncDecision{targets: map[string]ResyncTargetDecision{
				arnA: {Replicate: true},
				arnB: {Replicate: true},
			}},
			arn:  arnA,
			want: true,
		},
		{
			name:     "no resync decision",
			decision: ResyncDecision{},
			arn:      arnA,
			want:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roi := ReplicateObjectInfo{Name: "obj", Bucket: "bucket", ExistingObjResync: tc.decision}
			if got := objectNeedsResyncForARN(roi, tc.arn); got != tc.want {
				t.Fatalf("objectNeedsResyncForARN(arn=%s) = %v, want %v", tc.arn, got, tc.want)
			}
		})
	}
}
