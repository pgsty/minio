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

// Originally from https://github.com/gorilla/handlers with following license
// https://raw.githubusercontent.com/gorilla/handlers/master/LICENSE, forked
// and heavily modified for MinIO's internal needs.

package handlers

import (
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/minio/minio/internal/config"
	"github.com/pgsty/silo-pkg/v3/env"
)

var (
	// De-facto standard header keys.
	xForwardedFor    = http.CanonicalHeaderKey("X-Forwarded-For")
	xForwardedHost   = http.CanonicalHeaderKey("X-Forwarded-Host")
	xForwardedPort   = http.CanonicalHeaderKey("X-Forwarded-Port")
	xForwardedProto  = http.CanonicalHeaderKey("X-Forwarded-Proto")
	xForwardedScheme = http.CanonicalHeaderKey("X-Forwarded-Scheme")
	xRealIP          = http.CanonicalHeaderKey("X-Real-IP")
)

var (
	// RFC7239 defines a new "Forwarded: " header designed to replace the
	// existing use of X-Forwarded-* headers.
	// e.g. Forwarded: for=192.0.2.60;proto=https;by=203.0.113.43
	forwarded = http.CanonicalHeaderKey("Forwarded")
	// Allows for a sub-match of the first value after 'for=' to the next
	// comma, semi-colon or space. The match is case-insensitive.
	forRegex = regexp.MustCompile(`(?i)(?:for=)([^(;|,| )]+)(.*)`)
	// Allows for a sub-match for the first instance of scheme (http|https)
	// prefixed by 'proto='. The match is case-insensitive.
	protoRegex = regexp.MustCompile(`(?i)^(;|,| )+(?:proto=)(https|http)`)
)

// Environment variables governing how much of a request's claimed source address
// the server is willing to believe.
const (
	// EnvXFFHeader disables processing of X-Forwarded-For, and only of
	// X-Forwarded-For. Inherited from upstream with its meaning deliberately
	// unchanged: it is a parsing switch, not a trust boundary. Setting it to
	// "off" still leaves X-Real-IP and RFC 7239 Forwarded honored, so it is not
	// a way to stop a client naming its own address - EnvTrustedProxies is.
	EnvXFFHeader = "_MINIO_API_XFF_HEADER"

	// EnvTrustedProxies selects the trust policy. Unset honors forwarded headers
	// from any peer, which is the historical behavior; TrustNoProxies believes
	// none of them; anything else is a list of peer addresses and CIDR blocks
	// whose headers are honored, which turns the source address from a claim any
	// client can make into one only a named proxy can make.
	EnvTrustedProxies = "MINIO_API_TRUSTED_PROXIES"

	// TrustNoProxies is the EnvTrustedProxies value that believes no forwarded
	// source-address header from anyone, whichever of the three it arrives in.
	TrustNoProxies = "none"
)

// sourceIPTrust decides which peers may tell the server where a request came
// from. The address they choose becomes aws:SourceIp and the audit client
// address, so this is an access-control decision, not a logging preference.
type sourceIPTrust int

const (
	// trustAnyPeer honors forwarded headers from whoever sent them. Historical
	// default, sound only where every route to the API port passes through a
	// proxy that overwrites those headers.
	trustAnyPeer sourceIPTrust = iota

	// trustNoPeer ignores forwarded headers; the source address is the TCP peer.
	trustNoPeer

	// trustListedPeers honors forwarded headers only from allow-listed peers.
	trustListedPeers
)

var (
	sourceIPPolicy sourceIPTrust
	trustedProxies config.TrustedProxies
)

// enableXFFHeader carries upstream's X-Forwarded-For parsing switch. It applies
// within whichever trust policy is in force, and is orthogonal to it.
//
// Read at package initialisation, exactly as upstream does, and deliberately not
// re-read by ConfigureSourceIPTrust. Environment files are loaded after this
// point, so upstream silently ignores the setting when it is written there;
// picking it up would make an already-deployed setting start taking effect,
// which is a behavior change this fork has no reason to make on its way past.
var enableXFFHeader = env.Get(EnvXFFHeader, config.EnableOn) == config.EnableOn

// init establishes a policy from the process environment so that no code path
// runs without one. A server re-applies it from ConfigureSourceIPTrust once the
// environment is complete; an error here is dropped because the failure mode it
// leaves behind - trustNoPeer - is the safe one, and it is reported there.
func init() {
	_ = ConfigureSourceIPTrust()
}

// ConfigureSourceIPTrust reads the trust policy out of the environment and
// installs it. The server calls this after loading MINIO_CONFIG_ENV_FILE, which
// happens long after package initialisation: a policy read only at init would
// miss every deployment that configures MinIO through an environment file and
// would silently leave the historical trust-any-peer mode in place.
//
// Not safe to call once requests are being served.
func ConfigureSourceIPTrust() error {
	// Read through LookupEnv rather than env.Get, which discards the error from a
	// remote env:// lookup and hands back the empty string. That would read here
	// as "unset" and quietly reinstate the trust-any-peer default: a fetch that
	// failed is not a statement that no proxy is trusted.
	value, _, _, err := env.LookupEnv(EnvTrustedProxies)
	if err != nil {
		sourceIPPolicy, trustedProxies = trustNoPeer, nil
		return config.Errorf("%s could not be read: %v", EnvTrustedProxies, err)
	}

	policy, prefixes, err := lookupSourceIPTrust(value)
	sourceIPPolicy, trustedProxies = policy, prefixes
	return err
}

// lookupSourceIPTrust derives the trust policy from EnvTrustedProxies. A
// malformed allow-list yields trustNoPeer alongside the error, so that a caller
// which fails to check the error still fails closed.
func lookupSourceIPTrust(proxies string) (sourceIPTrust, config.TrustedProxies, error) {
	switch strings.ToLower(strings.TrimSpace(proxies)) {
	case "":
		return trustAnyPeer, nil, nil
	case TrustNoProxies, config.EnableOff:
		return trustNoPeer, nil, nil
	}

	prefixes, err := config.ParseTrustedProxies(proxies, EnvTrustedProxies)
	if err != nil {
		return trustNoPeer, nil, err
	}
	if len(prefixes) == 0 {
		// Separators and nothing else. The value is not blank, so it was written
		// on purpose, yet it names no proxy. Falling back to the permissive
		// default here would answer a deliberate configuration with the one
		// behavior it cannot have been asking for.
		return trustNoPeer, nil, config.Errorf("%s %q names no proxy", EnvTrustedProxies, proxies)
	}
	return trustListedPeers, prefixes, nil
}

// GetSourceScheme retrieves the scheme from the X-Forwarded-Proto and RFC7239
// Forwarded headers (in that order).
func GetSourceScheme(r *http.Request) string {
	var scheme string

	// Retrieve the scheme from X-Forwarded-Proto.
	if proto := r.Header.Get(xForwardedProto); proto != "" {
		scheme = strings.ToLower(proto)
	} else if proto = r.Header.Get(xForwardedScheme); proto != "" {
		scheme = strings.ToLower(proto)
	} else if proto := r.Header.Get(forwarded); proto != "" {
		// match should contain at least two elements if the protocol was
		// specified in the Forwarded header. The first element will always be
		// the 'for=', which we ignore, subsequently we proceed to look for
		// 'proto=' which should precede right after `for=` if not
		// we simply ignore the values and return empty. This is in line
		// with the approach we took for returning first ip from multiple
		// params.
		if match := forRegex.FindStringSubmatch(proto); len(match) > 1 {
			if match = protoRegex.FindStringSubmatch(match[2]); len(match) > 1 {
				scheme = strings.ToLower(match[2])
			}
		}
	}

	return scheme
}

// GetSourceIPFromHeaders retrieves the client address a request claims to come
// from, or the empty string when no claim may be believed and the caller should
// fall back to the TCP peer.
//
// SECURITY CONTRACT. The value returned here becomes aws:SourceIp and the audit
// log's client address, so whoever controls it controls both IP-based policy
// decisions and the attribution of every logged action. Which of the three
// interchangeable source-address headers - X-Forwarded-For, X-Real-IP, RFC 7239
// Forwarded - a client sends is irrelevant; they are equally forgeable, so the
// trust decision is taken over all three at once by EnvTrustedProxies:
//
//   - Unset (default). Any peer may set the headers, and the left-most
//     X-Forwarded-For entry wins. aws:SourceIp is then only as trustworthy as
//     the network: any client that can open a connection to the API port can
//     name its own address. An IpAddress condition is not enforceable under this
//     mode unless every route to the port passes through a proxy that overwrites
//     all three headers. Note that the stock nginx recipe,
//     proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for, appends
//     rather than overwrites, and so leaves a client-supplied entry in the
//     left-most position where this mode will read it.
//
//   - TrustNoProxies. No header is believed; the source address is always the
//     TCP peer.
//
//   - A list of addresses and CIDR blocks. Headers are believed only when the
//     TCP peer is on the allow-list, and the chains are read right-to-left. This
//     is the only mode under which aws:SourceIp is enforceable against a client
//     with direct network access.
//
// EnvXFFHeader is not one of these modes. It suppresses parsing of
// X-Forwarded-For within whichever mode is in force, leaving X-Real-IP and
// Forwarded honored, so it cannot stop a client naming its own address - a
// client refused one header simply sends another. It is kept at its upstream
// meaning rather than widened into a trust switch, because widening it would
// change what an already-deployed setting resolves to; TrustNoProxies is the
// setting that means what it says.
//
// The scheme headers are deliberately not covered: GetSourceScheme feeds the
// Location URL rather than a policy decision, and suppressing it would hand
// http:// URLs to every deployment terminating TLS at a proxy.
func GetSourceIPFromHeaders(r *http.Request) string {
	switch sourceIPPolicy {
	case trustNoPeer:
		return ""
	case trustListedPeers:
		if !peerMayForward(r) {
			return ""
		}
		return forwardedSourceIP(r)
	default:
		return unverifiedSourceIP(r)
	}
}

// unverifiedSourceIP reads the headers the way MinIO always has, taking the
// left-most X-Forwarded-For entry and falling back through X-Real-IP to RFC 7239
// Forwarded. Every value here is a claim by whoever sent it.
func unverifiedSourceIP(r *http.Request) string {
	var addr string

	if enableXFFHeader {
		if fwd := r.Header.Get(xForwardedFor); fwd != "" {
			// Only grab the first (client) address. Note that '192.168.0.1,
			// 10.1.1.1' is a valid key for X-Forwarded-For where addresses after
			// the first may represent forwarding proxies earlier in the chain.
			s := strings.Index(fwd, ", ")
			if s == -1 {
				s = len(fwd)
			}
			addr = fwd[:s]
		}
	}

	if addr == "" {
		if fwd := r.Header.Get(xRealIP); fwd != "" {
			// X-Real-IP should only contain one IP address (the client making the
			// request).
			addr = fwd
		} else if fwd := r.Header.Get(forwarded); fwd != "" {
			// match should contain at least two elements if the protocol was
			// specified in the Forwarded header. The first element will always be
			// the 'for=' capture, which we ignore. In the case of multiple IP
			// addresses (for=8.8.8.8, 8.8.4.4, 172.16.1.20 is valid) we only
			// extract the first, which should be the client IP.
			if match := forRegex.FindStringSubmatch(fwd); len(match) > 1 {
				// IPv6 addresses in Forwarded headers are quoted-strings. We strip
				// these quotes.
				addr = strings.Trim(match[1], `"`)
			}
		}
	}

	return addr
}

// forwardedSourceIP resolves the client address for a request whose peer is an
// allow-listed proxy.
//
// The forwarding chains are read right-to-left, stepping over entries that name
// a configured proxy, and the first remaining address wins. That direction is
// what makes the header usable: each proxy appends the peer it actually saw, so
// an entry a client injected sits to the left of the one its proxy wrote, and
// the walk stops before reaching it.
//
// That holds only while the allow-list names proxies. A list broad enough to
// cover addresses clients also occupy makes those clients skippable too, and the
// walk then continues past a real client into whatever it placed to the left. A
// broad list therefore does not merely trust more peers - it lets those peers
// forge. Configure proxy addresses, not the subnet the proxies sit in.
//
// X-Real-IP carries no chain and so cannot be checked against the allow-list; it
// is taken at face value, and only when the chain headers yield nothing. The
// deployment contract is that a configured proxy overwrites whichever headers it
// sets. A proxy that instead relays a client's copy is choosing to let the
// client answer this question, and no amount of parsing here can undo that.
//
// Note this orders the headers differently from getSTSLDAPTrustedProxySourceIP
// (cmd/sts-handlers.go), which prefers X-Real-IP. Neither order is safe for
// every proxy - preferring X-Real-IP is wrong where the proxy authors only
// X-Forwarded-For and relays the client's X-Real-IP (AWS ALB), and preferring
// X-Forwarded-For is wrong in the mirror case (an nginx that sets only
// X-Real-IP). The chain-validated header is preferred here because this decides
// access control rather than rate-limit bucketing, so the value that can be
// checked against the allow-list should win; it also keeps the header precedence
// identical to the default mode. Deployments whose proxy authors only X-Real-IP
// must strip X-Forwarded-For at the edge.
func forwardedSourceIP(r *http.Request) string {
	if enableXFFHeader {
		if addr := untrustedHop(r.Header.Values(xForwardedFor), canonicalSourceIP); addr != "" {
			return addr
		}
	}
	if addr := canonicalSourceIP(lastValue(r.Header.Values(xRealIP))); addr != "" {
		return addr
	}
	return untrustedHop(r.Header.Values(forwarded), forwardedForAddr)
}

// maxForwardedHops bounds how far back along a chain the walk will look.
//
// Real chains are a handful of hops and the answer sits at the right-hand end,
// so this is far above anything a deployment produces. It exists because the
// chain arrives from the network: without it, a client behind a trusted proxy
// could spend a megabyte of header on a walk this server has to finish. Running
// out of budget yields no address, so the request falls back to the peer - the
// same safe direction as a chain of entirely trusted hops.
const maxForwardedHops = 100

// lastValue returns the final line of a repeated header. X-Real-IP carries no
// chain to walk, so where a client's line and a proxy's line both survive, the
// later one is the one added closer to this server.
func lastValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// untrustedHop walks a forwarding chain from the right and returns the first
// address that is not itself a trusted hop, using addrOf to read one element.
//
// values holds the header's lines in the order received; repeated field lines
// are equivalent to one comma-joined line, and proxies disagree on which they
// emit - Go's reverse proxy and nginx rewrite a single line, while HAProxy's
// forwardfor adds a second. Reading only the first would leave a client's own
// line ahead of the proxy's, which is the position this walk exists to step over.
//
// The scan runs backwards over the raw text rather than over a split slice, so
// that a long chain costs no allocation.
func untrustedHop(values []string, addrOf func(string) string) string {
	budget := maxForwardedHops
	for i := len(values) - 1; i >= 0 && budget > 0; i-- {
		for s := values[i]; len(s) > 0 && budget > 0; budget-- {
			element := s
			if comma := strings.LastIndexByte(s, ','); comma >= 0 {
				element, s = s[comma+1:], s[:comma]
			} else {
				s = ""
			}
			if addr := addrOf(element); addr != "" && !isTrustedHop(addr) {
				return addr
			}
		}
	}
	return ""
}

// forwardedForAddr reads the for= address out of one RFC 7239 Forwarded element.
func forwardedForAddr(element string) string {
	match := forRegex.FindStringSubmatch(element)
	if len(match) <= 1 {
		return ""
	}
	return canonicalSourceIP(strings.Trim(match[1], `"`))
}

// canonicalSourceIP reduces one chain element to a bare IP address, or to the
// empty string when it does not hold one. Ports, brackets and surrounding space
// are stripped. Values an allow-list cannot reason about - a hostname, or an
// RFC 7239 obfuscated identifier such as for=_gazonk - are discarded rather than
// passed on, since a trust decision cannot be made about them.
func canonicalSourceIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	addr = strings.TrimPrefix(addr, "[")
	addr = strings.TrimSuffix(addr, "]")
	// A link-local peer arrives with a zone ("fe80::1%eth0"), which net.ParseIP
	// rejects outright - so without this the address resolves to nothing and the
	// peer could never be a configured proxy.
	if zone := strings.IndexByte(addr, '%'); zone != -1 {
		addr = addr[:zone]
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}
	return ""
}

// isTrustedHop reports whether a chain entry names one of the configured
// proxies, and so is an address to step over rather than attribute a request to.
//
// This deliberately does not extend the loopback exemption peerMayForward
// grants. Loopback is trusted as a *peer* because the local front-ends connect
// from there; a 127.0.0.1 entry inside a forwarding chain is just an address,
// and stepping over it would discard a real answer in favor of whatever sits
// further left.
func isTrustedHop(addr string) bool {
	return trustedProxies.Contains(addr)
}

// peerMayForward reports whether the request's TCP peer is allowed to speak for
// someone else.
//
// Loopback is always allowed, configured or not. The FTP and SFTP front-ends
// reach the S3 layer over 127.0.0.1 and declare their session's client with
// X-Forwarded-For (see cmd/sftp-server-driver.go), so excluding loopback would
// attribute every FTP and SFTP request to the server itself.
func peerMayForward(r *http.Request) bool {
	peer := canonicalSourceIP(r.RemoteAddr)
	if peer == "" {
		return false
	}
	if ip := net.ParseIP(peer); ip != nil && ip.IsLoopback() {
		return true
	}
	return trustedProxies.Contains(peer)
}

// TrustsForwardedHeaders reports whether the source-address headers already
// present on a request may be believed. The node-to-node forwarder uses this to
// decide whether to relay what it received or overwrite it.
func TrustsForwardedHeaders(r *http.Request) bool {
	switch sourceIPPolicy {
	case trustNoPeer:
		return false
	case trustListedPeers:
		return peerMayForward(r)
	default:
		return true
	}
}

// GetSourceIPRaw retrieves the IP from the request headers
// and falls back to r.RemoteAddr when necessary.
// however returns without bracketing.
func GetSourceIPRaw(r *http.Request) string {
	addr := GetSourceIPFromHeaders(r)
	if addr == "" {
		addr = r.RemoteAddr
	}

	// Default to remote address if headers not set.
	raddr, _, _ := net.SplitHostPort(addr)
	if raddr == "" {
		return addr
	}
	return raddr
}

// GetSourceIP retrieves the IP from the request headers
// and falls back to r.RemoteAddr when necessary.
func GetSourceIP(r *http.Request) string {
	addr := GetSourceIPRaw(r)
	if strings.ContainsRune(addr, ':') {
		return "[" + addr + "]"
	}
	return addr
}
