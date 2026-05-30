# Kerberos end-to-end test fixture

This directory contains the Docker fixture that backs `kerberos_integration_test.go`.
It is intentionally separate from the standard test suite because it requires
Docker (or Podman with a running machine) and takes ~30s on first run.

**Platform scope:** the test is gated on `//go:build e2e && darwin` because it
exercises alpaca's macOS GSS.framework Negotiate path. The Linux/Windows
Kerberos backends are not implemented in the same PR and would need their
own host-side test rigs (a domain-joined Windows VM for SSPI, or `gokrb5`
hooked into a krb5cc fixture for Linux); the Docker container itself is
just a stable KDC + squid for the host's Kerberos client to talk to.

## What it does

The Dockerfile builds a single image that runs:

- **MIT Kerberos KDC** (`krb5-kdc` + `krb5-admin-server`) for the realm
  `EXAMPLE.TEST`, with three principals:
  - `alice@EXAMPLE.TEST` — the test "user" the host's `kinit` obtains a
    TGT for.
  - `HTTP/proxy.example.test@EXAMPLE.TEST` — squid's service principal,
    kept in `/etc/squid/HTTP.keytab`.
  - `admin/admin@EXAMPLE.TEST` — kadmin master, used by the bootstrap
    script.
- **Squid** configured to advertise `Negotiate, NTLM, Basic`, with:
  - `negotiate_kerberos_auth` helper backed by the keytab above.
  - `ntlm_fake_auth` helper (NTLM cannot be tested against a real DC
    inside a container; the helper validates message shape only).
  - `basic_ncsa_auth` helper backed by `/etc/squid/passwd`.
- **A trivial Python `http.server`** on `127.0.0.1:8080` inside the
  container so squid has somewhere to forward authenticated requests.
  Keeping the upstream in-container avoids any host-network egress
  dependency, which is important on developer laptops behind corporate
  egress proxies.

## Running the test

```sh
CGO_ENABLED=1 go test -tags=e2e -run TestKerberosE2E -v .
```

The test:

1. Builds the Docker image (cached after the first run).
2. Starts a container with `:3128` (squid) and `:88` (KDC) published
   to dynamic localhost ports.
3. Writes a temporary `krb5.conf` pointing at the published KDC port
   and runs `kinit alice@EXAMPLE.TEST` on the host using the
   hard-coded password.
4. Drives alpaca's auth chain through the container, asserting:
   - Negotiate succeeds when a real Kerberos ticket is present.
   - Basic succeeds when explicitly advertised.
   - The chain prefers Negotiate over Basic.
   - When Negotiate's ticket is "lost" (`hasTicket: false`), the chain
     falls through to Basic.
   - When only Negotiate is configured but ineligible, the chain
     surfaces `errNoMatchingAuthMethod`.
   - The chain-level proxy-auth allowlist blocks credential exposure when the proxy host doesn't match.
5. Tears the container down on exit.

## Skipping conditions

The test calls `t.Skip()` rather than `t.Fatal()` when:

- Neither `docker` nor `podman` is on `PATH`.
- `kinit` (Heimdal/MIT) is not on `PATH`.
- `docker build` fails (e.g. daemon not running).

This keeps the e2e test invisible to anyone who doesn't have the
infrastructure for it, while letting a developer who does run it as
part of their normal pre-PR validation.

## Building behind a corporate proxy

The Dockerfile honours `--build-arg HTTP_PROXY=...` and
`--build-arg HTTPS_PROXY=...`. The test driver auto-detects these from
the host environment and rewrites `localhost`/`127.0.0.1` to
`host.docker.internal` so the apt fetches inside the build container can
egress via the host's proxy. If your laptop runs alpaca itself as the
egress proxy at `localhost:3128`, the test will use it automatically.

## Test credentials

These are baked into the image and are NOT secrets — every runner gets
the same passwords, and the image only ever runs inside the test
fixture's network namespace.

| Principal | Password |
|-----------|----------|
| `alice@EXAMPLE.TEST` | `alicepw` |
| `bob` (Basic auth) | `bobpw` |
| `admin/admin@EXAMPLE.TEST` | `adminpw` |
