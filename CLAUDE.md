# CLAUDE.md — Alpaca

## Project Overview

Alpaca is a local HTTP proxy for command-line tools written in Go. It supports:

- Proxy Auto-Configuration (PAC) files
- NTLM authentication
- Basic HTTP authentication
- Kerberos/Negotiate authentication (macOS only)
- System keyring integration (macOS, Windows, Linux/GNOME)
- Automatic network switching (bypasses unreachable proxies)

**Module path:** `github.com/samuong/alpaca/v2`
**License:** Apache 2.0

## Quick Reference

```bash
# Build
go build -v .

# Run all tests (CGO_ENABLED=1 is required)
CGO_ENABLED=1 go test ./...

# Format code
goimports -w .

# Lint
golangci-lint run
```

## Repository Structure

```
alpaca/
├── .github/workflows/     # CI (ci.yml) and release (release.yml) pipelines
├── assets/                # Logo and banner images
├── go.mod / go.sum        # Go module definition (Go 1.25.0+)
├── main.go                # Entry point, CLI flags, server bootstrap
├── proxy.go               # Core proxy handler (CONNECT tunneling, request forwarding)
├── transport.go           # Low-level connection management for CONNECT tunnels
├── authenticator.go       # NTLM authentication
├── basicauth.go           # Basic HTTP proxy authentication
├── multiauth.go           # authChain: picks authenticators for a 407 response
├── kerberos*.go           # Kerberos/Negotiate auth (macOS-specific)
├── credentials.go         # Credential sourcing (terminal, env, keyring)
├── keyring*.go            # System keyring integration per platform
├── pacfinder*.go          # PAC URL discovery (platform-specific)
├── pacfetcher.go          # PAC file downloading
├── pacrunner.go           # JavaScript PAC execution via otto VM
├── pacwrapper.go          # Wraps upstream PAC to point at localhost
├── proxyfinder.go         # Proxy discovery using PAC results
├── netmonitor.go          # Network connectivity monitoring
├── blocklist.go           # Temporary proxy blocklist during network changes
├── contextid.go           # Request context ID generation
├── requestlogger.go       # HTTP request/response logging middleware
├── CONTRIBUTING.md        # Contribution guidelines
└── *_test.go              # Test files (~16 test files)
```

## Architecture

### Request Handling Pipeline

Requests flow through a middleware chain built in `main.go:createServer`:

1. **AddContextID** — assigns a unique ID to each request via context
2. **ProxyFinder.WrapHandler** — discovers upstream proxy via PAC evaluation
3. **ProxyHandler.WrapHandler** — routes proxy requests (CONNECT or absolute-form URIs); non-proxy requests pass through to the mux
4. **RequestLogger** — logs all requests and responses

### Authentication Chain

Authentication methods are tried in order via `multiauth.go`. The chain is:
Negotiate → NTLM → Basic (matching Chrome's hierarchy). The `*authChain`
type is a *picker*: given the schemes the proxy advertised in its 407
response, plus the proxy hostname, it returns the ordered list of methods
the caller should attempt. `proxy.go` owns the iteration and the
connection-lifecycle invariants:

- CONNECT path (`retryConnectWithAuth`) re-dials the proxy on a fresh TCP
  connection between methods. This is required because NTLM and Negotiate
  are connection-bound (RFC 4559) and must not share a socket with another
  scheme's state machine.
- Plain HTTP path (`retryProxyRequestWithAuth`) gives each method its own
  cloned `*http.Transport` so its connection pool is isolated; the
  underlying `http.Transport` already manages connection reuse for NTLM's
  Type 1 → Type 3 sequence within a single method.
- The header `Proxy-Authorization` is cleared between attempts.
- Any error returned by a method aborts the chain (this is the
  abort-on-error invariant — see test `TestRetryProxyRequest_AbortsChainOnError`).

Negotiate availability is re-checked per-407 via `applicableTo()` rather
than at startup, so a Kerberos ticket that arrives after alpaca starts
(e.g. because Apple SSO finishes after the LaunchAgent launches alpaca,
or because the user runs `kinit` mid-session) is honoured automatically
without a restart.

Downgrade refusal: when the proxy returns 407 with no parseable
`Proxy-Authenticate`, only authenticators that opt in via
`safeWithoutChallenge() bool` are considered. Today that's NTLM and
Negotiate; Basic is excluded so its credentials are never sent without an
explicit server advertisement.

Host policy: each authenticator implements `applicableTo(proxyHost) bool`.
The `negotiateAuthenticator` uses this to enforce `KERBEROS_SPN_ALLOWLIST`,
falling through to the next method (instead of failing the whole chain) for
hosts that are out-of-policy.

### Key Interfaces

- `proxyAuthenticator` (in `proxy.go`) — implemented by `authenticator`
  (NTLM), `basicAuthenticator`, and `negotiateAuthenticator`. Methods:
  `do(req, rt) (resp, err)`, `scheme()`, `safeWithoutChallenge()`,
  `applicableTo(host)`.
- `*authChain` (in `multiauth.go`) — picks the ordered list of
  authenticators to try given the schemes the proxy advertised. NOT a
  `proxyAuthenticator` itself.

## Build & Test

### Requirements

- **Go 1.25.0+**
- **CGO_ENABLED=1** (required for builds and tests)

### Build

```bash
go build -v .
```

Cross-compilation:
```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -v .
```

Version injection at build time:
```bash
go build -v -ldflags "-X 'main.BuildVersion=v1.0.0'" .
```

### Test

```bash
CGO_ENABLED=1 go test ./...
```

Tests run on macOS (14), Ubuntu 22.04 (x86_64/ARM64), and Windows 2022 in CI.

### Lint & Format

```bash
goimports -w .          # Format (includes gofmt)
golangci-lint run       # Lint
```

Both are enforced in CI.

## Code Conventions

### Style

- **100-character line limit** — enforced in CI
- **Formatting:** `goimports` (not just `gofmt`)
- **Linting:** `golangci-lint`
- Follow [Effective Go](https://go.dev/doc/effective_go) patterns

### Naming

- Exported symbols: `PascalCase`
- Unexported symbols: `camelCase`
- Interfaces: descriptive names, often ending in `-er` (e.g., `proxyAuthenticator`)

### Testing

- Use **table-driven tests** where applicable
- Use `assert` and `require` from [testify](https://github.com/stretchr/testify) — not bare `if` checks
- Use `httptest.NewServer()` / `httptest.NewTLSServer()` for integration tests
- Every major component should have test coverage

### Commits

- Write clear, descriptive commit messages in plain English
- **Do not** use Conventional Commits prefixes (no `feat:`, `fix:`, `chore:`, etc.)
- Keep commits small and atomic — do not mix refactors with feature work

## CI/CD

### Continuous Integration (`.github/workflows/ci.yml`)

Runs on every push and PR to master:

| Job      | What it does                                          |
|----------|-------------------------------------------------------|
| format   | Validates `gofmt` on Ubuntu with Go 1.25             |
| lint     | Runs `golangci-lint`                                  |
| test     | Runs `go test ./...` on macOS, Ubuntu (x86/ARM), Windows |
| build    | Cross-compiles for darwin/linux/windows (amd64/arm64) |

### Release (`.github/workflows/release.yml`)

Triggered on tags matching `v*`. Creates a GitHub release and uploads platform-specific binaries.

## CLI Flags

| Flag        | Default      | Description                                    |
|-------------|-------------|------------------------------------------------|
| `-l`        | `localhost`  | Listen address (can be specified multiple times)|
| `-p`        | `3128`       | Port number                                    |
| `-C`        | (none)       | PAC file URL override                          |
| `-d`        | (none)       | NTLM domain                                    |
| `-u`        | current user | Username for proxy auth (NTLM)                 |
| `-H`        | false        | Print hashed NTLM credentials and exit         |
| `-w`        | `0`          | Seconds to wait at startup for a Kerberos ticket (macOS) |
| `--no-kerberos` | false    | Disable Kerberos auto-detection (macOS)        |
| `-k`        | false        | **Deprecated.** Equivalent to `-w 30`. Auto-detect makes it unnecessary |
| `-q`        | false        | Quiet mode — suppress all log output           |
| `-version`  | false        | Print version and exit                         |

## Environment variables

| Variable                 | Purpose                                                   |
|--------------------------|-----------------------------------------------------------|
| `NTLM_CREDENTIALS`       | `username@DOMAIN:hash` (generate with `alpaca -H`)        |
| `BASIC_CREDENTIALS`      | `login:password` for Basic auth                           |
| `KERBEROS_SPN_ALLOWLIST` | Comma-separated DNS suffixes that may receive SPNEGO tokens. On macOS defaults to the user's home Kerberos realm when unset; set to `*` to permit any host explicitly. |
| `NTLM_USERNAME`/`NTLM_DOMAIN` | Used by the keyring credential source                |

## Key Dependencies

| Dependency                    | Purpose                          |
|-------------------------------|----------------------------------|
| `github.com/robertkrimen/otto`| JavaScript VM for PAC execution  |
| `github.com/samuong/go-ntlmssp`| NTLM authentication            |
| `github.com/keybase/go-keychain`| macOS Keychain access          |
| `github.com/zalando/go-keyring`| Linux/Windows keyring access    |
| `github.com/gobwas/glob`     | Glob pattern matching            |
| `github.com/stretchr/testify`| Test assertions                  |
| `golang.org/x/term`          | Terminal password input          |

## Platform-Specific Code

Files with platform build tags:

- `*_darwin.go` — macOS-specific (Keychain, Kerberos, PAC via SCDynamicStore)
- `*_unix.go` — Unix/Linux-specific (PAC discovery)
- `*_windows.go` — Windows-specific (PAC discovery, credential management)
- `*_other.go` — Fallback stubs for unsupported platforms
