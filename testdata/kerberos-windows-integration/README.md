# Windows Kerberos/Negotiate: integration test and local validation

This directory documents how the Windows SSPI Negotiate backend
(`kerberos_windows.go`) was validated, and how to reproduce that validation
locally.

Unlike the macOS Kerberos integration test (`testdata/kerberos-darwin-integration/`),
**no runnable harness is shipped here**. The macOS test can stand up its whole
environment from `go test` (a Samba KDC and Squid in a container, plus a local
`kinit`), because nothing about it needs a real macOS logon session. The Windows
path cannot: SSPI reads the *current Windows logon session's* Kerberos
credentials, so a meaningful test requires a **domain-joined Windows host** with
a real ticket-granting ticket. That can't be containerised or run on the
project's CI matrix, so shipping a one-command harness would be misleading.
Instead, the integration test is environment-gated and self-skips, and the
manual setup that exercises it is described below.

All identifiers here are fictitious (`EXAMPLE.TEST`, `proxy.example.test`, and
similar).

## The integration test

`kerberos_windows_integration_test.go` (build tag `integration && windows`)
drives alpaca's auth chain against a real Negotiate-advertising proxy. It reads
its configuration from the environment and calls `t.Skip()` when the
infrastructure, or a real Kerberos credential, is absent, so it never fails on
a developer machine or in CI.

| Variable | Meaning | Example |
|----------|---------|---------|
| `ALPACA_IT_PROXY` | proxy `host:port`; the host must match the proxy's SPN | `proxy.example.test:3128` |
| `ALPACA_IT_UPSTREAM` | URL the proxy fetches once auth succeeds | `http://web.example.test/` |
| `ALPACA_IT_UPSTREAM_BODY` | expected upstream body (default `ok\n`) | `ok\n` |
| `ALPACA_IT_BASIC` | `login:password` for the Basic-fallback assertions | `bob:bobpw` |

Run it (on a domain-joined Windows host, in a session that holds a TGT):

```pwsh
$env:ALPACA_IT_PROXY = "proxy.example.test:3128"
$env:ALPACA_IT_UPSTREAM = "http://web.example.test/"
$env:ALPACA_IT_BASIC = "bob:bobpw"
go test -tags=integration -run TestKerberosWindowsIntegration -v .
```

It asserts five behaviours:

1. **Negotiate succeeds** when a ticket is present (forwarded `200`).
2. **Multi-method chain prefers Negotiate**: Basic is never invoked when
   Negotiate wins.
3. **Falls through to Basic** when the Negotiate ticket is gone.
4. **Refuses to downgrade** when only Negotiate is configured but ineligible
   (returns `errNoMatchingAuthMethod`, never silently sends Basic).
5. **Chain-level allowlist** excludes the proxy host before any method runs.

## Reproducing the validation environment

The environment is two pieces: a Linux **domain controller + proxy**, and a
**Windows client** joined to it.

### Domain controller + proxy (Linux, container)

A single container provides a Samba Active Directory domain controller (KDC,
LDAP, DNS), a Negotiate-advertising Squid backed by an AD-exported keytab, and a
tiny upstream web server that returns `ok`. Sketch:

- `samba-tool domain provision --server-role=dc --realm=EXAMPLE.TEST ...`
- a user `alice` (the Kerberos principal) and `bob` (Basic), plus a `proxysvc`
  service account carrying the SPN `HTTP/proxy.example.test`.
- Squid with `auth_param negotiate negotiate_kerberos_auth -s
  HTTP/proxy.example.test@EXAMPLE.TEST` and `auth_param basic`.

Requirements found necessary for a modern client, each the hard way:

- **Samba >= 4.22.** Windows 11 24H2 (build 26100) cannot complete a domain
  logon against Samba 4.17 (Debian *bookworm*); the logon fails *after*
  authentication with `STATUS_INVALID_BUFFER_SIZE` due to a Kerberos PAC
  incompatibility. Debian *trixie* ships Samba 4.22, which works. Note that on
  Debian 13 the AD DC daemon moved to a separate `samba-ad-dc` package.
- **AES keys on the proxy SPN.** Set `msDS-SupportedEncryptionTypes = 24`
  (AES128+AES256) on `proxysvc` and reset its password *before* exporting the
  keytab. A default export can be RC4-only, and modern Windows disables RC4, so
  client and proxy would share no enctype.
- **`visible_hostname` in `squid.conf`.** Without it, Squid tries to determine
  its hostname by reverse-resolving its own IP at startup; the test realm has no
  PTR zone, so the probe fails and Squid never binds its port.

### Windows client

Point the client's DNS at the domain controller, join the realm
(`Add-Computer -DomainName EXAMPLE.TEST`), and log on **interactively** as the
domain user so the LSA seats a real TGT (a network-only logon does not seat
one). Confirm with `klist` (expect a `krbtgt/EXAMPLE.TEST` entry, AES-256).

If the KDC's Kerberos reply exceeds a UDP datagram (a large PAC), the client
must use TCP/88. On a normal network this fallback is automatic; in a
constrained lab you may need to force Kerberos over TCP so the client retries on
TCP rather than silently failing on the oversized UDP reply.

## Results

Against this environment (Windows 11 24H2 ARM64 client, Samba 4.22 DC):

- `TestKerberosWindowsIntegration` passes **5/5 subtests**, as domain user
  `alice@EXAMPLE.TEST` holding a real AES-256 TGT.
- The **real `alpaca.exe`** (not just the test's auth-chain harness), run with
  `-C <pac-url>` selecting the proxy and **no** NTLM/Basic credentials
  configured, returned **`200 OK`** for a request routed through it, a result
  that can only come from a successful Negotiate/Kerberos exchange. Its log
  showed the full path: ticket detected, PAC fetched, `407`, Negotiate, `200`.
