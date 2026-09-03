// Copyright (c) 2015-2026 MinIO, Inc.
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
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/minio/minio/internal/auth"
	"github.com/minio/minio/internal/handlers"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/pgsty/silo-pkg/v3/policy"
	"github.com/pgsty/silo-pkg/v3/policy/condition"
)

const (
	testCondSourceIP  = "203.0.113.5"
	testCondRemoteILP = testCondSourceIP + ":12345"
)

func condValuesForRequest(t *testing.T, rawURL string, header map[string]string) map[string][]string {
	return condValuesForRequestWithTags(t, rawURL, header, "", nil)
}

func condValuesForRequestWithExistingTags(t *testing.T, rawURL string, header map[string]string, existingTags string) map[string][]string {
	return condValuesForRequestWithTags(t, rawURL, header, existingTags, nil)
}

func condValuesForRequestWithTags(t *testing.T, rawURL string, header map[string]string, existingTags string, requestTags *string) map[string][]string {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = testCondRemoteILP
	for k, v := range header {
		r.Header.Set(k, v)
	}
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	return getConditionValuesWithTags(r, "us-east-1", auth.Credentials{AccessKey: "lowpriv"}, existingTags, requestTags)
}

func resolvedConditionValues(values map[string][]string, name string) []string {
	if v := values[name]; len(v) > 0 {
		return v
	}
	return values[http.CanonicalHeaderKey(name)]
}

// A client must not be able to reach a condition key that the server computes
// for itself. Both routes are covered: a header whose canonical spelling
// collides with the key name, and a query parameter that collides with it
// exactly. The query route is the sharper one, because the merge appended to
// the server's value rather than replacing it and a condition function matches
// when any single value matches.
func TestGetConditionValuesRejectsClientSuppliedServerKeys(t *testing.T) {
	honest := condValuesForRequest(t, "http://minio.local/bkt/obj", nil)

	for _, kn := range condition.AllSupportedKeys {
		name := kn.ToKey().Name()
		if _, clientSupplied := clientSuppliedConditionKeys[name]; clientSupplied {
			continue // the request is where this one is supposed to come from
		}
		// Deliberately not skipped when the server left the key empty. An empty
		// name is exactly as forgeable as a populated one, and the keys the
		// server has no value for - most of jwt: and ldap: - are the ones a
		// resource variable expands.
		want := honest[name]
		canonical := http.CanonicalHeaderKey(name)

		t.Run("query/"+name, func(t *testing.T) {
			got := condValuesForRequest(t,
				"http://minio.local/bkt/obj?"+url.Values{name: {"ATTACKER"}}.Encode(), nil)
			if slices.Contains(got[name], "ATTACKER") {
				t.Errorf("?%s= reached %v, server computed %v", name, got[name], want)
			}
			if !slices.Equal(got[name], want) {
				t.Errorf("%v changed to %v", want, got[name])
			}
		})

		// aws:Referer is read out of the Referer header, so the header is its
		// source of truth rather than a way to forge it. aws:UserAgent is not
		// in the same position: it comes from User-Agent, which does not
		// canonicalise to "Useragent".
		if kn == condition.AWSReferer {
			continue
		}

		t.Run("header/"+canonical, func(t *testing.T) {
			got := condValuesForRequest(t, "http://minio.local/bkt/obj",
				map[string]string{canonical: "ATTACKER"})
			// The lookup the policy engine itself performs, exact name first
			// with the canonical form as fallback.
			seen := got[name]
			if len(seen) == 0 {
				seen = got[canonical]
			}
			if slices.Contains(seen, "ATTACKER") {
				t.Errorf("%s: header reached the lookup as %v, server computed %v",
					canonical, seen, want)
			}
		})
	}
}

func TestGetConditionValuesUsesActualRequestSource(t *testing.T) {
	for name, source := range clientSuppliedConditionKeys {
		t.Run(name, func(t *testing.T) {
			fromHeader := condValuesForRequest(t, "http://minio.local/bkt/obj",
				map[string]string{name: "HEADER"})
			fromQuery := condValuesForRequest(t,
				"http://minio.local/bkt/obj?"+url.Values{name: {"QUERY"}}.Encode(), nil)
			fromCanonicalQuery := condValuesForRequest(t,
				"http://minio.local/bkt/obj?"+url.Values{http.CanonicalHeaderKey(name): {"QUERY"}}.Encode(), nil)
			if got, want := slices.Contains(resolvedConditionValues(fromHeader, name), "HEADER"), source&conditionValueFromHeader != 0; got != want {
				t.Errorf("header accepted=%v, want %v: %v", got, want, fromHeader)
			}
			if got, want := slices.Contains(resolvedConditionValues(fromQuery, name), "QUERY"), source&conditionValueFromQuery != 0; got != want {
				t.Errorf("query accepted=%v, want %v: %v", got, want, fromQuery)
			}
			canonicalQueryAllowed := name == strings.ToLower(xhttp.AmzStorageClass)
			if got := slices.Contains(resolvedConditionValues(fromCanonicalQuery, name), "QUERY"); got != canonicalQueryAllowed {
				t.Errorf("case-variant query accepted=%v, want %v: %v", got, canonicalQueryAllowed, fromCanonicalQuery)
			}
		})
	}

	storageURL := "http://minio.local/bkt/obj?" + url.Values{
		strings.ToLower(xhttp.AmzStorageClass): {"QUERY"},
	}.Encode()
	storageValues := condValuesForRequest(t, storageURL, map[string]string{xhttp.AmzStorageClass: "HEADER"})
	if got := resolvedConditionValues(storageValues, strings.ToLower(xhttp.AmzStorageClass)); !slices.Equal(got, []string{"HEADER"}) {
		t.Errorf("storage class did not use header precedence: %v", got)
	}

	fromQuery := condValuesForRequest(t,
		"http://minio.local/bkt/obj?"+url.Values{xhttp.AmzObjectLockMode: {"COMPLIANCE"}}.Encode(), nil)
	if got := resolvedConditionValues(fromQuery, "object-lock-mode"); len(got) != 0 {
		t.Errorf("object-lock query value reached header condition as %v", got)
	}
}

func TestGetConditionValuesVersionIDPresence(t *testing.T) {
	nullVersionID, err := condition.NewNullFunc(condition.S3VersionID.ToKey(), true)
	if err != nil {
		t.Fatal(err)
	}

	withoutVersionID := condValuesForRequest(t, "http://minio.local/bkt/obj", nil)
	if _, ok := withoutVersionID["versionid"]; ok {
		t.Fatalf("an absent versionId was exposed to policy evaluation as %v", withoutVersionID["versionid"])
	}
	if !condition.NewFunctions(nullVersionID).Evaluate(withoutVersionID) {
		t.Fatal("Null s3:versionid=true did not match a request without versionId")
	}

	const versionID = "7f4b6b5f-bf25-4e98-95df-90cba8070dd8"
	withVersionID := condValuesForRequest(t,
		"http://minio.local/bkt/obj?"+url.Values{xhttp.VersionID: {versionID}}.Encode(), nil)
	if got := withVersionID["versionid"]; !slices.Equal(got, []string{versionID}) {
		t.Fatalf("expected versionId %q, got %v", versionID, got)
	}
	if condition.NewFunctions(nullVersionID).Evaluate(withVersionID) {
		t.Fatal("Null s3:versionid=true matched a request with versionId")
	}

	copySourceVersion := condValuesForRequest(t, "http://minio.local/bkt/copied", map[string]string{
		xhttp.AmzCopySource: "/source-bucket/source-object?" + url.Values{xhttp.VersionID: {versionID}}.Encode(),
	})
	if got := copySourceVersion["versionid"]; !slices.Equal(got, []string{versionID}) {
		t.Fatalf("copy source versionId was lost: got %v", got)
	}

	// The object layer trims the version before acting on it; the condition value
	// must be the same effective string, or a padded ?versionId=V%20 would let a
	// StringEquals/Deny on s3:versionid see a different value than the one deleted.
	paddedVersion := condValuesForRequest(t,
		"http://minio.local/bkt/obj?"+url.Values{xhttp.VersionID: {versionID + " "}}.Encode(), nil)
	if got := paddedVersion["versionid"]; !slices.Equal(got, []string{versionID}) {
		t.Fatalf("a padded versionId was not trimmed to the effective value: got %v", got)
	}

	paddedCopySource := condValuesForRequest(t, "http://minio.local/bkt/copied", map[string]string{
		xhttp.AmzCopySource: "/source-bucket/source-object?" + url.Values{xhttp.VersionID: {versionID + " "}}.Encode(),
	})
	if got := paddedCopySource["versionid"]; !slices.Equal(got, []string{versionID}) {
		t.Fatalf("a padded copy source versionId was not trimmed: got %v", got)
	}

	// A whitespace-only versionId names no version once trimmed, exactly as the
	// object layer treats it, so the key must be absent and Null:true must match.
	blankVersion := condValuesForRequest(t,
		"http://minio.local/bkt/obj?"+url.Values{xhttp.VersionID: {"   "}}.Encode(), nil)
	if _, ok := blankVersion["versionid"]; ok {
		t.Fatalf("a whitespace-only versionId was exposed to policy evaluation as %v", blankVersion["versionid"])
	}
	if !condition.NewFunctions(nullVersionID).Evaluate(blankVersion) {
		t.Fatal("Null s3:versionid=true did not match a request whose versionId was only whitespace")
	}
}

func TestGetConditionValuesUsesEffectiveRequestTags(t *testing.T) {
	rawURL := "http://minio.local/bkt/obj?" + url.Values{
		strings.ToLower(xhttp.AmzObjectTagging): {"security=public&virus=true"},
	}.Encode()

	// Generic operations such as CopyObject must not gain RequestObjectTag
	// values from a query parameter they do not consume.
	withoutEffectiveTags := condValuesForRequest(t, rawURL, nil)
	if len(withoutEffectiveTags["RequestObjectTag/security"]) != 0 || len(withoutEffectiveTags["RequestObjectTagKeys"]) != 0 {
		t.Fatalf("query tags leaked into a generic operation: %v", withoutEffectiveTags)
	}

	effectiveTags := "security=public&virus=true"
	withEffectiveTags := condValuesForRequestWithTags(t, rawURL, nil, "", &effectiveTags)
	if !slices.Equal(withEffectiveTags["RequestObjectTag/security"], []string{"public"}) {
		t.Fatalf("effective request tag missing: %v", withEffectiveTags)
	}
	if !slices.Contains(withEffectiveTags["RequestObjectTagKeys"], "security") ||
		!slices.Contains(withEffectiveTags["RequestObjectTagKeys"], "virus") {
		t.Fatalf("effective request tag keys missing: %v", withEffectiveTags["RequestObjectTagKeys"])
	}

	security, err := condition.NewStringEqualsFunc("", condition.NewKey(condition.RequestObjectTag, "security"), "public")
	if err != nil {
		t.Fatal(err)
	}
	allowedKeys, err := condition.NewStringLikeFunc("ForAllValues", condition.RequestObjectTagKeys.ToKey(), "security", "virus")
	if err != nil {
		t.Fatal(err)
	}
	conditions := condition.NewFunctions(security, allowedKeys)
	if conditions.Evaluate(withoutEffectiveTags) {
		t.Fatal("query upload satisfied request-tag policy without effective tags")
	}
	if !conditions.Evaluate(withEffectiveTags) {
		t.Fatal("effective query tags did not satisfy request-tag policy")
	}
}

func TestBucketPolicySSEConditionUsesHeader(t *testing.T) {
	fn, err := condition.NewStringEqualsFunc("", condition.S3XAmzServerSideEncryption.ToKey(), "aws:kms")
	if err != nil {
		t.Fatal(err)
	}
	conditions := condition.NewFunctions(fn)
	if conditions.Evaluate(condValuesForRequest(t,
		"http://minio.local/bkt/obj?x-amz-server-side-encryption=aws%3Akms", nil)) {
		t.Error("query parameter satisfied a condition on the SSE request header")
	}
	if !conditions.Evaluate(condValuesForRequest(t, "http://minio.local/bkt/obj",
		map[string]string{xhttp.AmzServerSideEncryption: "aws:kms"})) {
		t.Error("SSE request header did not satisfy its condition")
	}
}

// The end to end shape of the bypass: an IpAddress condition restricting a
// bucket to an internal range, against a request from outside it.
func TestBucketPolicySourceIPCannotBeForged(t *testing.T) {
	_, cidr, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	fn, err := condition.NewIPAddressFunc(condition.AWSSourceIP.ToKey(), cidr)
	if err != nil {
		t.Fatal(err)
	}
	bp := policy.BucketPolicy{
		Version: policy.DefaultVersion,
		Statements: []policy.BPStatement{{
			Effect:     policy.Allow,
			Principal:  policy.NewPrincipal("*"),
			Actions:    policy.NewActionSet(policy.GetObjectAction),
			Resources:  policy.NewResourceSet(policy.NewResource("bkt/*")),
			Conditions: condition.NewFunctions(fn),
		}},
	}
	allowed := func(rawURL string, header map[string]string) bool {
		return bp.IsAllowed(policy.BucketPolicyArgs{
			Action:          policy.GetObjectAction,
			BucketName:      "bkt",
			ObjectName:      "obj",
			ConditionValues: condValuesForRequest(t, rawURL, header),
		})
	}

	if allowed("http://minio.local/bkt/obj", nil) {
		t.Fatal("baseline: an address outside 10.0.0.0/8 must not satisfy the condition")
	}
	if allowed("http://minio.local/bkt/obj?SourceIp=10.1.2.3", nil) {
		t.Error("a query parameter forged aws:SourceIp")
	}
	if allowed("http://minio.local/bkt/obj", map[string]string{"Sourceip": "10.1.2.3"}) {
		t.Error("a header forged aws:SourceIp")
	}
}

// The trust policy has to reach the decision, not merely the resolver. This
// drives a forged X-Forwarded-For all the way through getConditionValues into a
// real IpAddress evaluation under each mode. Everything else about the trust
// modes is tested where the logic lives; this is the only test that would notice
// if the resolver were correct but the policy engine were reading something else.
func TestBucketPolicySourceIPForgeryAcrossTrustModes(t *testing.T) {
	_, cidr, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	fn, err := condition.NewIPAddressFunc(condition.AWSSourceIP.ToKey(), cidr)
	if err != nil {
		t.Fatal(err)
	}
	bp := policy.BucketPolicy{
		Version: policy.DefaultVersion,
		Statements: []policy.BPStatement{{
			Effect:     policy.Allow,
			Principal:  policy.NewPrincipal("*"),
			Actions:    policy.NewActionSet(policy.GetObjectAction),
			Resources:  policy.NewResourceSet(policy.NewResource("bkt/*")),
			Conditions: condition.NewFunctions(fn),
		}},
	}

	// An address inside the permitted range, asserted by a client that is not.
	const forgedClaim = "10.1.2.3"
	const outsider = "203.0.113.5:12345"
	const proxy = "192.0.2.7:9000"

	tests := []struct {
		name    string
		proxies string
		peer    string
		allowed bool
	}{{
		// Documented, and the reason the allow-list exists.
		name:    "default mode believes the claim",
		peer:    outsider,
		allowed: true,
	}, {
		name:    "trusting nobody ignores the claim",
		proxies: handlers.TrustNoProxies,
		peer:    outsider,
		allowed: false,
	}, {
		name:    "allow-list ignores an unlisted peer's claim",
		proxies: "192.0.2.7",
		peer:    outsider,
		allowed: false,
	}, {
		// The allow-list must not break the deployment it exists to serve.
		name:    "allow-list still honors its own proxy",
		proxies: "192.0.2.7",
		peer:    proxy,
		allowed: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				os.Unsetenv(handlers.EnvTrustedProxies)
				if err := handlers.ConfigureSourceIPTrust(); err != nil {
					t.Fatalf("restoring the default policy: %v", err)
				}
			})
			if tt.proxies == "" {
				os.Unsetenv(handlers.EnvTrustedProxies)
			} else {
				t.Setenv(handlers.EnvTrustedProxies, tt.proxies)
			}
			if err := handlers.ConfigureSourceIPTrust(); err != nil {
				t.Fatalf("configuring %q: %v", tt.proxies, err)
			}

			r, err := http.NewRequest(http.MethodGet, "http://minio.local/bkt/obj", nil)
			if err != nil {
				t.Fatal(err)
			}
			r.RemoteAddr = tt.peer
			r.Header.Set("X-Forwarded-For", forgedClaim)
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}

			got := bp.IsAllowed(policy.BucketPolicyArgs{
				Action:          policy.GetObjectAction,
				BucketName:      "bkt",
				ObjectName:      "obj",
				ConditionValues: getConditionValues(r, "us-east-1", auth.Credentials{AccessKey: "lowpriv"}),
			})
			if got != tt.allowed {
				t.Errorf("IsAllowed = %v, want %v (peer %s claiming %s)", got, tt.allowed, tt.peer, forgedClaim)
			}
		})
	}
}

// aws:SourceIp must be whatever the hardened resolver decided and nothing else.
// The resolver is where the forwarded-header trust policy is enforced and where
// its three modes are tested (internal/handlers/proxy_test.go); this pins the
// join, so the condition value cannot drift onto some other derivation that the
// policy would not cover.
//
// It also records the default-mode contract: with no trust policy configured,
// each of the three forwarded headers still sets aws:SourceIp, and so an
// IpAddress condition is only as good as the network path to the API port.
// Enforcing such a condition against a client with direct access requires
// MINIO_API_TRUSTED_PROXIES. Note that _MINIO_API_XFF_HEADER=off does not
// achieve it: the loop below covers all three headers precisely because
// suppressing one of them only moves the answer to the next.
func TestGetConditionValuesSourceIPMatchesResolver(t *testing.T) {
	for _, header := range []map[string]string{
		nil,
		{"X-Forwarded-For": "10.1.2.3"},
		{"X-Real-IP": "10.1.2.3"},
		{"Forwarded": "for=10.1.2.3"},
		{"X-Forwarded-For": "10.1.2.3, 198.51.100.9"},
	} {
		r, err := http.NewRequest(http.MethodGet, "http://minio.local/bkt/obj", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.RemoteAddr = testCondRemoteILP
		for k, v := range header {
			r.Header.Set(k, v)
		}

		got := resolvedConditionValues(condValuesForRequest(t, "http://minio.local/bkt/obj", header), condition.AWSSourceIP.ToKey().Name())
		want := handlers.GetSourceIPRaw(r)
		if len(got) != 1 || got[0] != want {
			t.Errorf("headers %v: aws:SourceIp = %v, resolver returned %q", header, got, want)
		}
	}
}

// "Deny unless the connection is TLS" is the usual hardening statement, and
// aws:SecureTransport is computed from r.TLS.
func TestBucketPolicySecureTransportCannotBeForged(t *testing.T) {
	fn, err := condition.NewBoolFunc(condition.AWSSecureTransport.ToKey(), false)
	if err != nil {
		t.Fatal(err)
	}
	bp := policy.BucketPolicy{
		Version: policy.DefaultVersion,
		Statements: []policy.BPStatement{
			{
				Effect: policy.Allow, Principal: policy.NewPrincipal("*"),
				Actions:   policy.NewActionSet(policy.GetObjectAction),
				Resources: policy.NewResourceSet(policy.NewResource("bkt/*")),
			},
			{
				Effect: policy.Deny, Principal: policy.NewPrincipal("*"),
				Actions:    policy.NewActionSet(policy.GetObjectAction),
				Resources:  policy.NewResourceSet(policy.NewResource("bkt/*")),
				Conditions: condition.NewFunctions(fn),
			},
		},
	}
	allowed := func(rawURL string, header map[string]string) bool {
		return bp.IsAllowed(policy.BucketPolicyArgs{
			Action:          policy.GetObjectAction,
			BucketName:      "bkt",
			ObjectName:      "obj",
			ConditionValues: condValuesForRequest(t, rawURL, header),
		})
	}

	// r.TLS is nil throughout, so every one of these is a plaintext request.
	if allowed("http://minio.local/bkt/obj", nil) {
		t.Fatal("baseline: a plaintext request must be denied")
	}
	if allowed("http://minio.local/bkt/obj?SecureTransport=true", nil) {
		t.Error("a query parameter forged aws:SecureTransport")
	}
	if allowed("http://minio.local/bkt/obj", map[string]string{"Securetransport": "true"}) {
		t.Error("a header forged aws:SecureTransport")
	}
}

// Reserving the server's own keys must not stop the request from supplying the
// values that are client-derived by design.
func TestGetConditionValuesKeepsClientDerivedKeys(t *testing.T) {
	got := condValuesForRequest(t, "http://minio.local/bkt/obj?prefix=team%2F",
		map[string]string{
			xhttp.AmzObjectLockMode:       "GOVERNANCE",
			xhttp.AmzServerSideEncryption: "aws:kms",
			"X-Amz-Meta-Team":             "storage",
			xhttp.AmzObjectTagging:        "project=silo",
		})

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"Object-Lock-Mode", "GOVERNANCE"},
		{xhttp.AmzServerSideEncryption, "aws:kms"},
		{"X-Amz-Meta-Team", "storage"},
		{"RequestObjectTag/project", "silo"},
		{"prefix", "team/"},
	} {
		if !slices.Contains(got[tc.key], tc.want) {
			t.Errorf("%s: expected %q, got %v", tc.key, tc.want, got[tc.key])
		}
	}
	if !slices.Contains(got["RequestObjectTagKeys"], "project") {
		t.Errorf("RequestObjectTagKeys: expected project, got %v", got["RequestObjectTagKeys"])
	}

	if len(got["ExistingObjectTag/project"]) != 0 {
		t.Errorf("request tags leaked into ExistingObjectTag: %v", got["ExistingObjectTag/project"])
	}
}

func TestGetConditionValuesSeparatesRequestAndExistingTags(t *testing.T) {
	got := condValuesForRequestWithExistingTags(t, "http://minio.local/bkt/obj",
		map[string]string{xhttp.AmzObjectTagging: "project=request&new=yes"},
		"project=stored&old=yes")

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"RequestObjectTag/project", "request"},
		{"RequestObjectTag/new", "yes"},
		{"ExistingObjectTag/project", "stored"},
		{"ExistingObjectTag/old", "yes"},
	} {
		if !slices.Equal(got[tc.key], []string{tc.want}) {
			t.Errorf("%s: expected %q, got %v", tc.key, tc.want, got[tc.key])
		}
	}
	if len(got["ExistingObjectTag/new"]) != 0 || len(got["RequestObjectTag/old"]) != 0 {
		t.Errorf("tag sources crossed: request new=%v, existing old=%v",
			got["ExistingObjectTag/new"], got["RequestObjectTag/old"])
	}
}

// Keys the server did not populate for this request are as forgeable as ones it
// did, so the reservation cannot depend on presence.
func TestGetConditionValuesRejectsAbsentInternalKeys(t *testing.T) {
	for _, key := range []string{
		"signatureAge",
		"groups",
		"DurationSeconds",
		"ExistingObjectTag/security",
		"RequestObjectTag/security",
		"RequestObjectTagKeys",
		"object-lock-mode",
		"object-lock-remaining-retention-days",
	} {
		t.Run(key, func(t *testing.T) {
			got := condValuesForRequest(t,
				"http://minio.local/bkt/obj?"+url.Values{key: {"ATTACKER"}}.Encode(), nil)
			if slices.Contains(got[key], "ATTACKER") {
				t.Errorf("?%s= was accepted into the condition values as %v", key, got[key])
			}
		})
	}
}

func TestGetConditionValuesOnlyAcceptsPresignedSignatureAge(t *testing.T) {
	const signatureAgeHeader = "x-amz-signature-age"

	for _, tc := range []struct {
		name    string
		target  string
		headers map[string]string
		want    bool
	}{
		{
			name:    "anonymous client header",
			target:  "http://minio.local/bkt/obj",
			headers: map[string]string{signatureAgeHeader: "1"},
		},
		{
			name:   "header-signed client header",
			target: "http://minio.local/bkt/obj",
			headers: map[string]string{
				xhttp.Authorization: signV4Algorithm + " attacker",
				signatureAgeHeader:  "1",
			},
		},
		{
			name: "presigned verifier value",
			target: "http://minio.local/bkt/obj?" + url.Values{
				xhttp.AmzCredential: {"access/20260803/us-east-1/s3/aws4_request"},
			}.Encode(),
			headers: map[string]string{signatureAgeHeader: "250"},
			want:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := condValuesForRequest(t, tc.target, tc.headers)
			_, ok := got["signatureAge"]
			if ok != tc.want {
				t.Fatalf("signatureAge presence: expected %v, got %v", tc.want, got["signatureAge"])
			}
		})
	}
}

// The object-lock value is stored under the header spelling while the policy key
// that reads it is lower case. Reserving only one spelling lets the other be
// supplied and resolved in its place - which the policy package's exact-name
// lookup then prefers over the real one.
func TestGetConditionValuesObjectLockSpelling(t *testing.T) {
	got := condValuesForRequest(t,
		"http://minio.local/bkt/obj?object-lock-mode=COMPLIANCE",
		map[string]string{xhttp.AmzObjectLockMode: "GOVERNANCE"})

	if v, ok := got["object-lock-mode"]; ok {
		t.Errorf("the lower-case spelling was accepted: %v", v)
	}
	if !slices.Equal(got["Object-Lock-Mode"], []string{"GOVERNANCE"}) {
		t.Errorf("expected the header value to stand, got %v", got["Object-Lock-Mode"])
	}

	fn, err := condition.NewStringEqualsFunc("",
		condition.S3ObjectLockMode.ToKey(), "COMPLIANCE")
	if err != nil {
		t.Fatal(err)
	}
	if condition.NewFunctions(fn).Evaluate(got) {
		t.Error("a policy requiring COMPLIANCE was satisfied by a GOVERNANCE request")
	}
}

// Resource variables read the condition map directly, so a forgeable key is a
// forgeable resource path. ${ldap:user} and ${jwt:preferred_username} are the
// home-directory idiom for LDAP and OIDC deployments; the server derives them
// from the credential, and a request must not be able to answer them.
func TestBucketPolicyResourceVariableCannotBeForged(t *testing.T) {
	for _, tc := range []struct{ variable, param, value string }{
		{"${ldap:user}", "user", "alice"},
		{"${ldap:username}", "username", "alice"},
		{"${jwt:preferred_username}", "preferred_username", "alice"},
		{"${jwt:sub}", "sub", "alice"},
		{"${aws:username}", "username", "alice"},
	} {
		t.Run(tc.variable, func(t *testing.T) {
			bp := policy.BucketPolicy{Version: policy.DefaultVersion, Statements: []policy.BPStatement{{
				Effect:    policy.Allow,
				Principal: policy.NewPrincipal("*"),
				Actions:   policy.NewActionSet(policy.GetObjectAction),
				Resources: policy.NewResourceSet(policy.NewResource("bkt/" + tc.variable + "/*")),
			}}}
			args := policy.BucketPolicyArgs{
				Action: policy.GetObjectAction, BucketName: "bkt", ObjectName: tc.value + "/secret",
			}
			args.ConditionValues = condValuesForRequest(t,
				"http://minio.local/bkt/"+tc.value+"/secret?"+
					url.Values{tc.param: {tc.value}}.Encode(), nil)
			if bp.IsAllowed(args) {
				t.Errorf("?%s=%s expanded %s and granted the prefix", tc.param, tc.value, tc.variable)
			}
		})
	}
}
