## 1. Rename the macOS integration test off "e2e" (behaviour-preserving)

- [x] 1.1 In `kerberos_integration_test.go`, change the build tag `//go:build e2e && darwin` to `//go:build integration && darwin` and rename `TestKerberosE2E` to `TestKerberosIntegration`; update the file's own doc comment (it currently describes the `e2e` tag and the run command)
- [x] 1.2 Rename the fixture directory `testdata/kerberos-e2e/` to `testdata/kerberos-integration/` (carrying `Dockerfile`, `README.md`, and `WINDOWS-TESTING.md`); update the Dockerfile path reference in the test and any path references in the READMEs
- [x] 1.3 Grep the repo and `.github/workflows/` for `-tags=e2e`, `TestKerberosE2E`, and `kerberos-e2e`; update every hit. The rename is not done until no `e2e` reference remains
- [x] 1.4 Confirm behaviour-preserving: `go test -tags=integration -run TestKerberosIntegration -v .` still builds the macOS fixture and runs (or skips cleanly without Docker), exactly as the old `-tags=e2e` command did

## 2. Provisioning assets (Windows)

- [x] 2.1 Create `testdata/kerberos-windows-integration/` with a Samba AD DC bring-up (realm, KDC, test domain user, `HTTP/<proxy-host>` service principal, exported keytab) using fictitious identifiers - `Dockerfile` + `bootstrap.sh` + `docker-compose.yml`
- [x] 2.2 Add a Squid configuration that advertises Negotiate (backed by the keytab) and Basic, against the provisioned realm - `squid.conf`
- [x] 2.3 Add a PowerShell helper that joins the Windows host to the realm, logs in the test user, and confirms a TGT (`klist`) - `join-and-test.ps1`
- [x] 2.4 Add a readiness gate that health-checks the KDC before squid starts (`bootstrap.sh` `wait_for_kdc`), and force time sync on the Windows side to avoid Kerberos clock skew (`join-and-test.ps1` `w32tm /resync`)

## 3. Integration test (Windows)

- [x] 3.1 Add `kerberos_windows_integration_test.go` (`//go:build integration && windows`) with `TestKerberosWindowsIntegration`, mirroring the macOS `kerberos_integration_test.go` structure
- [x] 3.2 Assert the invariants reproducible against a live proxy: Negotiate succeeds with a real ticket; Negotiate is preferred over Basic (Basic not invoked); the chain falls through to Basic when the ticket is gone; only-Negotiate-but-ineligible yields `errNoMatchingAuthMethod` (no Basic downgrade); the chain-level allowlist excludes the proxy. (The 407-without-`Proxy-Authenticate` refusal is unit-tested in `multiauth_test.go`; a correctly configured Squid always advertises a scheme, so it is not reproducible here.)
- [x] 3.3 Use `t.Skip()` (not `t.Fatal()`) when the proxy endpoint or a Kerberos credential is unavailable, with a clear diagnostic
- [x] 3.4 The test cleans up its HTTP transport on exit; the realm/proxy/domain-join lifecycle is owned by the provisioning harness (task 2), not the test

## 4. CI wiring

- [x] 4.1 Add a dedicated workflow (`workflow_dispatch` + release tags) that provisions the environment and runs `go test -tags=integration -run TestKerberosWindowsIntegration` - `.github/workflows/windows-kerberos-integration.yml`
- [x] 4.2 Confirm the standard `ci.yml` does NOT run the integration tests (its `test` job runs `go test ./...` with no `integration` tag, so the build tag keeps them out)
- [x] 4.3 Document the raw invocation `go test -tags=integration -run TestKerberosWindowsIntegration -v .` (the Windows analog of the macOS `go test -tags=integration -run TestKerberosIntegration -v .`); do not add task-runner wrappers - the repo uses plain go commands

## 5. Documentation

- [x] 5.1 Add `testdata/kerberos-windows-integration/README.md` documenting the topology, the env contract, how to run locally, and the rejected approaches (Wine, all-in-one container, reusing the macOS Docker fixture) with reasons
- [x] 5.2 Move the manual `WINDOWS-TESTING.md` into `testdata/kerberos-windows-integration/` (consolidating Windows testing docs) and cross-reference the automated harness from it

## 6. Validation

- [x] 6.1 `openspec validate add-windows-kerberos-integration --strict` passes
- [ ] 6.2 EXTERNAL DEPENDENCY (cannot be done from a macOS dev host): on a domain-joinable Windows runner/VM, dry-run the harness end to end and confirm `TestKerberosWindowsIntegration` reaches a real Negotiate `200` and all sub-tests pass before relying on it as a release gate. See `testdata/kerberos-windows-integration/README.md` "Validation checklist". Tracked in smithjw/alpaca#2.
- [ ] 6.3 When archiving this change, set a real `## Purpose` on the newly created `negotiate-integration-testing` spec - the archive step seeds a `TBD` placeholder that must be replaced (mirroring the Purpose on `negotiate-authentication`).
