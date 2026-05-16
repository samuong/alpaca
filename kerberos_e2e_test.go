// Copyright 2025 The Alpaca Authors
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

//go:build e2e && darwin

// End-to-end test fixture for alpaca's multi-method proxy authentication.
//
// Spins up a single container (testdata/kerberos-e2e/Dockerfile) running
// MIT KDC + squid configured to advertise Negotiate, NTLM and Basic, then
// exercises the full alpaca pipeline against it: PAC-less direct upstream,
// the multi-auth chain, and the security invariants (downgrade refusal,
// SPN allowlist enforcement, ticket re-check).
//
// Build tag is "e2e && darwin": the test exercises alpaca's macOS
// GSS.framework Negotiate path, which is the only Kerberos backend
// implemented in this PR. On other platforms newNegotiateAuthenticator
// returns nil so there's nothing to exercise; the build constraint
// keeps `go test -tags=e2e ./...` working transparently elsewhere.
//
// Run with:
//
//   CGO_ENABLED=1 go test -tags=e2e -run TestKerberosE2E -v .
//
// Prerequisites on the host:
//   - docker on PATH (Podman should also work via the docker shim)
//   - kinit on PATH (Heimdal ships with macOS; krb5-user on Linux)
//
// The test calls t.Skip() when prerequisites are missing so a developer
// without docker doesn't see an unexplained failure.

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	imageTag      = "alpaca-kerberos-e2e:dev"
	containerName = "alpaca-kerberos-e2e"
	// All identifiers below are deliberately fictitious. EXAMPLE.TEST
	// and *.example.test are reserved for testing per RFC 6761; the
	// principals and passwords are baked into the test fixture and
	// are NOT real credentials.
	realm         = "EXAMPLE.TEST"
	proxyHost     = "proxy.example.test"
	kdcHost       = "kdc.example.test"
	userPrinc     = "alice@" + realm
	userPassword  = "alicepw"
	basicUser     = "bob"
	basicPassword = "bobpw"
	// upstreamBody is what the in-container Python http.server returns
	// for /. Asserted by every successful e2e sub-test so that a squid
	// misconfiguration returning its own 200 page would not silently
	// pass.
	upstreamBody = "ok\n"
)

// e2eFixture wraps a running test container and exposes the host-side
// ports that the test needs to dial. It also remembers the temporary
// krb5.conf and credential cache so they're cleaned up on teardown.
type e2eFixture struct {
	t             *testing.T
	dockerBin     string
	proxyHostPort string // e.g. 127.0.0.1:53128
	kdcHostPort   string // e.g. 127.0.0.1:50088
	upstreamURL   string // URL squid will fetch and that returns 200 OK
	tempDir       string
	krb5ConfPath  string
	credCachePath string
}

func TestKerberosE2E(t *testing.T) {
	fx := setupFixture(t)
	defer fx.teardown()

	// All sub-tests share the fixture (one container, one Kerberos
	// ticket) so they run quickly. Each sub-test asserts a discrete
	// invariant.
	t.Run("Negotiate succeeds when ticket is present", fx.testNegotiateSucceeds)
	t.Run("Basic succeeds when explicitly advertised", fx.testBasicSucceeds)
	t.Run("Multi-method chain prefers Negotiate", fx.testMultiMethodPrefersNegotiate)
	t.Run("Falls through to Basic when Negotiate ticket is gone", fx.testFallsThroughOnTicketLoss)
	t.Run("Refuses Basic when only NTLM/Negotiate configured against Basic-only proxy", fx.testRefusesBasicDowngrade)
	t.Run("SPN allowlist excludes proxy", fx.testSPNAllowlistExclusion)
	// Note: there is no e2e sub-test for NTLM because squid's only
	// container-friendly NTLM helper (ntlm_fake_auth) emits Type-2
	// challenges that go-ntlmssp's strict parser rejects, and a real
	// NTLM helper requires a Windows DC. NTLM iteration through the
	// multi-method picker is covered at the unit level in
	// multiauth_integration_test.go::TestRetryProxyRequest_FallsThroughOn407
	// and the cryptographic correctness lives in samuong/go-ntlmssp.
}

// ---------------------------------------------------------------------
// Fixture lifecycle
// ---------------------------------------------------------------------

func setupFixture(t *testing.T) *e2eFixture {
	t.Helper()

	docker := findDocker(t)
	requireBinary(t, "kinit")

	fx := &e2eFixture{
		t:         t,
		dockerBin: docker,
		// The container bootstrap script starts a Python http.server
		// on 127.0.0.1:8080 inside the container. squid forwards to
		// it once authentication succeeds, so a 200 from this URL
		// proves the auth chain reached the "request forwarded"
		// stage. Keeping the upstream inside the container is the
		// simplest way to get cross-platform parity: reaching a
		// host-side service from the container would require
		// host.docker.internal (Docker Desktop only) or
		// --add-host=host.docker.internal:host-gateway (Linux Docker
		// 20.10+) plus careful firewall handling for inbound
		// container-originated traffic on macOS.
		upstreamURL: "http://127.0.0.1:8080/",
	}
	fx.buildImage(t)
	fx.runContainer(t)
	fx.waitForSquid(t)
	fx.kinit(t)
	return fx
}

func findDocker(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"docker", "podman"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("e2e: neither docker nor podman found on PATH")
	return ""
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("e2e: %s not found on PATH (skipping; install krb5-user / Heimdal)", name)
	}
}

func (fx *e2eFixture) buildImage(t *testing.T) {
	t.Helper()
	dir, err := filepath.Abs("testdata/kerberos-e2e")
	require.NoError(t, err)

	args := []string{"build", "-t", imageTag}
	// Plumb host's HTTP_PROXY/HTTPS_PROXY through as build args so the
	// image can be built behind a corporate proxy. host.docker.internal
	// is mapped by Docker Desktop; no-op on Linux Docker (where the
	// proxy must already be reachable from container build context).
	if proxy := os.Getenv("HTTP_PROXY"); proxy != "" {
		// host.docker.internal lets the container reach the host's
		// proxy if alpaca itself is running there. Substitute
		// localhost references accordingly.
		proxy = strings.ReplaceAll(proxy, "localhost", "host.docker.internal")
		proxy = strings.ReplaceAll(proxy, "127.0.0.1", "host.docker.internal")
		args = append(args, "--build-arg", "HTTP_PROXY="+proxy)
		args = append(args, "--build-arg", "HTTPS_PROXY="+proxy)
	}
	args = append(args, dir)

	t.Logf("e2e: building image %s (this may take a few minutes the first time)", imageTag)
	cmd := exec.Command(fx.dockerBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("e2e: docker build failed (skipping; ensure docker daemon is reachable):\n%s", output)
	}
}

func (fx *e2eFixture) runContainer(t *testing.T) {
	t.Helper()
	// Tear down any leftover container from a previous run.
	_ = exec.Command(fx.dockerBin, "rm", "-f", containerName).Run()

	// Allocate two host ports (squid, KDC). Use 0 to let the kernel
	// pick free ports; we'll read them back from `docker port`.
	args := []string{
		"run", "-d", "--rm",
		"--name", containerName,
		// Container hardening: defence-in-depth for a test fixture
		// that runs locally on developer machines. We keep the
		// capabilities the bootstrap script actually needs (CHOWN +
		// FOWNER + SETUID/SETGID for the keytab + squid privilege
		// drop, plus NET_BIND_SERVICE so krb5kdc can bind :88,
		// DAC_OVERRIDE for some squid runtime file ops) and drop
		// the rest. squid drops to user proxy:proxy at runtime so
		// retained root capabilities only affect bootstrap.
		"--cap-drop=ALL",
		"--cap-add=CHOWN",
		"--cap-add=FOWNER",
		"--cap-add=SETUID",
		"--cap-add=SETGID",
		"--cap-add=NET_BIND_SERVICE",
		"--cap-add=DAC_OVERRIDE",
		"--security-opt=no-new-privileges",
		"-p", "127.0.0.1::3128",
		"-p", "127.0.0.1::88/tcp",
		"-p", "127.0.0.1::88/udp",
		imageTag,
	}
	output, err := exec.Command(fx.dockerBin, args...).CombinedOutput()
	require.NoErrorf(t, err, "docker run failed:\n%s", output)

	// Read back the dynamic ports.
	fx.proxyHostPort = fx.dockerPort("3128/tcp")
	fx.kdcHostPort = fx.dockerPort("88/tcp")
	t.Logf("e2e: container started; squid=%s, kdc=%s",
		fx.proxyHostPort, fx.kdcHostPort)
}

func (fx *e2eFixture) dockerPort(internal string) string {
	out, err := exec.Command(fx.dockerBin, "port", containerName, internal).Output()
	require.NoErrorf(fx.t, err, "docker port %s failed", internal)
	// Output looks like "127.0.0.1:53128"
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "127.0.0.1:") {
			return line
		}
	}
	fx.t.Fatalf("could not parse docker port output: %q", out)
	return ""
}

func (fx *e2eFixture) waitForSquid(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodHead, "http://example.com", nil)
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(&url.URL{
					Scheme: "http",
					Host:   fx.proxyHostPort,
				}),
			},
			Timeout: 2 * time.Second,
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusProxyAuthRequired {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("e2e: squid did not respond with 407 within 60s; container logs follow:\n" + fx.containerLogs())
}

func (fx *e2eFixture) containerLogs() string {
	out, _ := exec.Command(fx.dockerBin, "logs", containerName).CombinedOutput()
	return string(out)
}

func (fx *e2eFixture) kinit(t *testing.T) {
	t.Helper()

	// Build a krb5.conf that tells kinit to reach the KDC on the
	// dynamic host port we got from docker. The realm and host names
	// match what's inside the container; the KDC entry is the
	// host-side address.
	fx.tempDir = t.TempDir()
	fx.krb5ConfPath = filepath.Join(fx.tempDir, "krb5.conf")
	fx.credCachePath = filepath.Join(fx.tempDir, "krb5cc")

	conf := fmt.Sprintf(`[libdefaults]
    default_realm = %s
    dns_lookup_kdc = false
    dns_lookup_realm = false
    rdns = false
    forwardable = true
    udp_preference_limit = 1
    default_tkt_enctypes = aes256-cts-hmac-sha1-96 aes128-cts-hmac-sha1-96
    default_tgs_enctypes = aes256-cts-hmac-sha1-96 aes128-cts-hmac-sha1-96

[realms]
    %s = {
        kdc = tcp/%s
        admin_server = %s
    }

[domain_realm]
    .example.test = %s
    example.test = %s
`, realm, realm, fx.kdcHostPort, fx.kdcHostPort, realm, realm)
	require.NoError(t, os.WriteFile(fx.krb5ConfPath, []byte(conf), 0o600))

	// Use t.Setenv so Go's testing framework restores the prior values
	// even on panic / Fatal between here and teardown. This matters
	// because the test may be invoked on a developer machine that's
	// signed in to a real corporate Kerberos realm; without the
	// guaranteed restore, a crashed test could leave the developer's
	// shell pointing at a tempdir that's been deleted, breaking
	// real-world Kerberos until they restart their session.
	t.Setenv("KRB5_CONFIG", fx.krb5ConfPath)
	t.Setenv("KRB5CCNAME", "FILE:"+fx.credCachePath)

	// kinit -V reads from stdin when --password-file isn't supported.
	// macOS Heimdal supports both --password-file and stdin via tty,
	// so we use stdin which is portable across MIT and Heimdal.
	cmd := exec.Command("kinit", userPrinc)
	cmd.Stdin = strings.NewReader(userPassword + "\n")
	cmd.Env = append(os.Environ(),
		"KRB5_CONFIG="+fx.krb5ConfPath,
		"KRB5CCNAME=FILE:"+fx.credCachePath,
	)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "kinit %s failed:\n%s\nKRB5_CONFIG=%s",
		userPrinc, output, fx.krb5ConfPath)

	// Verify the ticket landed.
	cmd = exec.Command("klist")
	cmd.Env = append(os.Environ(),
		"KRB5_CONFIG="+fx.krb5ConfPath,
		"KRB5CCNAME=FILE:"+fx.credCachePath,
	)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "klist failed after kinit:\n%s", output)
	require.Contains(t, string(output), realm,
		"klist did not show a ticket for %s", realm)
	t.Logf("e2e: kinit ok\n%s", strings.TrimSpace(string(output)))
}

func (fx *e2eFixture) teardown() {
	if fx.dockerBin != "" {
		_ = exec.Command(fx.dockerBin, "rm", "-f", containerName).Run()
	}
	// KRB5_CONFIG / KRB5CCNAME are restored automatically by t.Setenv.
}

// ---------------------------------------------------------------------
// Test helpers — drive alpaca against the fixture
// ---------------------------------------------------------------------

// proxyURL returns the URL alpaca should treat as the upstream proxy.
// The hostname must resolve to the SPN that squid's keytab signed
// (HTTP/proxy.example.test), so we use proxy.example.test in the URL
// and rely on a custom DialContext to actually connect to the host
// port that docker exposed.
func (fx *e2eFixture) proxyURL() *url.URL {
	host, port, _ := net.SplitHostPort(fx.proxyHostPort)
	_ = host
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(proxyHost, port)}
}

// dialer returns a net.Dialer-style function that rewrites
// proxy.example.test:N to 127.0.0.1:N so the SPN-bearing hostname
// reaches the actual container port.
func (fx *e2eFixture) dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	hostPort := fx.proxyHostPort
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, proxyHost+":") {
			addr = hostPort
		}
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
}

// transportThroughAlpaca builds an *http.Transport that sends requests
// through the given alpaca chain by invoking the chain helpers
// directly. It mirrors what ProxyHandler does without needing to spin
// up the full middleware stack.
func (fx *e2eFixture) transportThroughAlpaca(chain *authChain) http.RoundTripper {
	return &alpacaTestRT{
		fx:    fx,
		chain: chain,
	}
}

type alpacaTestRT struct {
	fx    *e2eFixture
	chain *authChain
}

func (a *alpacaTestRT) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyURL := a.fx.proxyURL()
	tr := &http.Transport{
		Proxy:       http.ProxyURL(proxyURL),
		DialContext: a.fx.dialer(),
	}
	defer tr.CloseIdleConnections()
	// Decorate the request with the proxy URL so applicableTo /
	// negotiateAuthenticator can find it.
	req = req.WithContext(context.WithValue(req.Context(),
		contextKeyProxy, proxyURL))

	// Buffer the body so we can replay across auth retries.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	rd := bytes.NewReader(bodyBytes)
	req.Body = io.NopCloser(rd)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		return resp, nil
	}
	if a.chain == nil {
		return resp, nil
	}
	schemes := parseProxyAuthenticateSchemes(resp.Header)
	_ = resp.Body.Close()
	return retryProxyRequestWithAuth(req, tr, a.chain, schemes, rd)
}

// ---------------------------------------------------------------------
// Sub-tests
// ---------------------------------------------------------------------

// instrumentedBasic wraps a basicAuthenticator with a call counter so
// tests can assert that the basic-auth path was (or was not) invoked.
type instrumentedBasic struct {
	*basicAuthenticator
	calls atomic.Int32
}

func (b *instrumentedBasic) do(req *http.Request, rt http.RoundTripper) (*http.Response, error) {
	b.calls.Add(1)
	return b.basicAuthenticator.do(req, rt)
}

func newInstrumentedBasic(creds string) *instrumentedBasic {
	return &instrumentedBasic{basicAuthenticator: newBasicAuthenticator(creds)}
}

func (fx *e2eFixture) testNegotiateSucceeds(t *testing.T) {
	neg := newNegotiateAuthenticator(0)
	require.NotNil(t, neg, "expected newNegotiateAuthenticator to find the kinit'd ticket")
	chain := newAuthChain(neg)
	require.NotNil(t, chain)

	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	require.NoError(t, err)
	assertSuccessful200(t, resp)
}

func (fx *e2eFixture) testBasicSucceeds(t *testing.T) {
	basic := newBasicAuthenticator(basicUser + ":" + basicPassword)
	chain := newAuthChain(basic)
	require.NotNil(t, chain)

	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	require.NoError(t, err)
	assertSuccessful200(t, resp)
}

func (fx *e2eFixture) testMultiMethodPrefersNegotiate(t *testing.T) {
	// All methods configured. Negotiate should be tried first and
	// should succeed; the instrumented Basic must NOT be invoked.
	// This is the explicit "no fallthrough to Basic" assertion the
	// previous version of this test only proved by elimination.
	neg := newNegotiateAuthenticator(0)
	require.NotNil(t, neg)
	basic := newInstrumentedBasic(basicUser + ":" + basicPassword)
	chain := newAuthChain(neg, basic)

	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	require.NoError(t, err)
	assertSuccessful200(t, resp)
	assert.EqualValues(t, 0, basic.calls.Load(),
		"Basic must not be invoked when Negotiate succeeded first")
}

func (fx *e2eFixture) testFallsThroughOnTicketLoss(t *testing.T) {
	// Build a chain whose Negotiate "loses" its ticket between picker
	// time and request time by overriding hasTicket to return false.
	// applicableTo will then exclude Negotiate, picker falls through
	// to Basic.
	neg := newNegotiateAuthenticator(0)
	require.NotNil(t, neg)
	negotiator, ok := neg.(*negotiateAuthenticator)
	require.True(t, ok)
	negotiator.hasTicket = func() bool { return false }

	basic := newBasicAuthenticator(basicUser + ":" + basicPassword)
	chain := newAuthChain(negotiator, basic)

	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	require.NoError(t, err)
	assertSuccessful200(t, resp)
}

func (fx *e2eFixture) testRefusesBasicDowngrade(t *testing.T) {
	// Configure ONLY Negotiate, then deliberately ineligible-ate it
	// via hasTicket=false. The picker must yield zero candidates and
	// the loop returns errNoMatchingAuthMethod, NOT silently send
	// Basic credentials.
	neg := newNegotiateAuthenticator(0)
	require.NotNil(t, neg)
	negotiator, ok := neg.(*negotiateAuthenticator)
	require.True(t, ok)
	negotiator.hasTicket = func() bool { return false }
	chain := newAuthChain(negotiator)

	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoMatchingAuthMethod)
}

func (fx *e2eFixture) testSPNAllowlistExclusion(t *testing.T) {
	// Allowlist that does NOT match proxyHost. applicableTo must
	// return false at picker time.
	neg := newNegotiateAuthenticator(0)
	require.NotNil(t, neg)
	negotiator, ok := neg.(*negotiateAuthenticator)
	require.True(t, ok)
	negotiator.allowedSuffixes = parseSPNAllowlist(".unrelated.test")

	// Without a fallback method, the chain must error.
	chain := newAuthChain(negotiator)
	resp, err := fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err,
		"expected SPN-allowlist exclusion to surface as errNoMatchingAuthMethod")

	// With Basic also configured, the chain must fall through.
	basic := newBasicAuthenticator(basicUser + ":" + basicPassword)
	chain = newAuthChain(negotiator, basic)
	resp, err = fx.transportThroughAlpaca(chain).RoundTrip(mustReq(t, fx))
	require.NoError(t, err)
	assertSuccessful200(t, resp)
}

// assertSuccessful200 verifies that resp is a 200 from the in-container
// upstream test server (body equals upstreamBody) — not, say, squid's own
// 200-shaped error page. Closes the body when done.
func assertSuccessful200(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, upstreamBody, string(body),
		"expected upstream test server response, not a squid synthesised page")
}

// mustReq builds a fresh request to the upstream test server. Squid
// will forward this once authentication succeeds; a 200 with body
// `upstreamBody` from the in-container HTTP server is what proves the
// auth chain worked end-to-end.
func mustReq(t *testing.T, fx *e2eFixture) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, fx.upstreamURL, nil)
	require.NoError(t, err)
	return req
}
