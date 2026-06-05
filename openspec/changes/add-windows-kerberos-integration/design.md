## Context

The Windows Negotiate backend (`add-windows-kerberos`) calls into SSPI, which only works against the Windows Local Security Authority on a host that holds a real Kerberos credential. Those calls cannot be exercised by the unit suite, which runs on undomained CI runners and developer macOS/Linux machines.

macOS has an automated integration test (`kerberos_integration_test.go`, build-tagged `e2e && darwin`, backed by `testdata/kerberos-e2e/`) that builds a Docker image with an MIT KDC + Squid and drives alpaca's real GSS.framework path through it. That fixture cannot be reused for Windows: SSPI talks to the Windows LSA, not to a Linux MIT KDC, so even with a `krb5.ini` on the Windows host the LSA knows nothing about the container's KDC.

This change adds the Windows equivalent - an opt-in integration test that stands up a real Active Directory realm, joins a Windows host to it, obtains a genuine Kerberos credential, and drives alpaca's Negotiate path through a Negotiate-advertising proxy - and, while establishing that test, aligns the naming so both platforms use a single `integration` build tag rather than the existing `e2e` tag on macOS.

## Goals / Non-Goals

**Goals:**

- Validate the real SSPI path: credential acquisition, `HTTP/<host>` SPN, single-leg SPNEGO token, proxy acceptance.
- Mirror the macOS integration assertions where platform-appropriate: Negotiate succeeds with a ticket; the chain prefers Negotiate over Basic; the chain falls through when no ticket is present; a 407 with no parseable `Proxy-Authenticate` yields no credentials.
- Use one consistent `integration` build tag across macOS and Windows, renaming the macOS test off `e2e` behaviour-preservingly.
- Make the Windows test opt-in and self-skipping so it is invisible to anyone without the infrastructure.
- Run it as a release-gate / on-demand CI job, not on every push.

**Non-Goals:**

- Running on the per-push CI matrix. Provisioning a domain and a Windows host costs minutes per run; the existing `windows-2022` compile + unit job stays the per-push guard.
- Reusing the macOS Docker fixture (the LSA-vs-MIT-KDC mismatch above).
- A Wine-based harness: Wine loads `secur32.dll` but does not implement SSPI's Negotiate package against an external KDC, so credential acquisition fails even with a populated MIT cache.
- Changing any alpaca runtime code, or changing what the macOS test does (the rename is naming-only).

## Decisions

### Decision: name these "integration" tests, not "e2e"

"e2e" (end-to-end) implies a full-system test of the whole product. These tests exercise one feature - Negotiate proxy authentication - against real infrastructure (a KDC and a proxy), which is precisely an integration test. Standardise on a single `integration` build tag so `go test -tags=integration ./...` runs the real-infrastructure tests on whatever platform they're gated to, and the macOS and Windows tests read as one family.

### Decision: rename the macOS integration test off `e2e` (behaviour-preserving)

The macOS test file is already `kerberos_integration_test.go`, but its build tag, function name, run command, and fixture directory still say `e2e`. Align them:

- build tag `//go:build e2e && darwin` → `//go:build integration && darwin`
- function `TestKerberosE2E` → `TestKerberosIntegration`
- fixture directory `testdata/kerberos-e2e/` → `testdata/kerberos-integration/` (carrying its `Dockerfile`, `README.md`, and the `WINDOWS-TESTING.md` that `add-windows-kerberos` added)
- every reference: the file's own doc comment, the run command in docs, and any README

This is a pure rename - the Docker fixture, the assertions, and the GSS.framework path are untouched.

### Decision: real Active Directory via Samba AD DC, not a hand-rolled KDC

The realm is provided by Samba in AD DC mode (a Linux container or VM). It issues the domain, the KDC, the test user, and the proxy's service principal, and exports a keytab for the proxy. This is the only approach that produces a domain a Windows host can actually *join*, which is the precondition for SSPI to acquire a usable credential. A bare MIT KDC (as macOS uses) is insufficient because the Windows side needs domain membership, not just a krb5 cache.

### Decision: topology - Samba AD DC + Squid on Linux, alpaca on a joined Windows host

The Linux side runs the Samba AD DC and a Squid that advertises `Negotiate` (and `Basic`, to test preference/fall-through) using a keytab for `HTTP/<proxy-host>` from the realm. The Windows side joins the domain, logs in as the test domain user (obtaining a TGT), and runs alpaca pointed at Squid. The Go test orchestrates the run from the Windows host and asserts on alpaca's behaviour and the proxy's responses.

### Decision: gate behind `integration && windows` and skip on missing infrastructure

The Windows test file carries `//go:build integration && windows`, matching the renamed macOS `integration && darwin` convention. At runtime it calls `t.Skip()` (not `t.Fatal()`) when the domain, the proxy endpoint, or a TGT is absent, so a developer on an unjoined Windows box - and the standard CI matrix - never sees a failure for missing infrastructure.

### Decision: provisioning lives in `testdata/kerberos-windows-integration/`

A sibling of the renamed macOS `testdata/kerberos-integration/`: scripts to bring up the Samba AD DC + Squid, a PowerShell helper to join the domain and obtain a ticket, and a README documenting the topology and how to run it locally. All identifiers are fictitious (`EXAMPLE.TEST` and similar), consistent with the macOS fixture.

### Decision: a dedicated on-demand CI workflow, separate from `ci.yml`

A new workflow (manual `workflow_dispatch` plus release tags) provisions the environment and runs `go test -tags=integration -run TestKerberosWindowsIntegration`. Keeping it out of `ci.yml` preserves fast per-push feedback while still giving a release-gate signal.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| The `e2e`→`integration` tag rename silently drops the macOS test from CI if a workflow still passes `-tags=e2e` | Grep the repo and any CI workflow for `-tags=e2e` / `TestKerberosE2E` and update them in the same change; the rename task is not done until no `e2e` reference remains |
| Kerberos is intolerant of clock skew (>5 min) between the Windows host and the Samba KDC | Force time sync as a provisioning step; surface skew in the failure output |
| Domain-join automation is brittle across Windows images | Script with explicit waits and retries; pin the Windows image; `t.Skip()` with a clear diagnostic if join did not complete |
| VM/runner cost if accidentally wired into per-push CI | Separate `workflow_dispatch`/release-only workflow; build tag keeps it out of the default `go test ./...` |
| Samba AD DC flakiness on first boot | Health-check the KDC/LDAP ports before the Windows side attempts to join, mirroring the macOS fixture's readiness gate |
| Maintenance burden of a second integration environment | Reuse the macOS fixture's structure and assertions; document clearly so it is approachable; treat as release-gate, not a daily dependency |

## Relationship to PR #178 testing feedback

Two testing points the maintainer raised on PR #178 and deferred to a future change inform this one:

- *Consolidating divergent test approaches.* The maintainer noted the repo has more than one testing style (some tests assume a locally installed Squid; the Kerberos fixture drives Docker from `go test`) and slightly preferred wrapping the whole system - including the alpaca binary - inside the container rather than driving Docker from the test. For the Windows harness that consolidation isn't available: the host under test must be domain-joined, so the alpaca binary runs on the Windows host while the realm and proxy run on Linux - a single all-in-one container can't host a domain-joined Windows client. The Go test therefore stays the orchestrator on Windows. Re-platforming the macOS Docker fixture onto the preferred pattern remains a separate future cleanup.
- *A real NTLM server for the auth tests.* The maintainer asked whether a real NTLM server (e.g. Samba) could back `authenticator_test.go` instead of fakes. The Samba AD DC this change stands up is exactly such a server; once it exists, pointing the NTLM tests at it is a natural follow-up. Out of scope here, but called out so the infrastructure isn't duplicated later.

## Migration Plan

Test-only and additive apart from the behaviour-preserving macOS rename. Nothing to roll back beyond reverting the rename and removing the Windows test, its `testdata/` assets, and the dedicated workflow. The Windows harness depends on `add-windows-kerberos` having landed (there is nothing to exercise otherwise); the macOS rename is independent and could land first.

## Open Questions

- CI substrate for the Windows host: a cloud Windows runner that can domain-join, or a self-hosted VM (e.g. libvirt/Vagrant). Recommended: start with `workflow_dispatch` on a self-hosted/VM runner and decide on a hosted option once the harness is proven.
- Whether to co-locate Squid on the Samba host or as a third node. Recommended: co-locate to keep the topology to two nodes (Linux DC+proxy, Windows client).
