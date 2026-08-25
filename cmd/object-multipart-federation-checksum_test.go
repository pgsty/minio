// Copyright (c) 2015-2025 MinIO, Inc.
// Copyright (c) 2025-2026 PGSTY
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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	miniogo "github.com/minio/minio-go/v7"
	miniocredentials "github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/set"
	"github.com/minio/minio/internal/auth"
	"github.com/minio/minio/internal/config/dns"
	"github.com/minio/minio/internal/hash"
	xhttp "github.com/minio/minio/internal/http"
)

const federatedTestUserAgent = "MinIO (linux; amd64) minio-go/v7.0.99 minio-federated/RELEASE.TEST"

func TestAPIFederatedUploadPartChecksumResponse(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testAPIFederatedUploadPartChecksumResponse,
		endpoints:  []string{"PutObjectPart", "NewMultipart"},
	})
}

func testAPIFederatedUploadPartChecksumResponse(_ ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	algorithms := []struct {
		name         string
		typ          hash.ChecksumType
		checksumType string
	}{
		{name: "crc32-full-object", typ: hash.ChecksumCRC32, checksumType: xhttp.AmzChecksumTypeFullObject},
		{name: "sha256-composite", typ: hash.ChecksumSHA256, checksumType: xhttp.AmzChecksumTypeComposite},
	}
	userAgents := []struct {
		name string
		ua   string
		want bool
	}{
		{name: "absent"},
		{name: "ordinary-sdk", ua: "aws-sdk-go/1.55.5"},
		{name: "federation", ua: federatedTestUserAgent, want: true},
		{name: "lookalike-prefix", ua: "evil-minio-federated/RELEASE.TEST"},
		{name: "lookalike-suffix", ua: "minio-federated-extra/RELEASE.TEST"},
		{name: "missing-version", ua: "minio-federated"},
		{name: "empty-version", ua: "minio-federated/"},
	}
	data := []byte("federated upload part checksum response")

	for _, algorithm := range algorithms {
		for _, userAgent := range userAgents {
			t.Run(algorithm.name+"/"+userAgent.name, func(t *testing.T) {
				object := "federation/response/" + algorithm.name + "/" + userAgent.name
				uploadID := newMultipartUploadHTTP(t, apiRouter, credentials, bucketName, object,
					algorithm.typ.String(), algorithm.checksumType)
				headers := map[string]string{}
				if userAgent.ua != "" {
					headers["User-Agent"] = userAgent.ua
				}
				_, rec := uploadPartHTTP(t, apiRouter, credentials,
					bucketName, object, uploadID, 1, data, headers)

				got := rec.Header().Get(algorithm.typ.Key())
				if userAgent.want {
					if want := mustChecksum(t, algorithm.typ, data); got != want {
						t.Fatalf("%s: checksum %q, want %q", instanceType, got, want)
					}
				} else if got != "" {
					t.Fatalf("%s: ordinary UploadPart exposed server checksum %q", instanceType, got)
				}
				if got := rec.Header().Get(xhttp.AmzChecksumType); got != "" {
					t.Fatalf("%s: UploadPart returned checksum type %q", instanceType, got)
				}
			})
		}
	}
}

func TestAPIFederatedUploadPartChecksumMinIOGoWire(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testAPIFederatedUploadPartChecksumMinIOGoWire,
		endpoints:  []string{"PutObjectPart", "NewMultipart"},
	})
}

func testAPIFederatedUploadPartChecksumMinIOGoWire(_ ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	server := httptest.NewServer(apiRouter)
	defer server.Close()

	core, err := miniogo.NewCore(server.Listener.Addr().String(), &miniogo.Options{
		Creds:        miniocredentials.NewStaticV4(credentials.AccessKey, credentials.SecretKey, ""),
		Secure:       false,
		Region:       globalMinioDefaultRegion,
		BucketLookup: miniogo.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("%s: create minio-go Core: %v", instanceType, err)
	}
	core.SetAppInfo("minio-federated", ReleaseTag)

	object := "federation/minio-go-wire"
	uploadID := newMultipartUploadHTTP(t, apiRouter, credentials, bucketName, object,
		hash.ChecksumCRC32.String(), xhttp.AmzChecksumTypeFullObject)
	data := []byte("minio-go must parse the remote computed checksum")
	part, err := core.PutObjectPart(t.Context(), bucketName, object, uploadID, 1,
		bytes.NewReader(data), int64(len(data)), miniogo.PutObjectPartOptions{})
	if err != nil {
		t.Fatalf("%s: minio-go PutObjectPart: %v", instanceType, err)
	}
	if want := mustChecksum(t, hash.ChecksumCRC32, data); part.ChecksumCRC32 != want {
		t.Fatalf("%s: minio-go checksum %q, want %q", instanceType, part.ChecksumCRC32, want)
	}
	if part.ETag == "" {
		t.Fatalf("%s: minio-go returned an empty ETag", instanceType)
	}
}

func TestAPIFederatedUploadPartChecksumConcurrentOverwrite(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testAPIFederatedUploadPartChecksumConcurrentOverwrite,
		endpoints:  []string{"PutObjectPart", "NewMultipart"},
	})
}

func testAPIFederatedUploadPartChecksumConcurrentOverwrite(_ ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	object := "federation/concurrent-overwrite"
	uploadID := newMultipartUploadHTTP(t, apiRouter, credentials, bucketName, object,
		hash.ChecksumSHA256.String(), xhttp.AmzChecksumTypeComposite)
	data := [][]byte{
		bytes.Repeat([]byte("first-writer-"), 4096),
		bytes.Repeat([]byte("second-writer-"), 4096),
	}
	reqs := make([]*http.Request, len(data))
	recorders := make([]*httptest.ResponseRecorder, len(data))
	for i := range data {
		req, err := newTestSignedRequestV4(http.MethodPut,
			getPutObjectPartURL("", bucketName, object, uploadID, "1"),
			int64(len(data[i])), bytes.NewReader(data[i]), credentials.AccessKey, credentials.SecretKey,
			map[string]string{"User-Agent": federatedTestUserAgent})
		if err != nil {
			t.Fatalf("%s: build concurrent request %d: %v", instanceType, i, err)
		}
		reqs[i] = req
		recorders[i] = httptest.NewRecorder()
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			apiRouter.ServeHTTP(recorders[i], reqs[i])
		}()
	}
	close(start)
	wg.Wait()

	for i, rec := range recorders {
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: concurrent request %d failed: %d %s", instanceType, i, rec.Code, rec.Body.String())
		}
		got := rec.Header().Get(hash.ChecksumSHA256.Key())
		if want := mustChecksum(t, hash.ChecksumSHA256, data[i]); got != want {
			t.Fatalf("%s: concurrent request %d checksum %q, want %q", instanceType, i, got, want)
		}
		// The ETag and the checksum must describe the same write, so a losing
		// writer can never publish the winner's checksum next to its own ETag.
		etags := rec.Header()[xhttp.ETag]
		if len(etags) != 1 {
			t.Fatalf("%s: concurrent request %d returned %d ETags", instanceType, i, len(etags))
		}
		md5sum := md5.Sum(data[i])
		if want := hex.EncodeToString(md5sum[:]); canonicalizeETag(etags[0]) != want {
			t.Fatalf("%s: concurrent request %d ETag %q, want %q", instanceType, i, etags[0], want)
		}
	}
}

// federationTestDNS is a minimal dns.Store so a single test process can play
// both federation roles.
type federationTestDNS struct {
	records map[string][]dns.SrvRecord
}

func (f federationTestDNS) Put(string) error { return nil }

func (f federationTestDNS) Get(bucket string) ([]dns.SrvRecord, error) {
	records, ok := f.records[bucket]
	if !ok {
		return nil, dns.ErrNoEntriesFound
	}
	return records, nil
}

func (f federationTestDNS) Delete(string) error                       { return nil }
func (f federationTestDNS) List() (map[string][]dns.SrvRecord, error) { return f.records, nil }
func (f federationTestDNS) DeleteRecord(dns.SrvRecord) error          { return nil }
func (f federationTestDNS) Close() error                              { return nil }
func (f federationTestDNS) String() string                            { return "federation-test-dns" }

// remoteBucketObjectLayer reports one existing bucket as missing so that
// isRemoteCopyRequired takes the legacy federation branch while the same
// process can still serve that bucket as the remote deployment.
type remoteBucketObjectLayer struct {
	ObjectLayer
	remoteBucket string
}

func (l remoteBucketObjectLayer) GetBucketInfo(ctx context.Context, bucket string, opts BucketOptions) (BucketInfo, error) {
	if bucket == l.remoteBucket {
		return BucketInfo{}, toObjectErr(errVolumeNotFound, bucket)
	}
	return l.ObjectLayer.GetBucketInfo(ctx, bucket, opts)
}

// TestAPIFederatedCopyObjectPartChecksum drives the legacy etcd federation
// branch of CopyObjectPartHandler end to end: the proxy forwards the copied
// bytes through the real getRemoteInstanceClient and minio-go, a second HTTP
// endpoint serves the real PutObjectPartHandler, and CopyPartResult must carry
// the checksum computed by that exact remote write.
func TestAPIFederatedCopyObjectPartChecksum(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testAPIFederatedCopyObjectPartChecksum,
		endpoints: []string{
			"CopyObjectPart", "NewMultipart", "PutObjectPart",
			"ListObjectParts", "CompleteMultipart", "PutObject",
		},
	})
}

func testAPIFederatedCopyObjectPartChecksum(objectAPI ObjectLayer, instanceType, bucketName string,
	apiRouter http.Handler, credentials auth.Credentials, t *testing.T,
) {
	algorithms := []struct {
		name         string
		typ          hash.ChecksumType
		checksumType string
	}{
		{name: "crc32-full-object", typ: hash.ChecksumCRC32, checksumType: xhttp.AmzChecksumTypeFullObject},
		{name: "sha256-composite", typ: hash.ChecksumSHA256, checksumType: xhttp.AmzChecksumTypeComposite},
	}

	data := bytes.Repeat([]byte("federated-upload-part-copy-"), 1024)
	srcObject := "federation/copy-source.bin"
	putCopyChecksumSource(t, apiRouter, credentials, bucketName, srcObject, data, nil)

	// The destination bucket really exists so the remote endpoint can serve it;
	// only the proxy's own bucket lookup is told that it lives elsewhere.
	remoteBucket := getRandomBucketName()
	if err := objectAPI.MakeBucket(t.Context(), remoteBucket, MakeBucketOptions{}); err != nil {
		t.Fatalf("%s: unable to create the remote bucket: %v", instanceType, err)
	}

	remote := httptest.NewServer(apiRouter)
	defer remote.Close()
	host, port, _ := strings.Cut(remote.Listener.Addr().String(), ":")

	globalObjLayerMutex.Lock()
	previousLayer := globalObjectAPI
	globalObjectAPI = remoteBucketObjectLayer{ObjectLayer: previousLayer, remoteBucket: remoteBucket}
	globalObjLayerMutex.Unlock()
	previousDNS, previousFederation, previousIPs := globalDNSConfig, globalBucketFederation, globalDomainIPs
	globalDNSConfig = federationTestDNS{records: map[string][]dns.SrvRecord{
		bucketName:   {{Host: host, Port: json.Number(port)}},
		remoteBucket: {{Host: host, Port: json.Number(port)}},
	}}
	// Every DNS record resolves to this process, so the bucket forwarding
	// middleware always serves locally and only the handler proxies.
	globalDomainIPs = set.CreateStringSet(remote.Listener.Addr().String())
	globalBucketFederation = true
	defer func() {
		globalObjLayerMutex.Lock()
		globalObjectAPI = previousLayer
		globalObjLayerMutex.Unlock()
		globalDNSConfig, globalBucketFederation, globalDomainIPs = previousDNS, previousFederation, previousIPs
	}()

	for _, algorithm := range algorithms {
		t.Run(algorithm.name, func(t *testing.T) {
			object := "federation/copy-destination-" + algorithm.name + ".bin"
			uploadID := newMultipartUploadHTTP(t, apiRouter, credentials, remoteBucket, object,
				algorithm.typ.String(), algorithm.checksumType)

			req, err := newTestSignedRequestV4(http.MethodPut,
				getCopyObjectPartURL("", remoteBucket, object, uploadID, "1"),
				0, nil, credentials.AccessKey, credentials.SecretKey,
				map[string]string{xhttp.AmzCopySource: SlashSeparator + pathJoin(bucketName, srcObject)})
			if err != nil {
				t.Fatalf("%s: unable to build UploadPartCopy request: %v", instanceType, err)
			}
			rec := httptest.NewRecorder()
			apiRouter.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: federated UploadPartCopy failed: %d %s", instanceType, rec.Code, rec.Body.String())
			}

			var response CopyObjectPartResponse
			if err := xml.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("%s: unable to decode CopyPartResult: %v", instanceType, err)
			}
			want := mustChecksum(t, algorithm.typ, data)
			if got := copyPartChecksum(algorithm.typ, response); got != want {
				t.Fatalf("%s: CopyPartResult %s is %q, want %q: %s",
					instanceType, algorithm.typ.String(), got, want, rec.Body.String())
			}

			// The persisted part must carry the same value, and the client must be
			// able to complete the upload with what CopyPartResult returned.
			parts := listPartsHTTP(t, apiRouter, credentials, remoteBucket, object, uploadID, nil)
			if len(parts.Parts) != 1 {
				t.Fatalf("%s: ListParts returned %d parts, want 1", instanceType, len(parts.Parts))
			}
			if got := partChecksum(algorithm.typ, parts.Parts[0]); got != want {
				t.Fatalf("%s: persisted part %s is %q, want %q", instanceType, algorithm.typ.String(), got, want)
			}
			etag := canonicalizeETag(response.ETag)
			completed := completePartsHTTP(t, apiRouter, credentials, remoteBucket, object, uploadID,
				[]CompletePart{completePartWithChecksum(algorithm.typ, 1, etag, want)}, nil)
			if completed.Code != http.StatusOK {
				t.Fatalf("%s: CompleteMultipartUpload rejected the federated part: %d %s",
					instanceType, completed.Code, completed.Body.String())
			}
		})
	}
}
