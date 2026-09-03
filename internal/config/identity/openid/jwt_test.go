// Copyright (c) 2015-2022 MinIO, Inc.
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

package openid

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v4"
	"github.com/minio/minio/internal/arn"
	"github.com/minio/minio/internal/config"
	jwtm "github.com/minio/minio/internal/jwt"
	xnet "github.com/pgsty/silo-pkg/v3/net"
)

func TestUpdateClaimsExpiry(t *testing.T) {
	testCases := []struct {
		exp             any
		dsecs           string
		expectedFailure bool
	}{
		{"", "", true},
		{"-1", "0", true},
		{"-1", "900", true},
		{"1574812326", "900", false},
		{1574812326, "900", false},
		{int64(1574812326), "900", false},
		{int(1574812326), "900", false},
		{uint(1574812326), "900", false},
		{uint64(1574812326), "900", false},
		{json.Number("1574812326"), "900", false},
		{1574812326.000, "900", false},
		{time.Duration(3) * time.Minute, "900", false},
	}

	for _, testCase := range testCases {
		t.Run("", func(t *testing.T) {
			claims := map[string]any{}
			claims["exp"] = testCase.exp
			err := updateClaimsExpiry(testCase.dsecs, claims)
			if err != nil && !testCase.expectedFailure {
				t.Errorf("Expected success, got failure %s", err)
			}
			if err == nil && testCase.expectedFailure {
				t.Error("Expected failure, got success")
			}
		})
	}
}

func initJWKSServer() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const jsonkey = `{"keys":
       [
         {"kty":"RSA",
          "n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
          "e":"AQAB",
          "alg":"RS256",
          "kid":"2011-04-29"}
       ]
     }`
		w.Write([]byte(jsonkey))
	}))
	return server
}

func mustGenerateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	return privateKey
}

func mustMarshalRSAJWKS(t *testing.T, kid string, publicKey *rsa.PublicKey) []byte {
	t.Helper()

	jwks := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"kid": kid,
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
			},
		},
	}

	body, err := json.Marshal(jwks)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

func initCountingJWKSServer(body []byte) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	return server, &hits
}

func newTestOpenIDConfig(t *testing.T, serverURL, clientID, clientSecret string) Config {
	t.Helper()

	u, err := xnet.ParseHTTPURL(serverURL)
	if err != nil {
		t.Fatal(err)
	}

	provider := providerCfg{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}
	provider.JWKS.URL = u

	return Config{
		Enabled: true,
		pubKeys: publicKeys{
			RWMutex: &sync.RWMutex{},
			pkMap:   map[string]crypto.PublicKey{},
		},
		arnProviderCfgsMap: map[arn.ARN]*providerCfg{
			DummyRoleARN: &provider,
		},
		ProviderCfgs: map[string]*providerCfg{
			"1": &provider,
		},
		closeRespFn: func(rc io.ReadCloser) {
			rc.Close()
		},
	}
}

func mustSignRS256Token(t *testing.T, privateKey *rsa.PrivateKey, kid, audience, subject string) string {
	t.Helper()

	token := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": subject,
	})
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	return tokenString
}

func TestJWTRejectsHMACType(t *testing.T) {
	server := initJWKSServer()
	defer server.Close()

	jwt := &jwtgo.Token{
		Method: jwtgo.SigningMethodHS256,
		Claims: jwtgo.StandardClaims{
			ExpiresAt: 253428928061,
			Audience:  "76b95ae5-33ef-4283-97b7-d2a85dc2d8f4",
		},
		Header: map[string]any{
			"typ": "JWT",
			"alg": jwtgo.SigningMethodHS256.Alg(),
			"kid": "76b95ae5-33ef-4283-97b7-d2a85dc2d8f4",
		},
	}

	token, err := jwt.SignedString([]byte("WNGvKVyyNmXq0TraSvjaDN9CtpFgx35IXtGEffMCPR0"))
	if err != nil {
		t.Fatal(err)
	}

	u1, err := xnet.ParseHTTPURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	pubKeys := publicKeys{
		RWMutex: &sync.RWMutex{},
		pkMap:   map[string]crypto.PublicKey{},
	}

	provider := providerCfg{
		ClientID:     "76b95ae5-33ef-4283-97b7-d2a85dc2d8f4",
		ClientSecret: "WNGvKVyyNmXq0TraSvjaDN9CtpFgx35IXtGEffMCPR0",
	}
	provider.JWKS.URL = u1
	cfg := Config{
		Enabled: true,
		pubKeys: pubKeys,
		arnProviderCfgsMap: map[arn.ARN]*providerCfg{
			DummyRoleARN: &provider,
		},
		ProviderCfgs: map[string]*providerCfg{
			"1": &provider,
		},
		closeRespFn: func(rc io.ReadCloser) {
			rc.Close()
		},
	}

	if err = cfg.PopulatePublicKey(DummyRoleARN); err != nil {
		t.Fatal(err)
	}
	if cfg.pubKeys.get(provider.ClientID) != nil {
		t.Fatal("client secret must not be used as a JWT verification key")
	}

	var claims jwtgo.MapClaims
	if err = cfg.Validate(t.Context(), DummyRoleARN, token, "", "", claims); err == nil {
		t.Fatal("expected HS256 ID token to be rejected")
	}
}

func TestJWTAcceptsRS256(t *testing.T) {
	const (
		clientID = "client-id"
		keyID    = "rsa-kid"
		subject  = "direct-rs256-user"
	)

	privateKey := mustGenerateRSAKey(t)
	server, hits := initCountingJWKSServer(mustMarshalRSAJWKS(t, keyID, &privateKey.PublicKey))
	defer server.Close()

	cfg := newTestOpenIDConfig(t, server.URL, clientID, "unused-secret")
	if err := cfg.PopulatePublicKey(DummyRoleARN); err != nil {
		t.Fatal(err)
	}

	token := mustSignRS256Token(t, privateKey, keyID, clientID, subject)
	claims := map[string]any{}
	if err := cfg.Validate(t.Context(), DummyRoleARN, token, "", "", claims); err != nil {
		t.Fatalf("expected RS256 ID token to be accepted, got: %v", err)
	}
	if got := claims["sub"]; got != subject {
		t.Fatalf("expected sub claim %q, got %v", subject, got)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected only the initial JWKS fetch, got %d requests", hits.Load())
	}
}

func TestJWTRetryRefreshesPublicKey(t *testing.T) {
	const (
		clientID = "client-id"
		keyID    = "rotated-rsa-kid"
		subject  = "retry-rs256-user"
	)

	privateKey := mustGenerateRSAKey(t)
	server, hits := initCountingJWKSServer(mustMarshalRSAJWKS(t, keyID, &privateKey.PublicKey))
	defer server.Close()

	cfg := newTestOpenIDConfig(t, server.URL, clientID, "unused-secret")
	token := mustSignRS256Token(t, privateKey, keyID, clientID, subject)

	claims := map[string]any{}
	if err := cfg.Validate(t.Context(), DummyRoleARN, token, "", "", claims); err != nil {
		t.Fatalf("expected retry path to refresh JWKS and accept the token, got: %v", err)
	}
	if got := claims["sub"]; got != subject {
		t.Fatalf("expected sub claim %q, got %v", subject, got)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one JWKS refresh during retry, got %d requests", hits.Load())
	}
}

func TestJWTRetryStillRejectsHMACType(t *testing.T) {
	const (
		clientID = "76b95ae5-33ef-4283-97b7-d2a85dc2d8f4"
		secret   = "WNGvKVyyNmXq0TraSvjaDN9CtpFgx35IXtGEffMCPR0"
		rsaKeyID = "jwks-rsa-kid"
	)

	privateKey := mustGenerateRSAKey(t)
	server, hits := initCountingJWKSServer(mustMarshalRSAJWKS(t, rsaKeyID, &privateKey.PublicKey))
	defer server.Close()

	cfg := newTestOpenIDConfig(t, server.URL, clientID, secret)

	// Seed the old HMAC material explicitly to prove the retry parser still
	// rejects HS256 before consulting the keyring.
	cfg.pubKeys.add(clientID, []byte(secret))

	jwt := &jwtgo.Token{
		Method: jwtgo.SigningMethodHS256,
		Claims: jwtgo.StandardClaims{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			Audience:  clientID,
		},
		Header: map[string]any{
			"typ": "JWT",
			"alg": jwtgo.SigningMethodHS256.Alg(),
			"kid": clientID,
		},
	}

	token, err := jwt.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	claims := map[string]any{}
	err = cfg.Validate(t.Context(), DummyRoleARN, token, "", "", claims)
	if err == nil {
		t.Fatal("expected HS256 token to be rejected on retry")
	}
	if !strings.Contains(err.Error(), "signing method HS256 is invalid") {
		t.Fatalf("expected retry failure to come from ValidMethods, got: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one JWKS refresh before retry rejection, got %d requests", hits.Load())
	}
}

func TestJWT(t *testing.T) {
	const jsonkey = `{"keys":
       [
         {"kty":"RSA",
          "n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
          "e":"AQAB",
          "alg":"RS256",
          "kid":"2011-04-29"}
       ]
     }`

	pubKeys := publicKeys{
		RWMutex: &sync.RWMutex{},
		pkMap:   map[string]crypto.PublicKey{},
	}
	err := pubKeys.parseAndAdd(bytes.NewBuffer([]byte(jsonkey)))
	if err != nil {
		t.Fatal("Error loading pubkeys:", err)
	}
	if len(pubKeys.pkMap) != 1 {
		t.Fatalf("Expected 1 keys, got %d", len(pubKeys.pkMap))
	}

	u1, err := xnet.ParseHTTPURL("http://127.0.0.1:8443")
	if err != nil {
		t.Fatal(err)
	}

	provider := providerCfg{}
	provider.JWKS.URL = u1
	cfg := Config{
		Enabled: true,
		pubKeys: pubKeys,
		arnProviderCfgsMap: map[arn.ARN]*providerCfg{
			DummyRoleARN: &provider,
		},
		ProviderCfgs: map[string]*providerCfg{
			"1": &provider,
		},
	}

	u, err := url.Parse("http://127.0.0.1:8443/?Token=invalid")
	if err != nil {
		t.Fatal(err)
	}

	var claims jwtgo.MapClaims
	if err = cfg.Validate(t.Context(), DummyRoleARN, u.Query().Get("Token"), "", "", claims); err == nil {
		t.Fatal(err)
	}
}

func TestDefaultExpiryDuration(t *testing.T) {
	testCases := []struct {
		reqURL    string
		duration  time.Duration
		expectErr bool
	}{
		{
			reqURL:   "http://127.0.0.1:8443/?Token=xxxxx",
			duration: time.Duration(60) * time.Minute,
		},
		{
			reqURL:    "http://127.0.0.1:8443/?DurationSeconds=9s",
			expectErr: true,
		},
		{
			reqURL:    "http://127.0.0.1:8443/?DurationSeconds=31536001",
			expectErr: true,
		},
		{
			reqURL:    "http://127.0.0.1:8443/?DurationSeconds=800",
			expectErr: true,
		},
		{
			reqURL:   "http://127.0.0.1:8443/?DurationSeconds=901",
			duration: time.Duration(901) * time.Second,
		},
	}

	for i, testCase := range testCases {
		u, err := url.Parse(testCase.reqURL)
		if err != nil {
			t.Fatal(err)
		}
		d, err := GetDefaultExpiration(u.Query().Get("DurationSeconds"))
		gotErr := (err != nil)
		if testCase.expectErr != gotErr {
			t.Errorf("Test %d: Expected %v, got %v with error %s", i+1, testCase.expectErr, gotErr, err)
		}
		if d != testCase.duration {
			t.Errorf("Test %d: Expected duration %d, got %d", i+1, testCase.duration, d)
		}
	}
}

func TestExpCorrect(t *testing.T) {
	signKey, _ := base64.StdEncoding.DecodeString("NTNv7j0TuYARvmNMmWXo6fKvM4o6nv/aUi9ryX38ZH+L1bkrnD1ObOQ8JAUmHCBq7Iy7otZcyAagBLHVKvvYaIpmMuxmARQ97jUVG16Jkpkp1wXOPsrF9zwew6TpczyHkHgX5EuLg2MeBuiT/qJACs1J0apruOOJCg/gOtkjB4c=")

	claimsMap := jwtm.NewMapClaims()
	claimsMap.SetExpiry(time.Now().Add(time.Minute))
	claimsMap.SetAccessKey("test-access")
	if err := updateClaimsExpiry("3600", claimsMap.MapClaims); err != nil {
		t.Error(err)
	}
	// Build simple token with updated expiration claim
	token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claimsMap)
	tokenString, err := token.SignedString(signKey)
	if err != nil {
		t.Error(err)
	}

	// Parse token to be sure it is valid
	err = jwtm.ParseWithClaims(tokenString, claimsMap, func(*jwtm.MapClaims) ([]byte, error) {
		return signKey, nil
	})
	if err != nil {
		t.Error(err)
	}
}

func TestKeycloakProviderInitialization(t *testing.T) {
	testConfig := providerCfg{
		DiscoveryDoc: DiscoveryDoc{
			TokenEndpoint: "http://keycloak.test/token/endpoint",
		},
	}
	testKvs := config.KVS{}
	testKvs.Set(Vendor, "keycloak")
	testKvs.Set(KeyCloakRealm, "TestRealm")
	testKvs.Set(KeyCloakAdminURL, "http://keycloak.test/auth/admin")
	cfgGet := func(param string) string {
		return testKvs.Get(param)
	}

	if testConfig.provider != nil {
		t.Errorf("Empty config cannot have any provider!")
	}

	if err := testConfig.initializeProvider(cfgGet, http.DefaultTransport); err != nil {
		t.Error(err)
	}

	if testConfig.provider == nil {
		t.Errorf("keycloak provider must be initialized!")
	}
}
