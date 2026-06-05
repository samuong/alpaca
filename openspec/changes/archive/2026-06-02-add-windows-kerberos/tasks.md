## 1. Shared Negotiate authenticator extraction

- [x] 1.1 Create `kerberos_common.go` (`//go:build darwin || windows`) and move the `negotiateAuthenticator` struct (`{hasTicket}`), `scheme()`, `applicableTo()`, and `do()` verbatim from `kerberos_darwin.go`
- [x] 1.2 In `kerberos_darwin.go`, keep only the GSS.framework cgo, `checkKerberosTicket()`, `generateSPNEGOToken()`, and the macOS `newNegotiateAuthenticator()`; confirm behaviour is unchanged
- [x] 1.3 Change `kerberos.go` build constraint from `!darwin` to `!darwin && !windows`
- [x] 1.4 Move `TestNegotiateApplicableTo` from `kerberos_darwin_test.go` to `kerberos_common_test.go` (`//go:build darwin || windows`) so both platforms run it
- [x] 1.5 `CGO_ENABLED=1 go build` + `go test` on macOS: confirm the extraction is behaviour-preserving

## 2. Windows SSPI backend

- [x] 2.1 Add `github.com/alexbrainman/sspi` to `go.mod`/`go.sum` (imported only from the `windows` build)
- [x] 2.2 Create `kerberos_windows.go` (`//go:build windows`): `checkKerberosTicket()` via `negotiate.AcquireCurrentUserCredentials()` + immediate `Release()`; `generateSPNEGOToken()` via `negotiate.NewClientContext(cred, "HTTP/"+host)` returning the first-leg token, with an empty-token guard; release the credential and context handles with `defer`
- [x] 2.3 Add the Windows `newNegotiateAuthenticator()` matching the macOS shape - no allowlist, no startup wait, no `safeWithoutChallenge`; platform-neutral startup log wording
- [x] 2.4 Confirm no `KERBEROS_SPN_ALLOWLIST`, `parseSPNAllowlist`, `defaultKerberosRealm`, or wait-loop code is carried over

## 3. Tests

- [x] 3.1 `kerberos_common_test.go`: `applicableTo` cases (ticket present, ticket absent, empty host) and the `do()` missing-proxy-host error path
- [x] 3.2 `kerberos_windows_test.go` (`//go:build windows`): `newNegotiateAuthenticator()` returns a non-nil `*negotiateAuthenticator` whose `scheme()` is `"Negotiate"` and whose `hasTicket` is wired; `checkKerberosTicket()` is safe to call in any environment (returns a bool without panicking)
- [x] 3.3 `CGO_ENABLED=1 go test ./...` passes on macOS; `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .` compiles the Windows backend and resolves the SSPI import

## 4. Documentation

- [x] 4.1 Update `README.md` and `CLAUDE.md` so Kerberos/Negotiate is listed for macOS and Windows; remove "macOS only" where it no longer applies
- [x] 4.2 Update the `--no-kerberos` flag help text in `main.go` to drop "macOS only"
- [x] 4.3 Add a concise `testdata/kerberos-e2e/WINDOWS-TESTING.md` manual smoke-test procedure, cross-referencing the automated harness change for full coverage

## 5. Validation

- [x] 5.1 `goimports`/`gofmt` clean on the changed files (100-character limit); pre-existing lint findings in untouched files left as-is
- [x] 5.2 `go vet` on macOS and `GOOS=windows go vet` clean for the changed files
- [x] 5.3 `openspec validate add-windows-kerberos --strict` passes
