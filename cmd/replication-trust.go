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
	"context"
	"net/http"

	objectreplication "github.com/minio/minio/internal/bucket/replication"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/pkg/v3/policy"
)

type replicationTrustKey struct{}
type replicaTrustKey struct{}

// hasReplicationMarker reports whether the internal replication marker has
// its one accepted wire value. Header presence alone is never a trust signal.
func hasReplicationMarker(h http.Header) bool {
	values, ok := h[http.CanonicalHeaderKey(xhttp.MinIOSourceReplicationRequest)]
	return ok && len(values) == 1 && values[0] == "true"
}

func hasReplicationMarkerHeader(h http.Header) bool {
	_, ok := h[http.CanonicalHeaderKey(xhttp.MinIOSourceReplicationRequest)]
	return ok
}

func hasReplicaStatus(h http.Header) bool {
	return h.Get(xhttp.AmzBucketReplicationStatus) == objectreplication.Replica.String()
}

func withReplicationTrust(ctx context.Context, trusted, replicaTrusted bool) context.Context {
	ctx = context.WithValue(ctx, replicationTrustKey{}, trusted)
	return context.WithValue(ctx, replicaTrustKey{}, trusted && replicaTrusted)
}

func isTrustedReplication(ctx context.Context) bool {
	trusted, _ := ctx.Value(replicationTrustKey{}).(bool)
	return trusted
}

func isReplicaTrusted(ctx context.Context) bool {
	trusted, _ := ctx.Value(replicaTrustKey{}).(bool)
	return trusted
}

// replicationPermissionAllowed must be called only after the request's
// existing authentication/signature path has succeeded and populated ReqInfo.
// Replication peers are authenticated principals; an anonymous bucket-policy
// grant must not turn client-controlled internal headers into trusted state.
func replicationPermissionAllowed(ctx context.Context, r *http.Request, bucket, object string, action policy.Action) bool {
	reqInfo := logger.GetReqInfo(ctx)
	if reqInfo == nil || reqInfo.Cred.AccessKey == "" {
		return false
	}
	reqInfo.BucketName = bucket
	reqInfo.ObjectName = object
	return authorizeRequest(ctx, r, action) == ErrNone
}

// replicationRequestHeaders are internal request controls. They are removed
// only after signature verification when a request has not earned replication
// trust. Public S3/SSE/checksum headers, proxy loop guards, and replication
// validity/readiness probes are intentionally not listed here.
var replicationRequestHeaders = []string{
	xhttp.MinIOSourceReplicationRequest,
	xhttp.MinIOSourceETag,
	xhttp.MinIOSourceMTime,
	xhttp.MinIOSourceDeleteMarker,
	xhttp.MinIOSourceDeleteMarkerDelete,
	xhttp.MinIOSourceTaggingTimestamp,
	xhttp.MinIOSourceObjectRetentionTimestamp,
	xhttp.MinIOSourceObjectLegalHoldTimestamp,
	"X-Minio-Replication-Server-Side-Encryption-Sealed-Key",
	"X-Minio-Replication-Server-Side-Encryption-Seal-Algorithm",
	"X-Minio-Replication-Server-Side-Encryption-Iv",
	"X-Minio-Replication-Encrypted-Multipart",
	xhttp.MinIOReplicationActualObjectSize,
	ReplicationSsecChecksumHeader,
	xhttp.AmzBucketReplicationStatus,
}

func stripReplicationRequestHeaders(h http.Header) {
	for _, name := range replicationRequestHeaders {
		h.Del(name)
	}
}

func hasReplicationRequestHeaders(h http.Header) bool {
	for _, name := range replicationRequestHeaders {
		if _, ok := h[http.CanonicalHeaderKey(name)]; ok {
			return true
		}
	}
	return false
}

func cloneRequestWithoutReplicationHeaders(r *http.Request, ctx context.Context) *http.Request {
	clone := r.Clone(ctx)
	stripReplicationRequestHeaders(clone.Header)
	return clone
}

// applyReplicationTrust binds the handler context to the effective request.
// The context marker is the authorization source of truth; header removal is
// defense in depth for option builders and future call sites.
func applyReplicationTrust(ctx context.Context, r *http.Request, trusted, replicaTrusted bool) (context.Context, *http.Request) {
	ctx = withReplicationTrust(ctx, trusted, replicaTrusted)
	if trusted {
		return ctx, r.WithContext(ctx)
	}
	return ctx, cloneRequestWithoutReplicationHeaders(r, ctx)
}
