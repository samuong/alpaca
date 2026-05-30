# Alpaca

![Alpaca Logo](assets/alpaca-small.png)

![Latest Tag][2] ![GitHub Workflow Status][3] ![GitHub Releases][4]

Alpaca is a local HTTP proxy for command-line tools. It supports proxy
auto-configuration (PAC) files, NTLM authentication, HTTP Basic
authentication, and (on macOS) Kerberos/Negotiate (SPNEGO) authentication.
![alt text](assets/alpaca-banner.png)

## Install using Homebrew

If you're using macOS and use [Homebrew](https://brew.sh/), you can install
using:

```sh
$ brew tap samuong/alpaca
$ brew install samuong/alpaca/alpaca
```

Launch Alpaca by running `alpaca`, or by using `brew services start alpaca`.

## Install using Go

If you've got the [Go](https://golang.org/cmd/go/) tool installed, you can
install using:

```sh
$ go install github.com/samuong/alpaca/v2@latest
```

## Build from source

If you'd like to build Alpaca from source, you'll need [Go](https://go.dev/)
1.25.0 or later. CGO must be enabled:

```sh
$ CGO_ENABLED=1 go build -v .
```

To run the tests:

```sh
$ CGO_ENABLED=1 go test ./...
```

## Download Binary

Alpaca can be downloaded from the [GitHub releases page][1].

## Install from distribution packages

[![Packaging status](https://repology.org/badge/vertical-allrepos/alpaca-proxy.svg)](https://repology.org/project/alpaca-proxy/versions)

## Usage

Start Alpaca by running the `alpaca` binary.

If the proxy server requires valid authentication credentials, you can provide them by means of:

- HTTP Basic authentication, if `BASIC_CREDENTIALS=login:password` is set in
  the environment;
- Kerberos / Negotiate, **automatically on macOS** when a ticket from Apple SSO
  / Ticket Viewer / `kinit` is available — no flag required (pass
  `--no-kerberos` to opt out). Tickets that arrive *after* alpaca starts are
  picked up automatically: alpaca re-checks credential availability on every
  407 response, so a user who launches alpaca before signing in to Apple SSO
  does not need to restart it once the ticket lands;
- NTLM via the shell prompt, if `-d` is passed;
- NTLM via the shell environment, if `NTLM_CREDENTIALS` is set;
- the system keyring (macOS, Windows and Linux/GNOME supported), if none of
  the above applies.

Multiple authentication methods can be enabled simultaneously. When the proxy
returns `407 Proxy Authentication Required`, Alpaca parses the proxy's
`Proxy-Authenticate` response header(s) and tries each configured method whose
scheme appears in the advertisement, in Chrome's preference order
(Negotiate → NTLM → Basic). If the proxy returns 407 with no parseable
`Proxy-Authenticate` header, Alpaca will only try schemes that begin with a
non-credential probe (NTLM Type 1, SPNEGO initial token); Basic credentials
are NEVER sent without an explicit advertisement, so a hostile endpoint
returning a bare 407 cannot harvest your password.

Otherwise, the authentication with proxy will be simply ignored.

### Restricting where Alpaca sends credentials

**Default behaviour: permissive.** Alpaca will offer whatever credentials
it has — Basic, NTLM, or Kerberos/Negotiate — to whichever proxy host your
PAC file nominates. The PAC is treated as the trust root for proxy
routing: if you've configured Alpaca to fetch a particular PAC, you've
already decided to route your traffic through whatever proxies that PAC
selects, so authenticating against those proxies is the natural
extension. At startup Alpaca logs:

```
Proxy auth allowlist: permissive (any host nominated by your PAC will receive credentials). Set ALPACA_PROXY_AUTH_ALLOWLIST to restrict.
```

**When to restrict.** The PAC-trust-root assumption can break in two
ways:

- **PAC over plain HTTP.** Many PAC URLs are HTTP (no TLS), so a
  network-position attacker can substitute their own response and direct
  Alpaca through an attacker-controlled proxy. With the permissive
  default, that proxy will receive your credentials in whatever form it
  asks for — Basic plaintext password, NTLMv2 hash, or a Kerberos service
  ticket.
- **WPAD discovery.** On networks that auto-discover PAC via DNS or
  DHCP, an attacker on the same broadcast segment can win the race and
  serve their own PAC.

To defend against these, restrict the set of proxy hosts allowed to
receive credentials via the `ALPACA_PROXY_AUTH_ALLOWLIST` environment
variable:

```sh
$ export ALPACA_PROXY_AUTH_ALLOWLIST=.corp.example.com,.proxy-vendor.example.net
$ alpaca
```

The value is a comma-separated list of DNS suffixes.

**Syntax:**

- A leading-dot entry (`.corp.example.com`) matches `corp.example.com`
  itself and any subdomain of it. A bare entry (`corp.example.com`) is
  normalised to the same form. Trailing dots are ignored so FQDN
  literals (`corp.example.com.`) work too.
- Matching is case-insensitive and uses DNS-label boundaries so that
  `.corp.example` matches `proxy.corp.example` but NOT `evilcorp.example`.
- The literal value `*` means "any host" — the same shape as the default.
  Useful to silence the startup discoverability log line if you're
  intentionally running permissive in a controlled deployment.
- The allowlist applies uniformly to **all** authentication methods. A
  host outside the allowlist will not receive Basic, NTLM, or Negotiate
  credentials, regardless of what the proxy advertises.

**Picking the right suffixes.** Internal proxies often live under a
different DNS namespace from your Kerberos realm — vendor-managed egress,
dedicated ops/IT subdomains, post-acquisition estates. To enumerate them,
grep your PAC file(s) for `PROXY` directives — every unique hostname
there is a candidate for the allowlist. The allowlist does not need to
match your Kerberos principal's realm; Alpaca asks the KDC for
`HTTP/proxy.example.net@CORP.EXAMPLE.COM`, and the KDC's SPN registry
(and any cross-realm trusts) decide whether to issue a service ticket
regardless of suffix.

SaaS proxies (Prisma Access, Zscaler, Netskope, etc.) authenticate via
their own client agents — GlobalProtect, Zscaler Client Connector,
Netskope Client — not 407 SPNEGO/NTLM/Basic, so they don't need to be in
the allowlist. When your PAC routes traffic through one of them, the
client agent handles the auth handshake separately and Alpaca never sees
a 407.

When a host is excluded, the log line is:

```
Proxy "proxy.example.net" not in proxy-auth allowlist (allowed: [.corp.example.com]); set ALPACA_PROXY_AUTH_ALLOWLIST to include this host, or unset to permit any host
```

### Troubleshooting

When auth misbehaves, the first thing to check is alpaca's own log:

- `Proxy "…" not in proxy-auth allowlist …` — your allowlist excludes
  the proxy host the PAC selected. Either add the host's DNS suffix to
  `ALPACA_PROXY_AUTH_ALLOWLIST`, or unset it to permit any host. See
  "Restricting where Alpaca sends
  credentials" above.
- `Kerberos ticket no longer valid; skipping Negotiate for …` —
  Negotiate detected an expired or revoked TGT. Run `klist` to confirm,
  then refresh via `kinit` (or wait for Apple SSO). The chain falls
  through to NTLM / Basic for the duration; once a fresh ticket appears
  Negotiate resumes on the next 407.
- `Auth method Negotiate declines for proxy host "…"` — Negotiate's
  runtime preconditions weren't met (today this means "no Kerberos
  ticket"). The preceding `Kerberos ticket no longer valid …` line
  explains why; if that line isn't there, alpaca never had a ticket to
  begin with — check `klist`.
- `No authenticator matched proxy "…"; returning 502 to client` —
  every configured method either declined via `applicableTo`, was
  excluded by the host allowlist, or didn't match the proxy's
  advertised schemes. The client sees a 502; this line tells you which
  proxy and that the chain ran out of options.

### Platform support for Kerberos

Kerberos / Negotiate authentication in this build is **macOS only**. It uses
Apple's `GSS.framework` to consume the system Kerberos credential cache —
the same one populated by Apple SSO, Ticket Viewer, and `kinit` — so no
extra configuration is required when a ticket is already present.

Windows and Linux Kerberos handling is intentionally out of scope for this
change; on those platforms `newNegotiateAuthenticator` returns `nil` and
Negotiate is transparently absent from the auth chain. Adding support on
either platform is a follow-up:

- **Windows** has system-wide Kerberos via SSPI (`Negotiate` package) and
  could be implemented either via cgo against `security.h` or in pure Go
  via `github.com/alexbrainman/sspi`.
- **Linux** has no system-wide credential store but `github.com/jcmturner/gokrb5`
  can read the per-user `krb5cc_$UID` cache produced by `kinit`.

Both are clean drop-in additions next to `kerberos_darwin.go`, sharing
the same `proxyAuthenticator` interface.

### Shell Prompt

You can also supply your domain and username (via command-line flags) and a
password (via a prompt):

```sh
$ alpaca -d MYDOMAIN -u me
Password (for MYDOMAIN\me):
```

### Non-interactive launch

If you want to use Alpaca without any interactive password prompt, you can store
your NTLM credentials (domain, username and MD4-hashed password) in an
environment variable called `$NTLM_CREDENTIALS`. You can use the `-H` flag to
generate this value:

```sh
$ ./alpaca -d MYDOMAIN -u me -H
# Add this to your ~/.profile (or equivalent) and restart your shell
NTLM_CREDENTIALS="me@MYDOMAIN:823893adfad2cda6e1a414f3ebdf58f7"; export NTLM_CREDENTIALS
```

Note that this hash is *not* cryptographically secure; it's just meant to stop
people from being able to read your password with a quick glance.

Once you've set this environment variable, you can start Alpaca by running
`./alpaca`.

### Keyring

On macOS, if you use [NoMAD](https://nomad.menu/products/#nomad) and have configured it
to [use the keychain](https://nomad.menu/help/keychain-usage/), Alpaca will use
these credentials to authenticate to any NTLM challenge from your proxies.

On Windows and Linux/GNOME you will need some extra work to persist the username (`NTLM_USERNAME`) and the domain (`NTLM_DOMAIN`)
in the shell environoment, while the password in the system keyring. Alpaca will read the password from the system keyring
(in the `login` collection) using the attributes `service=alpaca` and `username=$NTLM_USERNAME`.

To store the password in the GNOME keyring, do the following:
```bash
$ export NTLM_USERNAME=<your-username-here>
$ export NTLM_DOMAIN=<your-domain-here>
$ sudo apt install libsecret-tools
$ secret-tool store -c login -l "NTLM credentials" "service" "alpaca" "username" $NTLM_USERNAME
Password:
# Type your password, then run
$ alpaca
```

On macOS and Linux/GNOME systems, Alpaca uses the PAC URL from your system settings.
If you'd like to override this, or if Alpaca fails to detect your settings, you
can set this manually using the `-C` flag.

### Command-line flags

| Flag | Default | Description |
|------|---------|-------------|
| `-l` | `localhost` | Address to listen on (can be specified multiple times) |
| `-p` | `3128` | Port number to listen on |
| `-C` | (none) | URL of proxy auto-config (PAC) file |
| `-d` | (none) | Domain of the proxy account (for NTLM auth) |
| `-u` | current user | Username for proxy auth (NTLM) |
| `-H` | `false` | Print hashed NTLM credentials and exit |
| `-no-kerberos` | `false` | Disable Kerberos / Negotiate auto-detection (macOS only) |
| `-enable-socks` | `false` | Allow SOCKS5 proxies from PAC files. SOCKS5 has its own auth model and bypasses alpaca's HTTP authentication chain (and therefore the proxy-auth allowlist). |
| `-q` | `false` | Quiet mode, suppress all log output. Also suppresses the proxy-auth-allowlist startup nudge. |
| `-version` | `false` | Print version and exit |

### Environment variables

| Variable | Description |
|----------|-------------|
| `NTLM_CREDENTIALS`            | `username@DOMAIN:hash` (run `alpaca -H` to generate) |
| `BASIC_CREDENTIALS`           | `login:password` for HTTP Basic proxy auth |
| `ALPACA_PROXY_AUTH_ALLOWLIST` | Comma-separated DNS suffixes that may receive proxy credentials. Applies uniformly to Basic, NTLM, and Negotiate. Default is permissive (any host); set to `*` for the explicit permissive form. See "Restricting where Alpaca sends credentials" above. |
| `NTLM_USERNAME` / `NTLM_DOMAIN` | Used by the keyring credential source (Linux/GNOME, Windows) |

---

### Proxy

You also need to configure your tools to send requests via Alpaca. Usually this
will require setting the `http_proxy` and `https_proxy` environment variables:

```sh
$ export http_proxy=http://localhost:3128
$ export https_proxy=http://localhost:3128
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md
# Alpaca
...
```

When moving from, say, a corporate network to a public WiFi network (or
vice-versa), the proxies listed in the PAC script might become unreachable.
When this happens, Alpaca will temporarily bypass the parent proxy and send
requests directly, so there's no need to manually unset/re-set `http_proxy` and
`https_proxy` as you move between networks.

[1]: https://github.com/samuong/alpaca/releases
[2]: https://img.shields.io/github/v/tag/samuong/alpaca.svg?logo=github&label=latest
[3]: https://img.shields.io/github/actions/workflow/status/samuong/alpaca/ci.yml?branch=master
[4]: https://img.shields.io/github/downloads/samuong/alpaca/latest/total
