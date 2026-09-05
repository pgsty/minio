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
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio/internal/auth"
	"github.com/minio/minio/internal/crypto"
	xhttp "github.com/minio/minio/internal/http"
)

// TestAPISSECReplicationTargetHead pins what the replication sender's target
// HEAD sees for an SSE-C object, which is what replicateAll's dispatch relies
// on: a keyless HEAD answers 400 InvalidRequest (so the sender must retransmit
// rather than compare), a missing key still answers 404 NoSuchKey (so a missing
// replica keeps healing), a HEAD carrying the internal replication marker
// answers with the replica metadata (what the resync accounting HEAD now
// sends), and the metadata-only CopyObject the sender used to fall into fails
// with ExcessData on any non-empty object. See pgsty/silo#120.
func TestAPISSECReplicationTargetHead(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPISSECReplicationTargetHead,
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPISSECReplicationTargetHead(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	previousTLS := globalIsTLS
	globalIsTLS = true
	defer func() { globalIsTLS = previousTLS }()

	key := bytes.Repeat([]byte{0x42}, 32)
	keyMD5 := md5.Sum(key)
	data := bytes.Repeat([]byte("ssec-keyless-head-"), 512)
	sseHeaders := map[string]string{
		xhttp.AmzServerSideEncryptionCustomerAlgorithm: xhttp.AmzEncryptionAES,
		xhttp.AmzServerSideEncryptionCustomerKey:       base64.StdEncoding.EncodeToString(key),
		xhttp.AmzServerSideEncryptionCustomerKeyMD5:    base64.StdEncoding.EncodeToString(keyMD5[:]),
	}
	object := "ssec-keyless-head/replica"
	putCopyChecksumSource(t, apiRouter, credentials, bucketName, object, data, sseHeaders)

	// The replication target credential holds the standard replication actions.
	replicator := newObjectAttributesAuthzUser(t, instanceType, bucketName,
		`"s3:GetObject","s3:PutObject","s3:ReplicateObject","s3:ReplicateDelete","s3:ReplicateTags"`)

	// Exactly the header set replicateAll's StatObject sends today.
	senderHeaders := map[string]string{
		"X-Minio-Source-Proxy-Request": "false",
		xhttp.AmzTagDirective:          "ACCESS",
	}
	// The same request with the internal replication marker added.
	markedHeaders := map[string]string{
		"X-Minio-Source-Proxy-Request":      "false",
		xhttp.AmzTagDirective:               "ACCESS",
		xhttp.MinIOSourceReplicationRequest: "true",
	}

	head := func(t *testing.T, creds auth.Credentials, obj, versionID string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		headURL := getGetObjectURL("", bucketName, obj)
		if versionID != "" {
			// replicateAll addresses the source version (minio-go
			// api-get-options.go toQueryValues).
			headURL += "?versionId=" + versionID
		}
		req, err := newTestSignedRequestV4(http.MethodHead, headURL, 0, nil,
			creds.AccessKey, creds.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	sseDesc := errorCodes[ErrSSEEncryptedObject].Description

	baseInfo, err := obj.GetObjectInfo(t.Context(), bucketName, object, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sourceVersion := baseInfo.VersionID

	t.Run("sender-head-today-is-rejected", func(t *testing.T) {
		rec := head(t, replicator, object, sourceVersion, senderHeaders)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: keyless HEAD status %d, want 400", instanceType, rec.Code)
		}
		if got := rec.Header().Get("x-minio-error-code"); got != "InvalidRequest" {
			t.Fatalf("%s: error code %q, want InvalidRequest", instanceType, got)
		}
		desc := strings.Trim(rec.Header().Get("x-minio-error-desc"), `"`)
		if !strings.Contains(desc, sseDesc) {
			t.Fatalf("%s: error desc %q does not carry %q", instanceType, desc, sseDesc)
		}
		t.Logf("%s: keyless HEAD -> %d %s / %s", instanceType, rec.Code,
			rec.Header().Get("x-minio-error-code"), desc)
	})

	t.Run("missing-object-head-is-distinguishable", func(t *testing.T) {
		rec := head(t, replicator, "ssec-keyless-head/absent", "", senderHeaders)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: missing-object HEAD status %d, want 404", instanceType, rec.Code)
		}
		if got := rec.Header().Get("x-minio-error-code"); got != "NoSuchKey" {
			t.Fatalf("%s: missing-object error code %q, want NoSuchKey", instanceType, got)
		}
	})

	t.Run("marked-head-answers-with-metadata", func(t *testing.T) {
		rec := head(t, replicator, object, sourceVersion, markedHeaders)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: marked keyless HEAD status %d, want 200: %s", instanceType, rec.Code, rec.Body.String())
		}
		// setObjectHeaders assigns the ETag through the raw header map with the
		// non canonical key "ETag", so read it the same way.
		etag := ""
		if v := rec.Header()[xhttp.ETag]; len(v) > 0 {
			etag = strings.Trim(v[0], `"`)
		}
		clen := rec.Header().Get(xhttp.ContentLength)
		lastMod := rec.Header().Get(xhttp.LastModified)
		if etag == "" || clen == "" || lastMod == "" {
			t.Fatalf("%s: marked HEAD lacks comparison metadata etag=%q len=%q mtime=%q",
				instanceType, etag, clen, lastMod)
		}

		// What replicateAll would compare this against: the source ObjectInfo it
		// obtained from GetObjectNInfo(..., ReplicationRequest: true).
		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		srcInfo := gr.ObjInfo
		gr.Close()
		srcSize, err := srcInfo.GetActualSize()
		if err != nil {
			t.Fatal(err)
		}
		headSize, err := strconv.ParseInt(clen, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: source ETag=%q (len %d) size=%d ; target HEAD ETag=%q (len %d) size=%d",
			instanceType, srcInfo.ETag, len(srcInfo.ETag), srcSize, etag, len(etag), headSize)
		if headSize != srcSize {
			t.Errorf("%s: getReplicationAction size mismatch: source %d target %d", instanceType, srcSize, headSize)
		}
		if srcInfo.ETag != etag {
			t.Logf("%s: NOTE getReplicationAction would see an ETag mismatch (source keeps the sealed ETag, "+
				"the target HEAD returns the last 32 bytes) and therefore return replicateAll", instanceType)
		}
	})

	t.Run("zero-byte-metadata-copy-succeeds", func(t *testing.T) {
		// A zero-byte SSE-C object has nothing for the plaintext-sized reader to
		// overrun, so the same copy request succeeds. The ExcessData failure is a
		// property of non-empty objects.
		zero := "ssec-keyless-head/zero"
		putCopyChecksumSource(t, apiRouter, credentials, bucketName, zero, nil, sseHeaders)
		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, zero, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		zi := gr.ObjInfo
		gr.Close()

		copySrc := url.QueryEscape(SlashSeparator+bucketName+SlashSeparator+zero) + "?versionId=" + zi.VersionID
		headers := map[string]string{
			xhttp.AmzCopySource:                 copySrc,
			xhttp.MinIOSourceReplicationRequest: "true",
		}
		for k, v := range getCopyObjMetadata(zi, "") {
			if strings.EqualFold(k, "content-length") {
				continue
			}
			headers[k] = v
		}
		headers[xhttp.AmzObjectTagging] = "keyless-head=zero"
		req, err := newTestSignedRequestV4(http.MethodPut,
			getCopyObjectURL("", bucketName, zero)+"?versionId="+zi.VersionID, 0, nil,
			replicator.AccessKey, replicator.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		after, gerr := obj.GetObjectInfo(t.Context(), bucketName, zero, ObjectOptions{})
		if gerr != nil {
			t.Fatal(gerr)
		}
		t.Logf("%s: zero-byte metadata CopyObject -> %d; stored SSE-C=%v size=%d tags=%q",
			instanceType, rec.Code, crypto.SSEC.IsEncrypted(after.UserDefined), after.Size, after.UserTags)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: zero-byte metadata CopyObject status %d: %s", instanceType, rec.Code, rec.Body.String())
		}
	})

	t.Run("metadata-copy-is-what-resync-runs", func(t *testing.T) {
		// Exactly what replicateAll runs at cmd/bucket-replication.go:1582 once
		// the keyless HEAD has been misread: a same bucket, same key CopyObject
		// built from getCopyObjMetadata plus the replication marker, carrying no
		// customer key. getCopyObjMetadata already sets REPLICA status and
		// x-amz-tagging-directive: REPLACE, so the request is replica trusted.
		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		srcInfo := gr.ObjInfo
		gr.Close()

		// minio-go addresses the source version on the copy source and in the
		// destination query (api-compose-object.go:307,314).
		copySrc := url.QueryEscape(SlashSeparator+bucketName+SlashSeparator+object) + "?versionId=" + srcInfo.VersionID
		headers := map[string]string{
			xhttp.AmzCopySource:                 copySrc,
			xhttp.MinIOSourceReplicationRequest: "true",
		}
		copyMeta := getCopyObjMetadata(srcInfo, "")
		for k, v := range copyMeta {
			// net/http derives the request body length from a literal
			// Content-Length header; minio-go relies on req.ContentLength, so
			// drop it here to keep the in-process request faithful.
			if strings.EqualFold(k, "content-length") {
				t.Logf("%s: dropping content-length=%q from the copy metadata", instanceType, v)
				continue
			}
			headers[k] = v
		}
		t.Logf("%s: copy metadata keys: %v", instanceType, slices.Sorted(maps.Keys(copyMeta)))
		copyURL := getCopyObjectURL("", bucketName, object) + "?versionId=" + srcInfo.VersionID
		req, err := newTestSignedRequestV4(http.MethodPut, copyURL, 0, nil,
			replicator.AccessKey, replicator.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		t.Logf("%s: replication metadata CopyObject (version %s) -> %d %s", instanceType, srcInfo.VersionID, rec.Code,
			strings.ReplaceAll(rec.Body.String(), "\n", " "))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: metadata CopyObject status %d, want 400", instanceType, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<Code>ExcessData</Code>") {
			t.Fatalf("%s: metadata CopyObject did not fail with ExcessData: %s", instanceType, rec.Body.String())
		}

		// Whatever the status, the object must still read back with the key.
		greq, gerr := newTestSignedRequestV4(http.MethodGet, getGetObjectURL("", bucketName, object), 0, nil,
			credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if gerr != nil {
			t.Fatal(gerr)
		}
		grec := httptest.NewRecorder()
		apiRouter.ServeHTTP(grec, greq)
		if grec.Code != http.StatusOK || !bytes.Equal(grec.Body.Bytes(), data) {
			t.Errorf("%s: after the replication metadata CopyObject the object no longer reads back: %d (%d bytes)",
				instanceType, grec.Code, grec.Body.Len())
		} else {
			t.Logf("%s: object still reads back correctly with the customer key", instanceType)
		}
	})
}

// TestAPISSECReplicaRetransmitOverExistingVersion asserts that a full
// retransmit of an SSE-C object reaches the destination when the replica
// already exists with the source version and ETag. checkPreconditionsPUT used
// to reject a matching PreserveETag plus VersionID with 412 for the multipart
// path (the single-part sealed ETag is truncated before the comparison), and
// the sender turns 412 into success, so no part was ever sent and an SSE-C
// replica could never be repaired or updated. See pgsty/silo#120.
func TestAPISSECReplicaRetransmitOverExistingVersion(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPISSECReplicaRetransmitOverExistingVersion,
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPISSECReplicaRetransmitOverExistingVersion(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	previousTLS := globalIsTLS
	globalIsTLS = true
	defer func() { globalIsTLS = previousTLS }()

	key := bytes.Repeat([]byte{0x43}, 32)
	keyMD5 := md5.Sum(key)
	sseHeaders := map[string]string{
		xhttp.AmzServerSideEncryptionCustomerAlgorithm: xhttp.AmzEncryptionAES,
		xhttp.AmzServerSideEncryptionCustomerKey:       base64.StdEncoding.EncodeToString(key),
		xhttp.AmzServerSideEncryptionCustomerKeyMD5:    base64.StdEncoding.EncodeToString(keyMD5[:]),
	}
	replicator := newObjectAttributesAuthzUser(t, instanceType, bucketName,
		`"s3:GetObject","s3:PutObject","s3:ReplicateObject"`)

	replicaHeaders := func(t *testing.T, oi ObjectInfo) map[string]string {
		t.Helper()
		opts, _, err := putReplicationOpts(t.Context(), "", oi)
		if err != nil {
			t.Fatal(err)
		}
		opts.Internal.SourceMTime = time.Time{}
		out := make(map[string]string)
		for name, values := range opts.Header() {
			if len(values) > 0 {
				out[name] = values[0]
			}
		}
		out[xhttp.MinIOSourceReplicationRequest] = "true"
		out[xhttp.AmzBucketReplicationStatus] = "REPLICA"
		out[xhttp.MinIOSourceETag] = oi.ETag
		return out
	}

	t.Run("single-part-replica-put", func(t *testing.T) {
		data := bytes.Repeat([]byte("single-part-ssec-replica-"), 400)
		object := "ssec-duplicate/single"
		putCopyChecksumSource(t, apiRouter, credentials, bucketName, object, data, sseHeaders)

		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		srcInfo := gr.ObjInfo
		cipher, err := io.ReadAll(gr)
		gr.Close()
		if err != nil {
			t.Fatal(err)
		}
		// The source changed a tag after the replica was written: the
		// retransmit must carry it onto the same version.
		srcInfo.UserTags = "retransmit=single"
		hdrs := replicaHeaders(t, srcInfo)
		putURL := getPutObjectURL("", bucketName, object) + "?versionId=" + srcInfo.VersionID
		req, err := newTestSignedRequestV4(http.MethodPut, putURL, int64(len(cipher)),
			bytes.NewReader(cipher), replicator.AccessKey, replicator.SecretKey, hdrs)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: single-part replica PUT status %d, want 200: %s", instanceType, rec.Code, rec.Body.String())
		}
		assertRetransmittedVersion(t, obj, apiRouter, credentials, bucketName, object, srcInfo.VersionID, "retransmit=single", data, sseHeaders)
	})

	t.Run("zero-byte-replica-put", func(t *testing.T) {
		object := "ssec-duplicate/zero"
		putCopyChecksumSource(t, apiRouter, credentials, bucketName, object, nil, sseHeaders)

		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		srcInfo := gr.ObjInfo
		cipher, err := io.ReadAll(gr)
		gr.Close()
		if err != nil {
			t.Fatal(err)
		}
		srcInfo.UserTags = "retransmit=zero"
		hdrs := replicaHeaders(t, srcInfo)
		putURL := getPutObjectURL("", bucketName, object) + "?versionId=" + srcInfo.VersionID
		req, err := newTestSignedRequestV4(http.MethodPut, putURL, int64(len(cipher)),
			bytes.NewReader(cipher), replicator.AccessKey, replicator.SecretKey, hdrs)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: zero-byte replica PUT status %d, want 200: %s", instanceType, rec.Code, rec.Body.String())
		}
		assertRetransmittedVersion(t, obj, apiRouter, credentials, bucketName, object, srcInfo.VersionID, "retransmit=zero", nil, sseHeaders)
	})

	t.Run("multipart-replica-newmpu", func(t *testing.T) {
		data := bytes.Repeat([]byte("multipart-ssec-replica-"), 4096)
		object := "ssec-duplicate/multipart"

		newReq, err := newTestSignedRequestV4(http.MethodPost, getNewMultipartURL("", bucketName, object),
			0, nil, credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		newRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(newRec, newReq)
		if newRec.Code != http.StatusOK {
			t.Fatalf("%s: source NewMultipart %d: %s", instanceType, newRec.Code, newRec.Body.String())
		}
		var srcInit InitiateMultipartUploadResponse
		if err = xmlDecoder(newRec.Body, &srcInit, int64(newRec.Body.Len())); err != nil {
			t.Fatal(err)
		}
		partReq, err := newTestSignedRequestV4(http.MethodPut,
			getPutObjectPartURL("", bucketName, object, srcInit.UploadID, "1"), int64(len(data)),
			bytes.NewReader(data), credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		partRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(partRec, partReq)
		if partRec.Code != http.StatusOK {
			t.Fatalf("%s: source PutPart %d: %s", instanceType, partRec.Code, partRec.Body.String())
		}
		body, err := xml.Marshal(CompleteMultipartUpload{Parts: []CompletePart{
			{PartNumber: 1, ETag: canonicalizeETag(partRec.Header()[xhttp.ETag][0])},
		}})
		if err != nil {
			t.Fatal(err)
		}
		completeReq, err := newTestSignedRequestV4(http.MethodPost,
			getCompleteMultipartUploadURL("", bucketName, object, srcInit.UploadID), int64(len(body)),
			bytes.NewReader(body), credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		completeRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(completeRec, completeReq)
		if completeRec.Code != http.StatusOK {
			t.Fatalf("%s: source Complete %d: %s", instanceType, completeRec.Code, completeRec.Body.String())
		}

		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		srcInfo := gr.ObjInfo
		gr.Close()

		srcInfo.UserTags = "retransmit=multipart"
		hdrs := replicaHeaders(t, srcInfo)
		mpuURL := getNewMultipartURL("", bucketName, object) + "&versionId=" + srcInfo.VersionID
		req, err := newTestSignedRequestV4(http.MethodPost, mpuURL, 0, nil,
			replicator.AccessKey, replicator.SecretKey, hdrs)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		t.Logf("%s: multipart replica NewMultipartUpload over the same version+ETag -> %d %s (source ETag %q, multipart=%v)",
			instanceType, rec.Code, strings.ReplaceAll(rec.Body.String(), "\n", " "),
			srcInfo.ETag, crypto.IsMultiPart(srcInfo.UserDefined))
		if rec.Code == http.StatusPreconditionFailed {
			t.Fatalf("%s: multipart replica upload short-circuited with 412; the sender turns that into "+
				"success, so no part is ever sent", instanceType)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: replica NewMultipart status %d: %s", instanceType, rec.Code, rec.Body.String())
		}

		// Finish the replica upload the way the sender does and prove the object
		// is still readable with the customer key afterwards.
		gr2, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		rawPart, err := io.ReadAll(gr2)
		gr2.Close()
		if err != nil {
			t.Fatal(err)
		}
		var replicaInit InitiateMultipartUploadResponse
		if err = xmlDecoder(rec.Body, &replicaInit, int64(rec.Body.Len())); err != nil {
			t.Fatal(err)
		}
		partReq2, err := newTestSignedRequestV4(http.MethodPut,
			getPutObjectPartURL("", bucketName, object, replicaInit.UploadID, "1"), int64(len(rawPart)),
			bytes.NewReader(rawPart), replicator.AccessKey, replicator.SecretKey,
			map[string]string{xhttp.MinIOSourceReplicationRequest: "true"})
		if err != nil {
			t.Fatal(err)
		}
		partRec2 := httptest.NewRecorder()
		apiRouter.ServeHTTP(partRec2, partReq2)
		if partRec2.Code != http.StatusOK {
			t.Fatalf("%s: replica PutPart %d: %s", instanceType, partRec2.Code, partRec2.Body.String())
		}
		actualSize, err := srcInfo.GetActualSize()
		if err != nil {
			t.Fatal(err)
		}
		completeBody2, err := xml.Marshal(CompleteMultipartUpload{Parts: []CompletePart{
			{PartNumber: 1, ETag: canonicalizeETag(partRec2.Header()[xhttp.ETag][0])},
		}})
		if err != nil {
			t.Fatal(err)
		}
		completeReq2, err := newTestSignedRequestV4(http.MethodPost,
			getCompleteMultipartUploadURL("", bucketName, object, replicaInit.UploadID), int64(len(completeBody2)),
			bytes.NewReader(completeBody2), replicator.AccessKey, replicator.SecretKey, map[string]string{
				xhttp.MinIOSourceReplicationRequest:    "true",
				xhttp.MinIOSourceMTime:                 srcInfo.ModTime.Format(time.RFC3339Nano),
				xhttp.MinIOSourceETag:                  srcInfo.ETag,
				xhttp.MinIOReplicationActualObjectSize: strconv.FormatInt(actualSize, 10),
			})
		if err != nil {
			t.Fatal(err)
		}
		completeRec2 := httptest.NewRecorder()
		apiRouter.ServeHTTP(completeRec2, completeReq2)
		if completeRec2.Code != http.StatusOK {
			t.Fatalf("%s: replica Complete %d: %s", instanceType, completeRec2.Code, completeRec2.Body.String())
		}
		assertRetransmittedVersion(t, obj, apiRouter, credentials, bucketName, object, srcInfo.VersionID, "retransmit=multipart", data, sseHeaders)
	})
}

// assertRetransmittedVersion checks that a retransmit landed on the addressed
// version: the changed tag is stored on it and it still reads back with the
// customer key.
func assertRetransmittedVersion(t *testing.T, obj ObjectLayer, apiRouter http.Handler, credentials auth.Credentials,
	bucketName, object, versionID, wantTags string, want []byte, sseHeaders map[string]string,
) {
	t.Helper()
	info, err := obj.GetObjectInfo(t.Context(), bucketName, object, ObjectOptions{VersionID: versionID})
	if err != nil {
		t.Fatalf("version %s after retransmit: %v", versionID, err)
	}
	if info.UserTags != wantTags {
		t.Errorf("version %s tags after retransmit %q, want %q", versionID, info.UserTags, wantTags)
	}
	if !crypto.SSEC.IsEncrypted(info.UserDefined) {
		t.Errorf("version %s lost its SSE-C seal after retransmit", versionID)
	}
	getReq, err := newTestSignedRequestV4(http.MethodGet, getGetObjectURL("", bucketName, object)+"?versionId="+versionID,
		0, nil, credentials.AccessKey, credentials.SecretKey, sseHeaders)
	if err != nil {
		t.Fatal(err)
	}
	getRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !bytes.Equal(getRec.Body.Bytes(), want) {
		t.Fatalf("version %s does not read back with the customer key after retransmit: %d (%d bytes, want %d)",
			versionID, getRec.Code, getRec.Body.Len(), len(want))
	}
}

// TestPutReplicationOptsRetentionRemoval asserts that a source version whose
// retention was removed (stored as an empty mode and date) still builds
// replication options, carrying the removal's ordering timestamp and no value.
func TestPutReplicationOptsRetentionRemoval(t *testing.T) {
	removedAt := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	oi := ObjectInfo{
		Bucket: "b", Name: "o", VersionID: "v1", ModTime: removedAt.Add(-time.Hour),
		UserDefined: map[string]string{
			strings.ToLower(xhttp.AmzObjectLockMode):                   "",
			strings.ToLower(xhttp.AmzObjectLockRetainUntilDate):        "",
			ReservedMetadataPrefixLower + ObjectLockRetentionTimestamp: removedAt.Format(time.RFC3339Nano),
		},
	}
	opts, _, err := putReplicationOpts(t.Context(), "", oi)
	if err != nil {
		t.Fatalf("putReplicationOpts on a removed retention: %v", err)
	}
	if opts.Mode != "" || !opts.RetainUntilDate.IsZero() {
		t.Errorf("removal sent as a retention: mode %q date %v", opts.Mode, opts.RetainUntilDate)
	}
	if !opts.Internal.RetentionTimestamp.Equal(removedAt) {
		t.Errorf("removal timestamp %v, want %v", opts.Internal.RetentionTimestamp, removedAt)
	}
	if hdr := opts.Header(); hdr.Get(xhttp.AmzObjectLockMode) != "" || hdr.Get(xhttp.AmzObjectLockRetainUntilDate) != "" ||
		hdr.Get(xhttp.MinIOSourceObjectRetentionTimestamp) == "" {
		t.Errorf("removal headers %v: want no lock value and a retention timestamp", hdr)
	}
}

// TestAPISSECReplicaWriteExemptionIsKeyedOnTheIncomingWrite pins the predicate
// of the duplicate version and ETag exemption: it applies to an authenticated
// replica write that carries an SSE-C seal, whatever the destination holds, and
// not to a plaintext replica write that happens to match an SSE-C destination
// version. See pgsty/silo#120.
func TestAPISSECReplicaWriteExemptionIsKeyedOnTheIncomingWrite(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:                 t,
		objAPITest:        testAPISSECReplicaWriteExemptionIsKeyedOnTheIncomingWrite,
		makeBucketOptions: MakeBucketOptions{VersioningEnabled: true},
	})
}

func testAPISSECReplicaWriteExemptionIsKeyedOnTheIncomingWrite(obj ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	previousTLS := globalIsTLS
	globalIsTLS = true
	defer func() { globalIsTLS = previousTLS }()

	key := bytes.Repeat([]byte{0x44}, 32)
	keyMD5 := md5.Sum(key)
	sseHeaders := map[string]string{
		xhttp.AmzServerSideEncryptionCustomerAlgorithm: xhttp.AmzEncryptionAES,
		xhttp.AmzServerSideEncryptionCustomerKey:       base64.StdEncoding.EncodeToString(key),
		xhttp.AmzServerSideEncryptionCustomerKeyMD5:    base64.StdEncoding.EncodeToString(keyMD5[:]),
	}
	replicator := newObjectAttributesAuthzUser(t, instanceType, bucketName,
		`"s3:GetObject","s3:PutObject","s3:ReplicateObject"`)

	rawOf := func(t *testing.T, object string) (ObjectInfo, []byte) {
		t.Helper()
		gr, err := obj.GetObjectNInfo(t.Context(), bucketName, object, nil, http.Header{}, ObjectOptions{
			ReplicationRequest: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer gr.Close()
		raw, err := io.ReadAll(gr)
		if err != nil {
			t.Fatal(err)
		}
		return gr.ObjInfo, raw
	}
	replicaHeaders := func(t *testing.T, oi ObjectInfo) map[string]string {
		t.Helper()
		opts, _, err := putReplicationOpts(t.Context(), "", oi)
		if err != nil {
			t.Fatal(err)
		}
		opts.Internal.SourceMTime = time.Time{}
		out := make(map[string]string)
		for name, values := range opts.Header() {
			if len(values) > 0 {
				out[name] = values[0]
			}
		}
		out[xhttp.MinIOSourceReplicationRequest] = "true"
		out[xhttp.AmzBucketReplicationStatus] = "REPLICA"
		out[xhttp.MinIOSourceETag] = oi.ETag
		return out
	}
	replicaPut := func(t *testing.T, object, versionID string, body []byte, hdrs map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req, err := newTestSignedRequestV4(http.MethodPut,
			getPutObjectURL("", bucketName, object)+"?versionId="+versionID, int64(len(body)),
			bytes.NewReader(body), replicator.AccessKey, replicator.SecretKey, hdrs)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)
		return rec
	}

	// putSSECMultipart stores a one-part SSE-C multipart object: unlike a
	// single-part SSE-C object, whose stored ETag is the sealed one and never
	// matches the sender's, a multipart ETag compares equal, which is what
	// makes the duplicate version and ETag check reachable at all.
	putSSECMultipart := func(t *testing.T, object string, data []byte) {
		t.Helper()
		newReq, err := newTestSignedRequestV4(http.MethodPost, getNewMultipartURL("", bucketName, object),
			0, nil, credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		newRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(newRec, newReq)
		if newRec.Code != http.StatusOK {
			t.Fatalf("%s: NewMultipart %d: %s", instanceType, newRec.Code, newRec.Body.String())
		}
		var init InitiateMultipartUploadResponse
		if err = xmlDecoder(newRec.Body, &init, int64(newRec.Body.Len())); err != nil {
			t.Fatal(err)
		}
		partReq, err := newTestSignedRequestV4(http.MethodPut,
			getPutObjectPartURL("", bucketName, object, init.UploadID, "1"), int64(len(data)),
			bytes.NewReader(data), credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		partRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(partRec, partReq)
		if partRec.Code != http.StatusOK {
			t.Fatalf("%s: PutPart %d: %s", instanceType, partRec.Code, partRec.Body.String())
		}
		body, err := xml.Marshal(CompleteMultipartUpload{Parts: []CompletePart{
			{PartNumber: 1, ETag: canonicalizeETag(partRec.Header()[xhttp.ETag][0])},
		}})
		if err != nil {
			t.Fatal(err)
		}
		completeReq, err := newTestSignedRequestV4(http.MethodPost,
			getCompleteMultipartUploadURL("", bucketName, object, init.UploadID), int64(len(body)),
			bytes.NewReader(body), credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		completeRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(completeRec, completeReq)
		if completeRec.Code != http.StatusOK {
			t.Fatalf("%s: Complete %d: %s", instanceType, completeRec.Code, completeRec.Body.String())
		}
	}

	t.Run("plaintext-replica-over-ssec-version-is-still-rejected", func(t *testing.T) {
		object := "ssec-exemption/ssec-destination"
		putSSECMultipart(t, object, bytes.Repeat([]byte("ssec-destination-"), 4096))
		srcInfo, raw := rawOf(t, object)
		hdrs := replicaHeaders(t, srcInfo)
		// Without the seal the incoming write is a plaintext replica that merely
		// carries the stored version and ETag: the duplicate check still applies.
		for name := range hdrs {
			if strings.HasPrefix(name, "X-Minio-Replication-Server-Side-Encryption-") {
				delete(hdrs, name)
			}
		}
		rec := replicaPut(t, object, srcInfo.VersionID, raw, hdrs)
		if rec.Code != http.StatusPreconditionFailed {
			t.Fatalf("%s: plaintext replica write over an existing SSE-C version answered %d, want 412: %s",
				instanceType, rec.Code, rec.Body.String())
		}
	})

	t.Run("ssec-replica-over-plaintext-version-is-exempted", func(t *testing.T) {
		object := "ssec-exemption/plain-destination"
		// A plaintext version first, then an SSE-C version of the same key so
		// the seal is bound to this object path.
		putCopyChecksumSource(t, apiRouter, credentials, bucketName, object, []byte("plaintext destination version"), nil)
		plainInfo, err := obj.GetObjectInfo(t.Context(), bucketName, object, ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		data := bytes.Repeat([]byte("ssec-source-"), 400)
		putCopyChecksumSource(t, apiRouter, credentials, bucketName, object, data, sseHeaders)
		srcInfo, raw := rawOf(t, object)

		// The incoming write carries the SSE-C seal and addresses the plaintext
		// version with its ETag: the exemption is decided by the incoming
		// write, not by what the destination holds.
		hdrs := replicaHeaders(t, srcInfo)
		hdrs[xhttp.MinIOSourceETag] = plainInfo.ETag
		rec := replicaPut(t, object, plainInfo.VersionID, raw, hdrs)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: SSE-C replica write over a matching plaintext version answered %d, want 200: %s",
				instanceType, rec.Code, rec.Body.String())
		}
		getReq, err := newTestSignedRequestV4(http.MethodGet,
			getGetObjectURL("", bucketName, object)+"?versionId="+plainInfo.VersionID, 0, nil,
			credentials.AccessKey, credentials.SecretKey, sseHeaders)
		if err != nil {
			t.Fatal(err)
		}
		getRec := httptest.NewRecorder()
		apiRouter.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK || !bytes.Equal(getRec.Body.Bytes(), data) {
			t.Fatalf("%s: the retransmitted version does not read back with the customer key: %d (%d bytes)",
				instanceType, getRec.Code, getRec.Body.Len())
		}
	})
}
