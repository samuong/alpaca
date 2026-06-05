## Why

Alpaca offers Kerberos/Negotiate proxy authentication on macOS (via GSS.framework) but not on Windows, even though domain-joined Windows machines are the most common environment for Negotiate-advertising corporate proxies. Windows exposes the logged-in user's Kerberos credential through SSPI, so alpaca can deliver the same single-sign-on proxy auth that Windows browsers already get - no password prompt, no stored secret.

## What Changes

- Add a Windows Negotiate backend built on SSPI (`github.com/alexbrainman/sspi/negotiate`) that satisfies the same `proxyAuthenticator` contract as the macOS backend.
- Extract the platform-agnostic Negotiate authenticator (the `scheme`/`applicableTo`/`do` behaviour and the startup constructor) into a shared file compiled for `darwin || windows`. Each platform then supplies only two calls: a ticket-presence check and an SPNEGO token generator.
- Make Negotiate auto-detection genuinely cross-platform: the `--no-kerberos` opt-out and the per-407 ticket re-check apply on Windows exactly as on macOS.
- Reuse the existing chain-level host policy (`ALPACA_PROXY_AUTH_ALLOWLIST`) on Windows. No Windows-specific allowlist is introduced.
- Add unit tests for the Windows wiring and the shared authenticator, plus a documented manual smoke-test procedure. (An automated integration harness is proposed separately in `add-windows-kerberos-integration`.)
- No new flags, no new environment variables, no **BREAKING** changes.

## Capabilities

### New Capabilities

- `negotiate-authentication`: alpaca's Kerberos/Negotiate proxy-authentication behaviour - scheme handling, ticket auto-detection with per-407 re-check, SPN construction, single-leg SPNEGO token generation, and the platform backends (macOS GSS.framework, Windows SSPI) that implement it.

### Modified Capabilities

- (none) - no existing spec's requirements change. The chain-level host-policy behaviour is unchanged; Windows simply inherits it.

## Impact

- New code: `kerberos_common.go` (shared authenticator), `kerberos_windows.go` + `kerberos_windows_test.go` (new backend and its unit tests).
- Modified code: `kerberos_darwin.go` (slimmed to the two platform calls), `kerberos.go` (build constraint becomes `!darwin && !windows`), `main.go` (startup log wording no longer says "macOS only").
- New dependency: `github.com/alexbrainman/sspi` - Windows-only, pure Go (no cgo).
- CI: the existing `windows-2022` job now compiles and unit-tests the backend; no matrix change required.
- Docs: `README.md` and `CLAUDE.md` platform-support wording updated to list Windows.
