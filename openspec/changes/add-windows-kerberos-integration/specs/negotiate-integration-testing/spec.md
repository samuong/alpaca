## ADDED Requirements

### Requirement: Automated Negotiate integration test per supported platform

Each platform with a Negotiate backend (currently macOS and Windows) SHALL have an automated integration test that drives Alpaca's real Negotiate proxy-authentication path against a live Negotiate-advertising proxy, using a genuine Kerberos credential issued by a real Key Distribution Center rather than a mock or in-process fake.

#### Scenario: Negotiate succeeds with a real credential

- **WHEN** the platform's integration test runs with a valid Kerberos credential present, against a proxy that advertises Negotiate
- **THEN** Alpaca authenticates via Negotiate and the proxied request returns the expected upstream success response

#### Scenario: Negotiate preferred over Basic

- **WHEN** the proxy advertises both Negotiate and Basic and a credential is available
- **THEN** Alpaca authenticates with Negotiate and does not invoke Basic

#### Scenario: Fall-through when no credential

- **WHEN** no Kerberos credential is available
- **THEN** the chain does not complete Negotiate and proceeds to the next configured method

#### Scenario: No silent downgrade to Basic

- **WHEN** only Negotiate is configured but it is ineligible because no usable credential is present
- **THEN** no credentials are sent and the attempt fails rather than downgrading to Basic

#### Scenario: Host excluded by the chain allowlist

- **WHEN** the proxy host is excluded by the chain-level proxy-auth allowlist
- **THEN** no authenticator is attempted for that host, uniformly across methods

### Requirement: Consistent integration-test invocation across platforms

The real-infrastructure Kerberos/Negotiate tests SHALL be selected by a single `integration` build tag on every platform, not `e2e`, and SHALL share a consistent test-function and fixture-directory naming scheme so they form one recognisable family invoked the same way everywhere.

#### Scenario: A single tag selects the integration tests

- **WHEN** a developer runs `go test -tags=integration ./...`
- **THEN** the current platform's Kerberos integration test (macOS or Windows) is selected, without requiring any `e2e` tag

#### Scenario: No e2e identifiers remain

- **WHEN** inspecting the Kerberos integration tests on any platform
- **THEN** none uses an `e2e` build tag, test-function name, or fixture-directory name

### Requirement: Opt-in and self-skipping

The integration tests SHALL be excluded from the default build and SHALL skip rather than fail when their infrastructure is unavailable, so the standard test matrix and developers without a Kerberos environment are unaffected.

#### Scenario: Excluded from the default build

- **WHEN** `go test ./...` runs without the `integration` build tag
- **THEN** the integration tests are neither compiled nor executed

#### Scenario: Skipped when infrastructure is absent

- **WHEN** an integration test is built with its tag but the proxy endpoint or a Kerberos credential is unavailable
- **THEN** it calls `t.Skip()` with a diagnostic message instead of failing

### Requirement: Provisioned, reproducible environment with fictitious identifiers

Each integration test SHALL be backed by provisioning assets that stand up a throwaway Kerberos realm, a Negotiate-advertising proxy holding a service keytab for that realm, and - on platforms that require domain membership to obtain a credential - a domain join for the host under test. All identifiers SHALL be fictitious and bound to no real organisation.

#### Scenario: Realm and proxy principal provisioned

- **WHEN** the provisioning assets are applied
- **THEN** a realm, a test user, and an `HTTP/<proxy-host>` service principal with an exported keytab exist, using fictitious identifiers reserved for documentation and testing

#### Scenario: Proxy advertises Negotiate and Basic

- **WHEN** the proxy is brought up against the provisioned realm
- **THEN** it advertises both Negotiate (backed by the keytab) and Basic, so preference and fall-through can be exercised

### Requirement: Excluded from per-push CI, run on demand

The integration tests SHALL NOT run on the per-push continuous-integration matrix. Where a platform provides a dedicated CI job for its integration test, that job SHALL be triggered on demand or by a release event, never by an ordinary push.

#### Scenario: Not on the per-push matrix

- **WHEN** a commit is pushed and the standard CI workflow runs
- **THEN** no Kerberos integration test is executed by that workflow

#### Scenario: Runs on demand where a dedicated job exists

- **WHEN** a platform's dedicated integration workflow is triggered manually or by a release event
- **THEN** it provisions the environment and runs that platform's integration test
