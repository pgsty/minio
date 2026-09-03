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
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	humanize "github.com/dustin/go-humanize"
	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio/internal/bucket/cors"
	hashpkg "github.com/minio/minio/internal/hash"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/mux"
	"github.com/pgsty/silo-pkg/v3/policy"
)

// maxBucketCorsSize is the maximum allowed size of a CORS configuration document.
const maxBucketCorsSize = 64 * humanize.KiByte

// PutBucketCorsHandler - PUT bucket cors.
func (api objectAPIHandlers) PutBucketCorsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "PutBucketCors")

	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))

	objAPI := api.ObjectAPI()
	if objAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	vars := mux.Vars(r)
	bucket := vars["bucket"]

	if s3Error := checkRequestAuthType(ctx, r, policy.PutBucketCorsAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	if _, err := objAPI.GetBucketInfo(ctx, bucket, BucketOptions{}); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	if r.ContentLength <= 0 {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentLength), r.URL)
		return
	}
	if r.ContentLength > maxBucketCorsSize {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrEntityTooLarge), r.URL)
		return
	}

	// PutBucketCors requires a Content-Md5 or a supported full-header
	// checksum. validateLengthAndChecksum wraps r.Body so the supplied digest
	// is verified as the body is read below.
	if !validateLengthAndChecksum(r) {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMissingContentMD5), r.URL)
		return
	}

	corsBytes, err := io.ReadAll(r.Body)
	if err != nil {
		if errors.Is(err, hashpkg.ErrInvalidChecksum) {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrBadDigest), r.URL)
			return
		}
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	corsCfg, err := cors.ParseBucketCorsConfig(bytes.NewReader(corsBytes))
	if err != nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrMalformedXML), r.URL)
		return
	}
	if err := corsCfg.Validate(); err != nil {
		writeErrorResponse(ctx, w, APIError{
			Code:           "MalformedXML",
			HTTPStatusCode: http.StatusBadRequest,
			Description:    err.Error(),
		}, r.URL)
		return
	}

	updatedAt, err := updateLocalBucketCORSMetadata(ctx, objAPI, bucket, corsBytes)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	// Call site replication hook.
	//
	// We encode the xml bytes as base64 to ensure there are no encoding
	// errors.
	cfgStr := base64.StdEncoding.EncodeToString(corsBytes)
	replLogIf(ctx, globalSiteReplicationSys.BucketMetaHook(ctx, madmin.SRBucketMeta{
		Type:      madmin.SRBucketMetaTypeCorsConfig,
		Bucket:    bucket,
		Cors:      &cfgStr,
		UpdatedAt: updatedAt,
	}))

	writeSuccessResponseHeadersOnly(w)
}

// GetBucketCorsHandler - GET bucket cors.
func (api objectAPIHandlers) GetBucketCorsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "GetBucketCors")

	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))

	objAPI := api.ObjectAPI()
	if objAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	vars := mux.Vars(r)
	bucket := vars["bucket"]

	if s3Error := checkRequestAuthType(ctx, r, policy.GetBucketCorsAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	if _, err := objAPI.GetBucketInfo(ctx, bucket, BucketOptions{}); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	configData, _, err := globalBucketMetadataSys.GetCorsConfigXML(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchCORSConfiguration), r.URL)
			return
		}
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	writeSuccessResponseXML(w, configData)
}

// DeleteBucketCorsHandler - DELETE bucket cors.
func (api objectAPIHandlers) DeleteBucketCorsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "DeleteBucketCors")

	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))

	objAPI := api.ObjectAPI()
	if objAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	vars := mux.Vars(r)
	bucket := vars["bucket"]

	if s3Error := checkRequestAuthType(ctx, r, policy.DeleteBucketCorsAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	if _, err := objAPI.GetBucketInfo(ctx, bucket, BucketOptions{}); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	updatedAt, err := updateLocalBucketCORSMetadata(ctx, objAPI, bucket, nil)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	replLogIf(ctx, globalSiteReplicationSys.BucketMetaHook(ctx, madmin.SRBucketMeta{
		Type:      madmin.SRBucketMetaTypeCorsConfig,
		Bucket:    bucket,
		Cors:      nil,
		UpdatedAt: updatedAt,
	}))

	writeSuccessNoContent(w)
}
