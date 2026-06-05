## Why

The Windows Negotiate backend's SSPI calls (credential acquisition, SPNEGO token generation) cannot be exercised by the unit suite - they require a domain-joined Windows session holding a real Kerberos credential. macOS has an automated integration test (Docker MIT KDC + Squid) that validates the real GSS.framework path against a live proxy; Windows has no equivalent, so the SSPI integration is validated only by a manual smoke test. This change closes that parity gap with an automated, opt-in Windows harness, and aligns the integration-test naming across both platforms.

## What Changes

- Add an automated integration test that provisions a throwaway Active Directory realm (Samba AD DC) and a Negotiate-advertising proxy (Squid with the Kerberos helper), obtains a real Kerberos credential on a Windows test environment, and drives alpaca's Negotiate path through the proxy.
- Assert the same invariants the macOS integration test asserts, where platform-appropriate: Negotiate succeeds with a real ticket; the chain prefers Negotiate over Basic; the chain falls through when no ticket is present; a 407 with no parseable `Proxy-Authenticate` yields no credentials.
- Adopt a single `integration` build tag for these tests across platforms. "e2e" reads as a full-system test of everything; these exercise one feature (Negotiate proxy auth) against real infrastructure, which is an integration test.
- **Rename the existing macOS integration test off "e2e"** (behaviour-preserving): `//go:build e2e && darwin` → `integration && darwin`, `TestKerberosE2E` → `TestKerberosIntegration`, and `testdata/kerberos-e2e/` → `testdata/kerberos-integration/`, updating all references (run commands, comments, READMEs).
- Gate the Windows test behind the `integration` build tag and skip conditions so it is invisible to anyone without the infrastructure, and run it only as a release-gate / on-demand CI job rather than on every push.
- No changes to alpaca's runtime code or user-facing contracts.

## Capabilities

### New Capabilities

- `negotiate-integration-testing`: automated integration tests that exercise Alpaca's real Negotiate proxy-authentication path against a live KDC and a Negotiate-advertising proxy on each supported platform (macOS and Windows), plus the shared `integration` build tag and naming convention both platforms adopt. This change adds the Windows test and its provisioning harness and brings the existing macOS test under this capability (renaming it off `e2e`).

### Modified Capabilities

- (none) - no source-of-truth spec existed for either platform's integration test before this change, so bringing the macOS test under the new `negotiate-integration-testing` capability and renaming its build tag, function, and fixture directory is a behaviour-preserving refactor, not a modification of an existing spec.

## Impact

- New test-only assets: a build-tagged Windows integration test, harness scripts to provision the Samba AD DC + Squid + domain join, and CI wiring for an on-demand/release Windows job.
- Renamed test-only assets on macOS (behaviour-preserving): the `e2e` build tag, the `TestKerberosE2E` function, and the `testdata/kerberos-e2e/` directory - which includes the `WINDOWS-TESTING.md` added by `add-windows-kerberos`.
- External dependency: a Windows VM environment in CI (cost: minutes of VM time per run); deliberately not part of the per-push matrix.
- No production-code or runtime-dependency changes.
