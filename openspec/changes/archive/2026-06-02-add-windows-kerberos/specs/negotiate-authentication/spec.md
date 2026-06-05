## ADDED Requirements

### Requirement: Negotiate proxy authentication on macOS and Windows

Alpaca SHALL offer Kerberos/Negotiate proxy authentication on both macOS and Windows using the operating system's current Kerberos credential, and SHALL NOT require a credential to be present at the moment alpaca starts.

#### Scenario: Credential present at startup

- **WHEN** alpaca starts on a supported platform and the user holds a usable Kerberos credential
- **THEN** Negotiate is registered in the authentication chain and a startup log line records that a ticket was found

#### Scenario: No credential at startup

- **WHEN** alpaca starts on a supported platform with no Kerberos credential available
- **THEN** Negotiate is still registered and alpaca logs that it will re-check on each 407 response

#### Scenario: Auto-detection disabled

- **WHEN** alpaca is started with the `--no-kerberos` flag
- **THEN** no Negotiate authenticator is registered on any platform

#### Scenario: Unsupported platform

- **WHEN** alpaca runs on a platform other than macOS or Windows
- **THEN** no Negotiate authenticator is constructed

### Requirement: Per-407 applicability with silent fall-through

The Negotiate authenticator SHALL re-evaluate Kerberos credential availability on every 407 response and SHALL decline - allowing the chain to fall through to the next method - rather than fail the chain when no credential is available or the proxy host is empty. It SHALL NOT enforce host policy itself.

#### Scenario: Credential available for a non-empty host

- **WHEN** the picker evaluates Negotiate for a 407 from a non-empty proxy host and a credential is available
- **THEN** Negotiate is applicable regardless of the host's DNS suffix

#### Scenario: Credential unavailable

- **WHEN** the picker evaluates Negotiate and no credential is currently available
- **THEN** Negotiate declines and the chain proceeds to the next configured method

#### Scenario: Empty proxy host

- **WHEN** the picker evaluates Negotiate and the proxy host is empty
- **THEN** Negotiate declines

### Requirement: SPNEGO token generation

When performing Negotiate authentication the authenticator SHALL generate a single initiator SPNEGO token for the proxy's HTTP service principal and attach it as a base64-encoded `Proxy-Authorization: Negotiate` header. It SHALL return an error, aborting the attempt, when the proxy host cannot be determined or when token generation yields an empty token. It SHALL NOT perform mutual authentication or additional handshake legs.

#### Scenario: Token generated and attached

- **WHEN** Negotiate authenticates a request to a resolvable proxy host with a valid credential
- **THEN** the request carries a `Proxy-Authorization: Negotiate <base64-token>` header and is sent once

#### Scenario: Proxy host missing

- **WHEN** Negotiate runs without a resolvable proxy host in the request context
- **THEN** it returns an error and sends no credential

#### Scenario: Empty token

- **WHEN** the platform token generator returns a zero-length token
- **THEN** Negotiate returns an error

### Requirement: Platform-appropriate service principal naming

The macOS backend SHALL request the GSS host-based service name form `HTTP@<host>`, and the Windows backend SHALL request the SSPI service principal name form `HTTP/<host>`. Both forms name the same Kerberos service principal for the proxy.

#### Scenario: macOS service name form

- **WHEN** the macOS backend generates a token for proxy host `proxy.example.com`
- **THEN** it requests the GSS host-based service name `HTTP@proxy.example.com`

#### Scenario: Windows service principal form

- **WHEN** the Windows backend generates a token for proxy host `proxy.example.com`
- **THEN** it requests the SSPI service principal name `HTTP/proxy.example.com`

### Requirement: Host policy enforced by the chain, not by Negotiate

Eligibility of a proxy host to receive credentials SHALL be enforced uniformly by the authentication chain via the `ALPACA_PROXY_AUTH_ALLOWLIST` environment variable. The Negotiate authenticator SHALL NOT implement a separate per-method host allowlist on any platform.

#### Scenario: Host excluded by the chain allowlist

- **WHEN** a proxy host is not permitted by `ALPACA_PROXY_AUTH_ALLOWLIST`
- **THEN** the picker returns no authenticators and no SPNEGO token is generated for that host

#### Scenario: Host permitted

- **WHEN** a proxy host is permitted by the allowlist, or no allowlist is configured
- **THEN** Negotiate is eligible subject to credential availability
