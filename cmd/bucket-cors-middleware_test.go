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
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/minio/internal/auth"
	"github.com/minio/minio/internal/bucket/cors"
)

type corsLookupCountingObjectLayer struct {
	ObjectLayer
	getObjectNInfoCalls atomic.Int64
}

func (o *corsLookupCountingObjectLayer) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, opts ObjectOptions) (*GetObjectReader, error) {
	o.getObjectNInfoCalls.Add(1)
	return o.ObjectLayer.GetObjectNInfo(ctx, bucket, object, rs, h, opts)
}

func TestPerBucketCorsPreflight(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"http://example.com"},
		AllowedMethods: []string{"GET", "PUT"},
		AllowedHeaders: []string{"*"},
		ExposeHeaders:  []string{"ETag"},
		MaxAgeSeconds:  3000,
	}}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mybucket/obj", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "X-Amz-Date")

	handled := applyBucketCors(rec, req, cfg)
	if !handled {
		t.Fatal("expected preflight to be handled")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, PUT" {
		t.Fatalf("allow-methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "X-Amz-Date" {
		t.Fatalf("allow-headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "ETag" {
		t.Fatalf("expose-headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "3000" {
		t.Fatalf("max-age = %q", got)
	}
	requireCorsVary(t, rec.Header())
	if rec.Code != http.StatusOK {
		t.Fatalf("preflight status = %d", rec.Code)
	}
	requireCorsOriginVary(t, rec.Header())
}

func TestPerBucketCorsActualRequestNoMatchVariesByOrigin(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"https://allowed.example.com"},
		AllowedMethods: []string{"GET"},
	}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)
	req.Header.Set("Origin", "https://denied.example.com")

	if handled := applyBucketCors(rec, req, cfg); handled {
		t.Fatal("actual request must continue when CORS does not match")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q", got)
	}
	requireCorsOriginVary(t, rec.Header())
}

func TestPerBucketCorsPreflightNoMatch(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"http://example.com"},
		AllowedMethods: []string{"GET"},
	}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mybucket/obj", nil)
	req.Header.Set("Origin", "http://evil.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	handled := applyBucketCors(rec, req, cfg)
	if !handled {
		t.Fatal("expected preflight to be handled (rejected)")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("rejected preflight returned allow-origin %q", got)
	}
	requireCorsVary(t, rec.Header())
}

func TestPerBucketCorsPreflightWildcardOriginAndZeroMaxAge(t *testing.T) {
	doc := `<CORSConfiguration><CORSRule><AllowedOrigin>*</AllowedOrigin><AllowedMethod>GET</AllowedMethod><AllowedMethod>HEAD</AllowedMethod><AllowedHeader>*</AllowedHeader><ExposeHeader>ETag</ExposeHeader><MaxAgeSeconds>0</MaxAgeSeconds></CORSRule></CORSConfiguration>`
	cfg, err := cors.ParseBucketCorsConfig(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mybucket/obj", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "RANGE")

	if handled := applyBucketCors(rec, req, cfg); !handled {
		t.Fatal("expected preflight to be handled")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow-credentials = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD" {
		t.Fatalf("allow-methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "RANGE" {
		t.Fatalf("allow-headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "ETag" {
		t.Fatalf("expose-headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "0" {
		t.Fatalf("max-age = %q", got)
	}
	requireCorsVary(t, rec.Header())
}

func TestPerBucketCorsPreflightUsesFirstFullyMatchingRule(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{
		{
			AllowedOrigins: []string{"https://app.example.com"},
			AllowedMethods: []string{"GET"},
			AllowedHeaders: []string{"x-a"},
			ExposeHeaders:  []string{"x-rule-a"},
			MaxAgeSeconds:  1,
		},
		{
			AllowedOrigins: []string{"https://app.example.com"},
			AllowedMethods: []string{"GET", "HEAD"},
			AllowedHeaders: []string{"*"},
			ExposeHeaders:  []string{"x-rule-b"},
			MaxAgeSeconds:  2,
		},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mybucket/obj", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "X-B")

	if handled := applyBucketCors(rec, req, cfg); !handled {
		t.Fatal("expected preflight to be handled")
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "x-rule-b" {
		t.Fatalf("selected rule expose-headers = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD" {
		t.Fatalf("selected rule allow-methods = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "2" {
		t.Fatalf("selected rule max-age = %q", got)
	}
}

func TestPerBucketCorsActualRequest(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		ExposeHeaders:  []string{"ETag"},
	}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)
	req.Header.Set("Origin", "http://any.com")

	handled := applyBucketCors(rec, req, cfg)
	if handled {
		t.Fatal("actual (non-preflight) request must not be terminated by CORS")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("allow-credentials = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "ETag" {
		t.Fatalf("expose-headers = %q", got)
	}
}

func TestPerBucketCorsOriginPatternResponse(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"https://app.example.com", "https://*", "*"},
		AllowedMethods: []string{"GET"},
	}}}

	tests := []struct {
		origin          string
		wantOrigin      string
		wantCredentials string
	}{
		{"https://app.example.com", "https://app.example.com", "true"},
		{"https://other.example.com", "https://other.example.com", "true"},
		{"http://other.example.com", "*", ""},
	}

	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)
		req.Header.Set("Origin", tt.origin)
		if handled := applyBucketCors(rec, req, cfg); handled {
			t.Fatal("actual request must not be terminated by CORS")
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
			t.Fatalf("origin %q: allow-origin = %q, want %q", tt.origin, got, tt.wantOrigin)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != tt.wantCredentials {
			t.Fatalf("origin %q: allow-credentials = %q, want %q", tt.origin, got, tt.wantCredentials)
		}
	}
}

func TestBucketCorsMetadataErrorFailsClosed(t *testing.T) {
	oldObjectAPI := newObjectLayerFn()
	oldMetadataSys := globalBucketMetadataSys
	setObjectLayer(nil)
	globalBucketMetadataSys = NewBucketMetadataSys()
	// A resident bucket whose stored CORS document does not parse must not be
	// answered with the global policy: it has a configuration we cannot honor.
	meta := newBucketMetadata("cors-metadata-error")
	meta.corsConfigErr = fmt.Errorf("invalid bucket CORS configuration")
	globalBucketMetadataSys.Set("cors-metadata-error", meta)
	defer func() {
		setObjectLayer(oldObjectAPI)
		globalBucketMetadataSys = oldMetadataSys
	}()

	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, getGetObjectURL("", "cors-metadata-error", "object"), nil)
		req.Header.Set("Origin", "https://app.example.com")
		if method == http.MethodOptions {
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		}
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d, want %d", method, rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s metadata error fell back to global allow-origin %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("%s metadata error fell back to global credentials %q", method, got)
		}
	}
}

func TestBucketCorsSkipsMetadataLookupWithoutOrigin(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsSkipsMetadataLookupWithoutOrigin,
		endpoints:  []string{"GetObject"},
	})
}

func testBucketCorsSkipsMetadataLookupWithoutOrigin(obj ObjectLayer, _ string, _ string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	oldObjectAPI := newObjectLayerFn()
	counting := &corsLookupCountingObjectLayer{ObjectLayer: obj}
	setObjectLayer(counting)
	defer setObjectLayer(oldObjectAPI)

	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, getGetObjectURL("", "api", "v1/login"), nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	requireCorsOriginVary(t, rec.Header())
	if got := counting.getObjectNInfoCalls.Load(); got != 0 {
		t.Fatalf("request without Origin performed %d bucket metadata reads", got)
	}
}

func TestBucketCorsOriginlessPreflightShapeUsesGlobalHandler(t *testing.T) {
	nextCalled := false
	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, getGetObjectURL("", "api", "v1/login"), nil)
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if nextCalled {
		t.Fatal("originless preflight-shaped OPTIONS reached the application handler")
	}
	requireCorsOriginVary(t, rec.Header())
}

func TestBucketCorsNoConfigUsesGlobalFallback(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsNoConfigUsesGlobalFallback,
		endpoints:  []string{"GetBucketCors"},
	})
}

func TestBucketCorsMissingBucketUsesGlobalFallback(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsMissingBucketUsesGlobalFallback,
		endpoints:  []string{"GetBucketCors"},
	})
}

func testBucketCorsMissingBucketUsesGlobalFallback(obj ObjectLayer, _ string, bucket string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	// Model a fully started server: bucket metadata loading has completed, so
	// a name that is not resident is genuinely not a CORS-bearing bucket.
	restore := markBucketMetadataInitialized(t)
	defer restore()

	// A non-resident bucket name must not cause any bucket-metadata disk read.
	oldObjectAPI := newObjectLayerFn()
	counting := &corsLookupCountingObjectLayer{ObjectLayer: obj}
	setObjectLayer(counting)
	defer setObjectLayer(oldObjectAPI)

	before := bucketMetadataMapLen()

	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, getGetObjectURL("", bucket+"-missing", "object"), nil)
	req.Header.Set("Origin", "https://app.example.com")
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
	// Regression guard: the pre-auth CORS lookup for a non-existent bucket must
	// neither read bucket metadata from disk nor cache a synthetic entry.
	if got := counting.getObjectNInfoCalls.Load(); got != 0 {
		t.Fatalf("missing-bucket CORS lookup performed %d bucket metadata reads", got)
	}
	if after := bucketMetadataMapLen(); after != before {
		t.Fatalf("missing-bucket CORS lookup grew metadataMap from %d to %d", before, after)
	}
}

func testBucketCorsNoConfigUsesGlobalFallback(_ ObjectLayer, _ string, bucket string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, getGetObjectURL("", bucket, "object"), nil)
	req.Header.Set("Origin", "https://app.example.com")
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
}

func TestPerBucketCorsActualPatternOriginSupportsCredentials(t *testing.T) {
	cfg := &cors.Config{CORSRules: []cors.Rule{{
		AllowedOrigins: []string{"https://*.example.com"},
		AllowedMethods: []string{"GET"},
	}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)
	req.Header.Set("Origin", "https://app.example.com")

	if handled := applyBucketCors(rec, req, cfg); handled {
		t.Fatal("actual request must continue")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow-credentials = %q", got)
	}
}

func TestPerBucketCorsActualNullOriginSurvivesForwardingMiddleware(t *testing.T) {
	next := setBucketForwardingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("per-bucket null origin", func(t *testing.T) {
		cfg := &cors.Config{CORSRules: []cors.Rule{{
			AllowedOrigins: []string{"null"},
			AllowedMethods: []string{"GET"},
		}}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)
		req.Header.Set("Origin", "null")

		if handled := applyBucketCors(rec, req, cfg); handled {
			t.Fatal("actual request must continue")
		}
		next.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "null" {
			t.Fatalf("allow-origin = %q", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("allow-credentials = %q", got)
		}
	})

	t.Run("legacy unmarked null origin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Access-Control-Allow-Origin", "null")
		req := httptest.NewRequest(http.MethodGet, "/mybucket/obj", nil)

		next.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("allow-origin = %q", got)
		}
	})
}

func requireCorsVary(t *testing.T, header http.Header) {
	t.Helper()
	values := strings.Join(header.Values("Vary"), ",")
	for _, want := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !strings.Contains(values, want) {
			t.Fatalf("Vary = %q, missing %q", values, want)
		}
	}
}

func requireCorsOriginVary(t *testing.T, header http.Header) {
	t.Helper()
	if values := strings.Join(header.Values("Vary"), ","); !strings.Contains(values, "Origin") {
		t.Fatalf("Vary = %q, missing Origin", values)
	}
}

// markBucketMetadataInitialized marks the global bucket-metadata subsystem as
// fully loaded, modeling a running server (the API test harness sets up the
// subsystem but does not run Init). It returns a function that restores the
// previous state.
func markBucketMetadataInitialized(t *testing.T) func() {
	t.Helper()
	sys := globalBucketMetadataSys
	if sys == nil {
		t.Fatal("globalBucketMetadataSys is nil")
	}
	sys.Lock()
	prev := sys.initialized
	sys.initialized = true
	sys.Unlock()
	return func() {
		sys.Lock()
		sys.initialized = prev
		sys.Unlock()
	}
}

// bucketMetadataMapLen returns the number of resident bucket-metadata entries.
func bucketMetadataMapLen() int {
	sys := globalBucketMetadataSys
	if sys == nil {
		return 0
	}
	sys.RLock()
	defer sys.RUnlock()
	return len(sys.metadataMap)
}

// TestBucketCorsUnknownBucketDoesNotGrowMetadata is the regression guard for
// the pre-auth resource-exhaustion path: an unauthenticated, Origin-bearing
// request whose first path segment is not a real bucket must fall back to the
// global CORS policy without loading bucket metadata from disk and without
// caching a synthetic entry. Before the resident-only lookup, each distinct
// name grew metadataMap by one and issued an erasure metadata probe.
func TestBucketCorsUnknownBucketDoesNotGrowMetadata(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsUnknownBucketDoesNotGrowMetadata,
		endpoints:  []string{"GetBucketCors"},
	})
}

func testBucketCorsUnknownBucketDoesNotGrowMetadata(obj ObjectLayer, _ string, _ string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	restore := markBucketMetadataInitialized(t)
	defer restore()

	oldObjectAPI := newObjectLayerFn()
	counting := &corsLookupCountingObjectLayer{ObjectLayer: obj}
	setObjectLayer(counting)
	defer setObjectLayer(oldObjectAPI)

	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	before := bucketMetadataMapLen()
	// Console/admin routes plus enough distinct valid names to make accidental
	// cache growth or one metadata probe per name unambiguous.
	names := []string{"minio", "api"}
	for i := 0; i < 500; i++ {
		names = append(names, fmt.Sprintf("cors-missing-%03d", i))
	}
	for _, name := range names {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, getGetObjectURL("", name, "obj"), nil)
			req.Header.Set("Origin", "https://app.example.com")
			if method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}
			wrapped.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
				t.Fatalf("%s/%s: allow-origin = %q, want global fallback", name, method, got)
			}
		}
	}
	for _, path := range []string{"/../obj", "/A/obj", "/x/obj", "/minio/admin/v3/info", "/api/v1/login"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "https://app.example.com")
		wrapped.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Fatalf("%s: allow-origin = %q, want global fallback", path, got)
		}
	}

	if got := counting.getObjectNInfoCalls.Load(); got != 0 {
		t.Fatalf("unknown-bucket CORS lookups performed %d bucket metadata reads", got)
	}
	if after := bucketMetadataMapLen(); after != before {
		t.Fatalf("unknown-bucket CORS lookups grew metadataMap from %d to %d", before, after)
	}
}

func TestBucketCorsStartupMissFailsClosedWithoutIO(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsStartupMissFailsClosedWithoutIO,
		endpoints:  []string{"GetBucketCors"},
	})
}

func testBucketCorsStartupMissFailsClosedWithoutIO(obj ObjectLayer, _ string, _ string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	oldObjectAPI := newObjectLayerFn()
	oldMetadataSys := globalBucketMetadataSys
	counting := &corsLookupCountingObjectLayer{ObjectLayer: obj}
	setObjectLayer(counting)
	globalBucketMetadataSys = NewBucketMetadataSys()
	defer func() {
		setObjectLayer(oldObjectAPI)
		globalBucketMetadataSys = oldMetadataSys
	}()

	innerCalled := false
	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/startup-missing/object", nil)
	req.Header.Set("Origin", "https://app.example.com")
	wrapped.ServeHTTP(rec, req)

	if !innerCalled || rec.Code != http.StatusNoContent {
		t.Fatalf("startup miss did not reach inner handler: called=%v status=%d", innerCalled, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("startup miss used permissive global CORS: %q", got)
	}
	if got := counting.getObjectNInfoCalls.Load(); got != 0 {
		t.Fatalf("startup miss performed %d metadata reads", got)
	}
	if got := globalBucketMetadataSys.Count(); got != 0 {
		t.Fatalf("startup miss grew metadataMap to %d", got)
	}
}

// markBucketMetadataLoadFailed records a bucket as one whose metadata failed to
// load at startup while the subsystem is Initialized, modeling the degraded
// state where a real bucket is not resident. Returns a restore function.
func markBucketMetadataLoadFailed(t *testing.T, bucket string) func() {
	t.Helper()
	sys := globalBucketMetadataSys
	if sys == nil {
		t.Fatal("globalBucketMetadataSys is nil")
	}
	sys.Lock()
	_, had := sys.loadFailed[bucket]
	sys.loadFailed[bucket] = struct{}{}
	sys.Unlock()
	return func() {
		sys.Lock()
		if !had {
			delete(sys.loadFailed, bucket)
		}
		sys.Unlock()
	}
}

// TestBucketCorsLoadFailedBucketFailsClosed guards P1: a real bucket whose
// metadata could not be loaded at startup (present in loadFailed, subsystem
// Initialized) must NOT be answered with the permissive global CORS policy. We
// cannot rule out a restrictive per-bucket config for it, so it must fail
// closed — without a synchronous disk read.
func TestBucketCorsLoadFailedBucketFailsClosed(t *testing.T) {
	ExecObjectLayerAPITest(ExecObjectLayerAPITestArgs{
		t:          t,
		objAPITest: testBucketCorsLoadFailedBucketFailsClosed,
		endpoints:  []string{"GetBucketCors"},
	})
}

func testBucketCorsLoadFailedBucketFailsClosed(obj ObjectLayer, _ string, _ string, _ http.Handler, _ auth.Credentials, t *testing.T) {
	restoreInit := markBucketMetadataInitialized(t)
	defer restoreInit()
	restoreFail := markBucketMetadataLoadFailed(t, "strict-cors-bucket")
	defer restoreFail()

	oldObjectAPI := newObjectLayerFn()
	counting := &corsLookupCountingObjectLayer{ObjectLayer: obj}
	setObjectLayer(counting)
	defer setObjectLayer(oldObjectAPI)

	wrapped := corsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, getGetObjectURL("", "strict-cors-bucket", "object"), nil)
		req.Header.Set("Origin", "https://app.example.com")
		if method == http.MethodOptions {
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		}
		wrapped.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s: load-failed bucket fell back to global allow-origin %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("%s: load-failed bucket fell back to global credentials %q", method, got)
		}
	}
	if got := counting.getObjectNInfoCalls.Load(); got != 0 {
		t.Fatalf("load-failed CORS lookup performed %d synchronous bucket metadata reads", got)
	}
}
