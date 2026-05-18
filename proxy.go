// Copyright 2019, 2021, 2022, 2023, 2024 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

var tlsClientConfig *tls.Config

// proxyAuthenticator is implemented by per-scheme authenticators
// (NTLM, Negotiate, Basic). The picker (*authChain) calls the
// predicate methods to decide whether and in what order to try each
// authenticator; proxy.go's retry helpers then call do() to actually
// perform the authenticated request.
//
// Picker call order, given a 407 response:
//
//  1. For each configured authenticator, the picker calls
//     applicableTo(proxyHost). False excludes the method silently;
//     the chain falls through to the next configured method.
//  2. Authenticators that survive (1) are intersected with the
//     advertised schemes (case-insensitive match against scheme()).
//     Methods not on that list are dropped.
//  3. If the proxy didn't advertise any parseable scheme, only
//     authenticators returning safeWithoutChallenge() == true are
//     kept. This is the F-1 / F-4 downgrade defence: schemes whose
//     first message contains credential material (Basic) MUST opt
//     out, otherwise a hostile endpoint returning a bare 407 could
//     harvest the credential.
//
// proxy.go's retry helpers iterate the resulting candidate list,
// calling do() on each in turn. Any error from do() aborts the chain.
// A 407 from do() is treated as "this method was rejected, try the
// next"; a non-407 response is returned to the caller.
type proxyAuthenticator interface {
	// scheme returns the HTTP authentication scheme this
	// authenticator handles (e.g. "Negotiate", "NTLM", "Basic").
	// Matched case-insensitively against the schemes the proxy
	// advertised in its Proxy-Authenticate response header.
	scheme() string

	// applicableTo reports whether this authenticator is willing to
	// authenticate against the given proxy host. Implementations may
	// use this to enforce policy (e.g. SPN allowlists for Kerberos,
	// or a state-of-the-world check such as "is my Kerberos ticket
	// still valid"). Returning false causes the picker to omit this
	// authenticator from the candidate list, so the chain falls
	// through to the next method instead of failing mid-flight.
	applicableTo(proxyHost string) bool

	// safeWithoutChallenge reports whether this authenticator may be
	// used against a proxy that returned 407 with no parseable
	// Proxy-Authenticate header. The contract is: "is my first
	// message safe to send before the proxy has explicitly named me
	// as a supported scheme?". Schemes whose first message contains
	// credential MATERIAL (e.g. Basic, where the first byte sent IS
	// the password) MUST return false. Schemes that initiate with a
	// non-credential probe (NTLM Type 1, SPNEGO initial token) may
	// return true even though those probes do contain identifying
	// information about the principal — they don't contain a secret
	// the attacker doesn't already have a chance to capture from
	// challenge-response traffic.
	safeWithoutChallenge() bool

	// do performs a single authenticated request after the proxy has
	// returned 407 Proxy Authentication Required. The implementation
	// MUST issue all of its round-trips (e.g. NTLM Type 1 / Type 3)
	// on the supplied RoundTripper; it must NOT re-dial. Iteration
	// across multiple authenticators is the caller's job.
	//
	// The implementation MAY set request headers (typically
	// Proxy-Authorization) on req before round-tripping. The picker
	// clears Proxy-Authorization between attempts so a method
	// doesn't have to.
	//
	// Body ownership: on success do() returns a non-nil response
	// whose Body the CALLER is responsible for closing. On error
	// do() returns (nil, err) and has cleaned up any internal state.
	do(req *http.Request, rt http.RoundTripper) (*http.Response, error)
}

// Authentication-scheme tokens, lower-cased and centralised so the rest
// of the code can refer to them without sprinkling string literals.
// These match the canonical IANA names for HTTP Authentication Schemes;
// the picker lower-cases at runtime so the values returned by each
// authenticator's scheme() method are matched case-insensitively against
// these constants and against the schemes advertised by the proxy.
const (
	schemeBasic     = "basic"     //nolint:unused // used by tests and intent docs
	schemeNTLM      = "ntlm"      //nolint:unused // used by tests and intent docs
	schemeNegotiate = "negotiate" //nolint:unused // used by tests and intent docs
)

// isToken reports whether s satisfies the RFC 7230 §3.2.6 "token"
// production. Used to defend against proxies (or attackers) sending
// malformed Proxy-Authenticate scheme names.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r > 0x7E {
			return false
		}
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			continue
		}
		switch r {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.',
			'^', '_', '`', '|', '~':
			continue
		}
		return false
	}
	return true
}

// parseProxyAuthenticateSchemes returns the deduplicated, lower-cased
// scheme names from a 407 response's Proxy-Authenticate header(s),
// preserving the order in which they appeared. RFC 7235 §4.3 allows
// either multiple Proxy-Authenticate header fields OR a single header
// field containing a comma-separated list of challenges (RFC 7230 list
// extension), so we honour both. The portion after the scheme token
// (realm, qop, base64 token68, etc.) is ignored — schemes that need it
// parse it themselves from the original response.
func parseProxyAuthenticateSchemes(header http.Header) []string {
	var schemes []string
	seen := make(map[string]bool)
	for _, value := range header.Values("Proxy-Authenticate") {
		for _, name := range splitChallengeNames(value) {
			name = strings.ToLower(name)
			if !isToken(name) || seen[name] {
				continue
			}
			seen[name] = true
			schemes = append(schemes, name)
		}
	}
	return schemes
}

// splitChallengeNames extracts the scheme names from a single
// Proxy-Authenticate header field value that may contain multiple
// challenges joined by commas (RFC 7235 §4.3 + RFC 7230 §7 list
// extension). It walks the value byte-by-byte so that commas inside
// quoted-strings are not treated as challenge separators.
//
// RFC 7235's challenge grammar is:
//
//	challenge = auth-scheme [ 1*SP ( token68 /
//	    [ ( "," / auth-param ) *( OWS "," [ OWS auth-param ] ) ] ) ]
//
// i.e. a single challenge may contain its OWN comma-separated parameter
// list. So a comma at top level can either start a new challenge OR
// continue the current one's parameter list. We disambiguate by looking
// at the first whitespace-bounded token of each comma-separated segment:
// if it contains '=' it's a key=value parameter and we ignore it; if it
// looks like a bare token (no '='), it's the next challenge's auth-scheme.
//
// Known limitation: a misbehaving proxy that puts an unquoted comma
// inside a parameter VALUE (RFC violation, e.g. `Basic realm=foo,bar`)
// will cause `bar` to be misclassified as a scheme name. This is benign
// because no real authenticator's scheme() returns `bar`, so the picker
// silently ignores it. We accept this rather than implementing full
// RFC 7235 grammar parsing for the tail of unparseable proxies.
//
// The returned strings are the raw auth-scheme tokens; the caller
// validates them via isToken.
func splitChallengeNames(value string) []string {
	var names []string
	var inQuotes bool
	start := 0
	flush := func(end int) {
		segment := strings.TrimSpace(value[start:end])
		if segment == "" {
			return
		}
		// Take the first whitespace-bounded token of the segment.
		head := segment
		if i := strings.IndexAny(segment, " \t"); i >= 0 {
			head = segment[:i]
		}
		// If the head contains '=', this segment is a key=value
		// parameter continuation of the previous challenge, not a
		// new auth-scheme.
		if strings.ContainsRune(head, '=') {
			return
		}
		names = append(names, head)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
		case c == '\\' && inQuotes && i+1 < len(value):
			i++ // skip next byte (escaped char inside quoted-string)
		case c == ',' && !inQuotes:
			flush(i)
			start = i + 1
		}
	}
	flush(len(value))
	return names
}

type ProxyHandler struct {
	transport *http.Transport
	auth      *authChain
	block     func(string)
}

type proxyFunc func(*http.Request) (*url.URL, error)

func NewProxyHandler(auth *authChain, proxy proxyFunc, block func(string)) ProxyHandler {
	tr := &http.Transport{Proxy: proxy, TLSClientConfig: tlsClientConfig}
	return ProxyHandler{tr, auth, block}
}

func (ph ProxyHandler) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Pass CONNECT requests and absolute-form URIs to the ProxyHandler.
		// If the request URL has a scheme, it is an absolute-form URI
		// (RFC 7230 Section 5.3.2).
		if req.Method == http.MethodConnect || req.URL.Scheme != "" {
			ph.ServeHTTP(w, req)
			return
		}
		// The request URI is an origin-form or asterisk-form target which we
		// handle as an origin server (RFC 7230 5.3). authority-form URIs
		// are only for CONNECT, which has already been dispatched to the
		// ProxyHandler.
		next.ServeHTTP(w, req)
	})
}

func (ph ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	deleteRequestHeaders(req)
	if req.Method == http.MethodConnect {
		ph.handleConnect(w, req)
	} else {
		ph.proxyRequest(w, req, ph.auth)
	}
}

func (ph ProxyHandler) handleConnect(w http.ResponseWriter, req *http.Request) {
	// Establish a connection to the server, or an upstream proxy.
	id := req.Context().Value(contextKeyID)
	proxyURL, err := ph.transport.Proxy(req)
	if err != nil {
		log.Printf("[%d] Error finding proxy for request: %v", id, err)
	}
	var server net.Conn
	if proxyURL == nil {
		server, err = connectDirect(req)
	} else {
		server, err = connectViaProxy(req, proxyURL, ph.auth)
		var oe *net.OpError
		if errors.As(err, &oe) && oe.Op == "proxyconnect" {
			log.Printf("[%d] Temporarily blocking proxy: %q", id, proxyURL.Host)
			ph.block(proxyURL.Host)
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	closeInDefer := true
	defer func() {
		if closeInDefer {
			_ = server.Close()
		}
	}()
	// Take over the connection back to the client by hijacking the ResponseWriter.
	h, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("[%d] Error hijacking response writer", id)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	client, _, err := h.Hijack()
	if err != nil {
		log.Printf("[%d] Error hijacking connection: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		if closeInDefer {
			_ = client.Close()
		}
	}()
	// Write the response directly to the client connection. If we use Go's ResponseWriter, it
	// will automatically insert a Content-Length header, which is not allowed in a 2xx CONNECT
	// response (see https://tools.ietf.org/html/rfc7231#section-4.3.6).
	var resp []byte
	if req.ProtoAtLeast(1, 1) {
		resp = []byte("HTTP/1.1 200 Connection Established\r\n\r\n")
	} else {
		resp = []byte("HTTP/1.0 200 Connection Established\r\n\r\n")
	}
	if _, err := client.Write(resp); err != nil {
		log.Printf("[%d] Error writing response: %v", id, err)
		return
	}
	// Kick off goroutines to copy data in each direction. Whichever goroutine finishes first
	// will close the Reader for the other goroutine, forcing any blocked copy to unblock. This
	// prevents any goroutine from blocking indefinitely (which will leak a file descriptor).
	closeInDefer = false
	go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
	go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
}

func connectDirect(req *http.Request) (net.Conn, error) {
	server, err := net.Dial("tcp", req.Host)
	if err != nil {
		id := req.Context().Value(contextKeyID)
		log.Printf("[%d] Error dialling host %s: %v", id, req.Host, err)
	}
	return server, err
}

func connectViaProxy(req *http.Request, proxyURL *url.URL, auth *authChain) (net.Conn, error) {
	id := req.Context().Value(contextKeyID)

	// SOCKS5 short-circuit: SOCKS5 has its own authentication model
	// (RFC 1928 §3) and never returns 407, so the HTTP-proxy auth
	// chain doesn't apply. Dial directly and hand the conn back to
	// handleConnect for hijacking. Gated upstream by the
	// `--enable-socks` flag (default off).
	if proxyURL.Scheme == "socks5" {
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil, &net.OpError{Op: "proxyconnect", Net: "tcp", Err: err}
		}
		log.Printf("[%d] CONNECT %s via SOCKS5 %s (HTTP auth chain bypassed)",
			id, req.Host, proxyURL.Host)
		conn, err := dialer.Dial("tcp", req.Host)
		if err != nil {
			return nil, &net.OpError{Op: "proxyconnect", Net: "tcp", Err: err}
		}
		return conn, nil
	}

	// Decorate the request with the upstream proxy URL so that
	// scheme-specific authenticators (notably negotiateAuthenticator,
	// which builds the SPN from the proxy hostname) can find it. The
	// proxyfinder middleware sets this for plain-HTTP requests via
	// proxyRequest, but CONNECT bypasses that middleware so we set it
	// here.
	req = req.WithContext(context.WithValue(req.Context(), contextKeyProxy, proxyURL))

	var tr transport
	defer tr.Close() //nolint:errcheck
	if err := tr.dial(proxyURL); err != nil {
		log.Printf("[%d] Error dialling proxy %s: %v", id, proxyURL.Host, err)
		return nil, err
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		log.Printf("[%d] Error reading CONNECT response: %v", id, err)
		return nil, err
	}
	if resp.StatusCode == http.StatusProxyAuthRequired && auth != nil {
		log.Printf("[%d] Got %q response, retrying with auth", id, resp.Status)
		schemes := parseProxyAuthenticateSchemes(resp.Header)
		debugf("[%d] Proxy advertised schemes: %v", id, schemes)
		_ = resp.Body.Close()
		// resp is now stale; the retry helper returns a fresh one.
		authResp, err := retryConnectWithAuth(req, proxyURL, auth, schemes, &tr)
		if err != nil {
			return nil, err
		}
		log.Printf("[%d] Got %q response", id, authResp.Status)
		resp = authResp
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return nil, fmt.Errorf(
			"[%d] all configured authentication methods rejected by proxy", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[%d] Unexpected response status: %s", id, resp.Status)
	}
	return tr.hijack(), nil
}

// retryConnectWithAuth iterates the configured auth chain over a CONNECT
// request, redialling the proxy connection between methods. NTLM and
// Negotiate are connection-bound (RFC 4559), so each method must run on
// its own freshly-dialled socket; sharing one socket across methods
// would mix authentication state machines and could leak credentials
// onto a connection a different scheme had already negotiated against.
//
// Returns the final response (caller closes Body) or an error. If every
// candidate is rejected, the last 407 is returned to the caller.
func retryConnectWithAuth(req *http.Request, proxyURL *url.URL, auth *authChain,
	schemes []string, tr *transport) (*http.Response, error) {
	id := req.Context().Value(contextKeyID)
	candidates := auth.pick(schemes, proxyURL.Hostname())
	if len(candidates) == 0 {
		return nil, errNoMatchingAuthMethod
	}
	var lastResp *http.Response
	for i, method := range candidates {
		// Redial: schemes that are connection-bound (NTLM, Negotiate)
		// require their entire challenge/response sequence to occur
		// over a single, fresh TCP connection. Some proxies also close
		// the socket on a 407.
		if err := tr.dial(proxyURL); err != nil {
			log.Printf("[%d] Error re-dialling %s for %s: %v",
				id, proxyURL.Host, method.scheme(), err)
			return nil, err
		}
		// Defensive: ensure each method starts from a clean header
		// state so that a header set by a prior method (or by the
		// initial request) does not bleed into this attempt.
		req.Header.Del("Proxy-Authorization")
		log.Printf("[%d] Attempting %s authentication", id, method.scheme())
		// NB: any error returned by method.do aborts the chain. This
		// invariant prevents N-5 (header bleed across iterations on
		// the error path) and is intentional; do not change to
		// continue-on-error without revisiting the test that pins
		// this contract.
		resp, err := method.do(req, tr)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusProxyAuthRequired {
			return resp, nil
		}
		if i < len(candidates)-1 {
			_ = resp.Body.Close()
			continue
		}
		// Final candidate also rejected — return the 407 (with body
		// open) to the caller so it can surface diagnostics.
		lastResp = resp
	}
	return lastResp, nil
}

func (ph ProxyHandler) proxyRequest(w http.ResponseWriter, req *http.Request, auth *authChain) {
	// Make a copy of the request body, in case we have to replay it (for authentication)
	var buf bytes.Buffer
	id := req.Context().Value(contextKeyID)
	if n, err := io.Copy(&buf, req.Body); err != nil {
		log.Printf("[%d] Error copying request body (got %d/%d): %v",
			id, n, req.ContentLength, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rd := bytes.NewReader(buf.Bytes())
	req.Body = io.NopCloser(rd)
	resp, err := ph.transport.RoundTrip(req)
	if err != nil {
		log.Printf("[%d] Error forwarding request: %v", id, err)
		w.WriteHeader(http.StatusBadGateway)
		var oe *net.OpError
		if errors.As(err, &oe) && oe.Op == "proxyconnect" {
			proxyURL, err := ph.transport.Proxy(req)
			if err != nil {
				log.Printf("[%d] Proxy connect error to unknown proxy: %v", id, err)
				return
			}
			log.Printf("[%d] Temporarily blocking proxy: %q", id, proxyURL.Host)
			ph.block(proxyURL.Host)
		}
		return
	}
	if resp.StatusCode == http.StatusProxyAuthRequired && auth != nil {
		schemes := parseProxyAuthenticateSchemes(resp.Header)
		debugf("[%d] Proxy advertised schemes: %v", id, schemes)
		_ = resp.Body.Close()
		log.Printf("[%d] Got %q response, retrying with auth", id, resp.Status)
		resp, err = retryProxyRequestWithAuth(req, ph.transport, auth, schemes, rd)
		if err != nil {
			log.Printf("[%d] Error forwarding request (with auth): %v", id, err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		log.Printf("[%d] Got %q response", id, resp.Status)
	}
	defer resp.Body.Close() //nolint:errcheck
	copyResponseHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		// The response status has already been sent, so if copying fails, we can't return
		// an error status to the client.  Instead, log the error.
		log.Printf("[%d] Error copying response body: %v", id, err)
		return
	}
}

// retryProxyRequestWithAuth iterates the configured auth chain over a
// regular (non-CONNECT) HTTP request. Each method gets its OWN cloned
// *http.Transport so that:
//   - NTLM's Type 1 → Type 3 round-trips share a connection (the
//     clone's pool is otherwise idle, so Transport keeps the conn
//     alive between the two RTs); and
//   - the next method starts from a brand-new pool, guaranteeing it
//     cannot accidentally reuse a connection that the previous
//     method's authentication state was bound to.
//
// CloseIdleConnections() on a shared *http.Transport is a hint, not a
// guarantee, so it is insufficient on its own — the per-method clone is
// the load-bearing primitive. See multiauth.go for the picker contract.
func retryProxyRequestWithAuth(req *http.Request, rt *http.Transport, auth *authChain,
	schemes []string, body *bytes.Reader) (*http.Response, error) {
	id := req.Context().Value(contextKeyID)
	proxyHost := ""
	if value := req.Context().Value(contextKeyProxy); value != nil {
		if u, ok := value.(*url.URL); ok {
			proxyHost = u.Hostname()
		}
	}
	candidates := auth.pick(schemes, proxyHost)
	if len(candidates) == 0 {
		return nil, errNoMatchingAuthMethod
	}
	var lastResp *http.Response
	for i, method := range candidates {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			log.Printf("[%d] Error seeking request body for %s retry: %v",
				id, method.scheme(), err)
			return nil, err
		}
		req.Body = io.NopCloser(body)
		req.Header.Del("Proxy-Authorization")
		// Per-method transport clone gives us an isolated connection
		// pool, so connection-bound auth (NTLM/Negotiate) cannot leak
		// state across methods.
		methodRT := rt.Clone()
		log.Printf("[%d] Attempting %s authentication", id, method.scheme())
		// NB: any error from method.do aborts the chain — see same
		// comment in retryConnectWithAuth.
		resp, err := method.do(req, methodRT)
		// Free the cloned pool's idle connections regardless of
		// outcome. This is best-effort; the real isolation comes
		// from each method having its OWN pool.
		methodRT.CloseIdleConnections()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusProxyAuthRequired {
			return resp, nil
		}
		if i < len(candidates)-1 {
			_ = resp.Body.Close()
			continue
		}
		lastResp = resp
	}
	return lastResp, nil
}

func deleteConnectionTokens(header http.Header) {
	// Remove any header field(s) with the same name as a connection token (see
	// https://tools.ietf.org/html/rfc2616#section-14.10)
	if values, ok := header["Connection"]; ok {
		for _, value := range values {
			if value == "close" {
				continue
			}
			tokens := strings.Split(value, ",")
			for _, token := range tokens {
				header.Del(strings.TrimSpace(token))
			}
		}
	}
}

func deleteRequestHeaders(req *http.Request) {
	// Delete hop-by-hop headers (see https://tools.ietf.org/html/rfc2616#section-13.5.1)
	deleteConnectionTokens(req.Header)
	req.Header.Del("Connection")
	req.Header.Del("Keep-Alive")
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("TE")
	req.Header.Del("Upgrade")
}

func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	// Delete hop-by-hop headers (see https://tools.ietf.org/html/rfc2616#section-13.5.1)
	deleteConnectionTokens(w.Header())
	w.Header().Del("Connection")
	w.Header().Del("Keep-Alive")
	w.Header().Del("Proxy-Authenticate")
	w.Header().Del("Trailer")
	w.Header().Del("Transfer-Encoding")
	w.Header().Del("Upgrade")
}
