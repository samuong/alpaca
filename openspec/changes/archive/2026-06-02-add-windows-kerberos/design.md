## Context

Alpaca authenticates to Negotiate-advertising proxies on macOS using the system Kerberos credential via GSS.framework (`kerberos_darwin.go`). The authenticator is small: a `negotiateAuthenticator{hasTicket}` whose `do()` builds a single SPNEGO token for the proxy's `HTTP` service and attaches it as `Proxy-Authorization: Negotiate <base64>`. Host policy is not the authenticator's concern - the picker (`*authChain` in `multiauth.go`) enforces a uniform, chain-level allowlist (`ALPACA_PROXY_AUTH_ALLOWLIST`) across Basic, NTLM, and Negotiate.

Windows is the most common home for Negotiate proxies, and it exposes the logged-in user's Kerberos credential through SSPI. The merged macOS work already structured the code so a second backend is a drop-in: the chain, the picker, the retry/redial loop, and the `proxyAuthenticator` contract are all platform-agnostic. What is missing is a Windows implementation of two calls - "do we have a credential?" and "make me a token for this host?" - plus the small amount of shared authenticator code lifted out of the macOS file so both platforms use it.

This change deliberately tracks the *reviewed and merged* macOS design (PR #178 / #179), not the earlier iteration: there is no per-method SPN allowlist, no startup wait flag, and no `safeWithoutChallenge()` on the interface. The Windows backend inherits the chain-level allowlist instead of carrying its own.

## Goals / Non-Goals

**Goals:**

- Deliver Kerberos/Negotiate proxy auth on Windows with the same observable behaviour as macOS, wherever the platform allows it (see the parity matrix below).
- Implement the backend on Microsoft's SSPI Negotiate package (`github.com/alexbrainman/sspi/negotiate`) - pure Go, no cgo.
- Extract the shared authenticator into one file so the two backends cannot drift apart, while leaving the merged macOS code path behaviourally unchanged (reviewer sees "code moved", not "code rewritten").
- Justify each behaviour against the relevant RFC so the implementation is principled, not cargo-culted from the macOS code.
- Unit-test everything that is testable off a domain-joined host; document the rest as a manual smoke test (automated integration testing is the sibling change `add-windows-kerberos-integration`).

**Non-Goals:**

- Mutual authentication and channel binding (RFC 5929). macOS does not request them; Windows will match (single-leg, initiator-only).
- Forcing Kerberos and rejecting NTLM-under-Negotiate. RFC 4559 covers both mechanisms under the `Negotiate` scheme; matching the platform's native selection is correct.
- A Linux Kerberos backend. The stub remains for `!darwin && !windows`.
- Any change to the chain, the picker, the host-policy allowlist, or the `proxyAuthenticator` contract.
- New CLI flags or environment variables.

## Decisions

### Decision: extract a shared authenticator; keep two thin platform files

`negotiateAuthenticator` (the struct, `scheme()`, `applicableTo()`, `do()`) moves verbatim from `kerberos_darwin.go` into a new `kerberos_common.go` guarded by `//go:build darwin || windows`. Each platform file then provides only two package-level functions that the shared code already calls by name: `checkKerberosTicket() bool` and `generateSPNEGOToken(proxyHost string) ([]byte, error)`.

`newNegotiateAuthenticator()` stays *per platform* rather than moving to the shared file. Rationale: it is the only place with platform-flavoured startup log wording, and keeping it split lets the merged macOS log output stay byte-for-byte identical - the macOS diff becomes purely "lines moved to common", which is the easiest possible review. The cost is ~10 duplicated lines per platform, which is cheaper than a behavioural change to merged code. (Alternative considered: a single shared constructor with neutral wording - rejected because it perturbs merged macOS output for no functional gain.)

The non-Kerberos stub in `kerberos.go` changes its build tag from `!darwin` to `!darwin && !windows`.

### Decision: SPN is `HTTP/<host>` on Windows, `HTTP@<host>` on macOS - same principal, different naming API

Both forms name the identical Kerberos service principal `HTTP/<host>@<REALM>` (RFC 4120). The difference is the naming convention each API consumes: GSS.framework imports a host-based service name `service@host` (`GSS_C_NT_HOSTBASED_SERVICE`, RFC 2743 §4.1), while SSPI's Negotiate package expects the SPN syntax `service/host` that Active Directory registers and `setspn` displays. This is a platform-appropriate divergence, not a behavioural one: the KDC issues the same ticket either way. Documented inline so a future reader does not "fix" one to match the other.

### Decision: single-leg token, initiator-only (match macOS, per RFC 4559)

The macOS backend calls `gss_init_sec_context` once with no flags and sends the resulting token; it never processes a return token from the proxy. Windows matches: `negotiate.NewClientContext` yields the initial output token and we send only that. RFC 4559 §5 allows additional round trips for mutual authentication, but for client-to-proxy Kerberos the initiator's first token is what the proxy validates against its keytab. Requesting mutual auth or continuing the handshake would be a *new* behaviour absent on macOS, so it is explicitly out of scope.

### Decision: ticket presence via `AcquireCurrentUserCredentials`, released immediately

`checkKerberosTicket()` probes by acquiring the current user's Negotiate credential handle and releasing it at once - we only need to know whether acquisition would succeed. This is the SSPI analogue of macOS's `gss_acquire_cred` for the default initiator credential. The handle (and the per-token context in `generateSPNEGOToken`) must be explicitly `Release()`d: SSPI handles are owned by the OS, not the Go GC, so a missed release leaks a kernel handle on every authenticated request. Both release paths use `defer`.

### Decision: inherit the chain-level allowlist; no Windows-specific SPN allowlist

The merged design enforces host policy once, uniformly, in `*authChain.pick` via `ALPACA_PROXY_AUTH_ALLOWLIST`. Negotiate's `applicableTo()` is reserved for runtime preconditions (ticket presence, non-empty host) and explicitly must not duplicate host policy - the `proxyAuthenticator` interface doc says so. Windows therefore needs nothing here: it is protected by the same gate as Basic and NTLM. This omits the `KERBEROS_SPN_ALLOWLIST`, `parseSPNAllowlist`, `defaultKerberosRealm`/`USERDNSDOMAIN`, and startup wait loop that an earlier draft of this backend carried before the macOS design was simplified.

### Decision: dependency `github.com/alexbrainman/sspi`

It is the de-facto pure-Go SSPI wrapper (used by Go's own `golang.org/x/crypto/ssh/agent` ecosystem and many proxy tools), needs no cgo, and is import-guarded behind `//go:build windows` so it never enters the macOS or Linux build graph. Alternative considered: hand-rolled `syscall` bindings to `secur32.dll` - rejected as needless surface area for a well-trodden wrapper.

## macOS ↔ Windows parity matrix

Every observable Negotiate behaviour on macOS, and how Windows matches it. "Shared" means the behaviour lives in `kerberos_common.go` and is identical by construction.

| Behaviour | macOS (GSS.framework) | Windows (SSPI) | Shared? | Basis |
|-----------|-----------------------|----------------|---------|-------|
| Registered in chain unless `--no-kerberos` | yes (`main.go`) | yes (same code path) | main.go | - |
| Returns a usable authenticator even with no ticket at startup | yes | yes | per-platform ctor | auto-detect; ticket may arrive later |
| Startup log: ticket found / will re-check per-407 | yes | yes (own wording) | per-platform ctor | - |
| `scheme()` == `"Negotiate"` | yes | yes | shared | RFC 4559 §4 |
| `applicableTo`: false on empty host | yes | yes | shared | cannot form an SPN |
| `applicableTo`: re-check ticket every 407, silent fall-through if absent | yes | yes | shared | ticket may expire/arrive mid-session |
| `applicableTo`: does NOT enforce host policy | yes | yes | shared | host policy is chain-level (`multiauth.go`) |
| `do`: attach `Proxy-Authorization: Negotiate <base64(token)>` | yes | yes | shared | RFC 4559 §4, RFC 9110 §11.7.2, RFC 4648 §4 |
| `do`: error if proxy host missing from context | yes | yes | shared | - |
| SPN target | `HTTP@host` | `HTTP/host` | per-platform | same principal; RFC 4120, RFC 2743 §4.1 |
| Token generation: single initiator leg, no mutual auth | `gss_init_sec_context` once, flags=0 | `NewClientContext`, send first token only | per-platform | RFC 4559 §5 (extra legs optional) |
| Token generation: error on empty token | yes | yes | per-platform | a 0-byte token is a failure |
| Ticket presence check | `gss_acquire_cred`, lifetime>0 | `AcquireCurrentUserCredentials` then `Release` | per-platform | RFC 4120 |
| Connection-bound handshake (fresh dial per method) | handled by `proxy.go` | same | shared (proxy.go) | RFC 4559 §5 |
| 407 with no parseable `Proxy-Authenticate` → no credentials | yes (picker) | yes (picker) | shared (multiauth.go) | RFC 9110 §11.7.1 / §15.5.8 |
| Host policy via `ALPACA_PROXY_AUTH_ALLOWLIST` | yes (picker) | yes (picker) | shared (multiauth.go) | uniform across schemes |

## RFC conformance summary

- **RFC 4559** (SPNEGO-based Kerberos and NTLM HTTP Authentication): the `Negotiate` scheme, the `Proxy-Authorization: Negotiate <base64>` form, and the connection-oriented handshake. Both backends emit a single initiator token; §5's optional mutual-auth round trips are not used.
- **RFC 9110** (HTTP Semantics) §11.7.1 (`Proxy-Authenticate`), §11.7.2 (`Proxy-Authorization`), §15.5.8 (407): the picker already refuses to send credentials when a 407 carries no parseable challenge; Windows inherits this unchanged.
- **RFC 4178** (SPNEGO): the negotiation mechanism that both GSS.framework and SSPI's Negotiate package implement; alpaca consumes the resulting token opaquely.
- **RFC 4120** (Kerberos V5): the underlying ticketing; the service principal `HTTP/<host>@<REALM>` is identical on both platforms.
- **RFC 2743** (GSS-API v2) §4.1: the host-based service name form `HTTP@host` that GSS.framework consumes, versus the SPN form SSPI consumes.
- **RFC 4648** §4: base64 of the token bytes.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| SSPI `Negotiate` may select NTLM under the hood on a non-domain machine, so `checkKerberosTicket` returns true and Negotiate is offered where only NTLM is viable | Acceptable and RFC-4559-conformant (Negotiate spans Kerberos and NTLM); the proxy validates the token and the chain still falls through to NTLM/Basic. Documented as expected behaviour, matching native Windows clients. |
| Leaked SSPI credential/context handles on every request | `defer cred.Release()` and `defer cc.Release()`; covered by code review and the manual smoke test (handle count stable across many requests). |
| `NewClientContext` fails for a specific SPN (no service ticket, cross-realm, clock skew) and aborts the chain → 502 | Same failure shape as macOS (a `do()` error aborts per the existing abort-on-error invariant). Smoke-test guidance lists clock skew and keytab/SPN mismatch as first checks. Refining per-host fall-through is a separate future change for both platforms. |
| SSPI calls are not exercised by the unit suite | Unit-test the wiring and the shared logic; validate the real SSPI path via the documented smoke test and, subsequently, the automated harness in `add-windows-kerberos-integration`. |
| An earlier draft of this backend carried features later removed from the macOS design (per-method allowlist, startup wait, `safeWithoutChallenge`); reintroducing them by reflex would diverge from `master` | This design pins the target shape explicitly; the spec deltas and tasks enumerate only the current surface. |

## Migration Plan

Additive and platform-isolated. Two commits, in order: (1) extract the shared authenticator (macOS behaviour unchanged), (2) add the Windows backend, dependency, tests, and docs. Rollback is a straight revert of either commit; no state, config, or data migration is involved. macOS and Linux builds are unaffected (verified by `go vet`/build on each platform in CI).

## Open Questions

- Confirm we do **not** want to force Kerberos-only on Windows (i.e. accept Negotiate-wrapped NTLM). Recommended: accept it, per RFC 4559 and native-client parity.
- Startup log wording on Windows: mirror the macOS phrasing with platform-appropriate examples (no Apple SSO reference), or keep it minimal. Recommended: minimal, platform-neutral examples.
