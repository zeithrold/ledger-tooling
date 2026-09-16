# Ledger tooling

Shared, versioned Go tooling for Ledger's backend, Flutter app and future web client. `just` provides discoverable recipes; `ledger-tool` executes repository-specific argument arrays and produces comparable quality evidence without Python or a shell-script runtime.

## Development

Use Go 1.26.6 (declared in `go.mod`), Git and optional `just`. With the standard
`GOTOOLCHAIN=auto` setting, Go selects/downloads the required project toolchain;
this does not replace the machine-wide Go installation. Each repository's
`justfile` exports `GOTOOLCHAIN=go1.26.6` only for its commands, including SDK
probes. Cold toolchain/module
caches require network access. A deliberately offline environment must provision
the exact toolchain first.

```sh
go test ./...
go vet ./...
go run ./cmd/ledger-tool --root . check
```

Consumers use `go run ./tool/bootstrap.go <command>` and a pinned source bundle with a verified digest. The bundle makes a clean checkout usable before this repository has a published release. Updating the bundle is a reviewed dependency update: change source, run tests, rebuild the bundle, update its pin and validate every consumer. Do not fetch a moving branch at runtime.

The tool does not replace Flutter/Dart, Go, Docker or platform SDKs. macOS, Linux and Windows run the same Go CLI; iOS device builds still require macOS/Xcode, and native app tests still require the relevant device/runtime. The native OS matrix in CI verifies tool execution, not every mobile build target.

## Commands

| Command | Purpose |
| --- | --- |
| `doctor` | Inspect tools required by the repository |
| `check` | Execute the repository's declared quality steps |
| `policy-check --base REF` | Reject coverage-policy weakening against the target revision |
| `version-check --pattern REGEX -- COMMAND ...` | Execute a capability probe and require matching version output |
| `changes --base REF --json` | Explain affected suites; `--github-output PATH` writes CI outputs |
| `coverage-check --base REF` | Enforce overall and incremental floors using the configured profile |
| `fingerprint` | Identify the source snapshot used by evidence |
| `review-check --file .governance/review.json` | Reject missing, stale or blocking independent-review evidence |
| `commit-check 'fix(scope): summary'` | Validate a Conventional Commit/PR title |
| `generate-check --output PATH -- COMMAND ...` | Regenerate in an isolated copy and compare output |
| `isolated-run --include PATH -- COMMAND ...` | Run a tool against explicit copied inputs, without developer config |
| `currency --check` | Verify backend currency source hashes and generated artifacts |
| `currency --app-check` | Verify an exported app pack without requiring a backend checkout |
| `debug-start CASE` | Create a local evidence session |
| `debug-run SESSION PROFILE` | Run the configured reproduction profile |
| `debug-verify SESSION PROFILE` | Require a prior failing run on different source and run the same profile |
| `debug-report SESSION` | Render the recorded result and limitations |
| `ui-report --manifest PATH` | Render PNG before/after/difference evidence with environment metadata |
| `bundle CHECKOUT ...` | Generate deterministic source bundles, launcher files and digest locks in explicit consumer checkouts |

Named repository commands use the same CLI. Extra arguments are appended to the last declared step, so do not pass secrets as arguments or assume a shell interprets them. See [configuration and evidence](docs/contracts.md), [contributing](CONTRIBUTING.md) and the reusable [skill templates](templates/skills/).

Reports prove the checks they actually ran. A source fingerprint is not reviewer identity authentication, a successful test process does not prove a reported bug was reproduced, and a screenshot difference is not visual approval.

`security` uses pinned Gitleaks v8.30.1 on an isolated source copy and govulncheck v1.8.0 against the current official Go vulnerability database. Findings are fully redacted; detected reachable vulnerabilities fail the Go dependency gate. Network failure is a blocked check, never a clean scan. Flutter's dependency adapter produces an advisory `flutter pub outdated --json` report; it is not Go vulnerability coverage of Dart packages and does not force all dependencies to their latest release.

To update consumers after testing the tooling source, run `go run ./cmd/ledger-tool bundle /absolute/consumer/path` for each checkout. The command creates a deterministic source archive, SHA-256 lock and generated bootstrap; review and commit all three together. No remote publication or sibling checkout is needed by a consumer. `just` recipes use typed, shell-quoted arguments; use the Go CLI for arbitrary flag arrays.
