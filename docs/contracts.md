# Configuration and evidence contracts

All paths in JSON are repository-relative slash paths unless an argument explicitly accepts an external checkout. A schema version change is reviewed as an interface change. Do not put credentials or machine-specific paths in these files.

## `governance.json`

```json
{
  "schema_version": 1,
  "kind": "go",
  "commands": {
    "test": {
      "description": "Deterministic test suite",
      "steps": [{
        "argv": ["go", "test", "./..."],
        "dir": "",
        "env": {},
        "timeout_seconds": 300
      }]
    }
  },
  "coverage": {
    "format": "go",
    "profiles": ["coverage/unit.out"],
    "source_roots": ["cmd", "internal"],
    "exclude": [],
    "minimum": 70,
    "incremental_minimum": 90
  },
  "debug_profiles": {
    "unit": {"steps": [{"argv": ["go", "test", "-json", "-count=1", "./..."]}]}
  }
}
```

Use `kind: "flutter"` and `format: "lcov"` for Flutter; the future web consumer chooses its adapter when created. `@ledger-tool` as the first argv item dispatches another built-in or configured command. `{root}` expands to the repository root in arguments, environment values and working directory. Extra CLI arguments append to the last step. Commands execute without shell expansion, pipes, redirection or command substitution.

Coverage inventory must include eligible production files even when never executed. Dart LCOV uses an explicit executable-line inventory when provided by its coverage producer. Generated exclusions are reviewed and reported. A temporary `coverage.baseline` contains `minimum`, ISO `started`/`expires` and a concrete `reason`; record the actual measurement and remediation owner in reviewed evidence. Its duration is at most 30 days and it may not replace the 90% changed-code gate. The initial migration expiry is no later than 2026-10-16.

`policy-check` compares the current policy with the target Git revision. Overall/incremental floors and baseline floors may not decrease, baseline expiry may not extend, exclusions may not be added, and source roots may not shrink. A first repository policy has no prior ratchet. Changes requiring a new generated exclusion need an explicit policy migration; do not silently bypass this check. Use the same explicit target base as the coverage gate for committed feature-branch review.

`version-check --pattern REGEX -- COMMAND ...` runs the actual command, requires a zero exit status and matching stdout/stderr, and rejects empty-match regexes. It has a 30-second deadline and a one-MiB output bound. A version entry is evidence of installed capability, not an installer or authorization to upgrade the global environment.

## Review and change evidence

The review schema is in the [review skill template](../templates/skills/ledger-architecture-review.md). `.governance/review.json` is tracked so CI can validate it. It is excluded from its own fingerprint. All P0/P1 findings must be resolved, and the final source must match. Identity and independence remain assertions requiring a trusted reviewer; the tool cannot authenticate them from a JSON file.

`changes` returns `code`, `backend`, `ui`, `visual`, `policy`, `mutation`, `fuzz`, `native`, `docs_only` booleans and a `files` array. `ui` also triggers native suites for policy changes; `visual` identifies production UI changes requiring before/after evidence. Local calls without a base compare uncommitted/untracked work with HEAD; they do not review already committed feature-branch work. Use `--base <target-ref>` for that branch, and set `LEDGER_BASE` for the coverage step inside `check`. CI provides the PR base SHA. Initial commits use all source as changed rather than silently skipping. Governance and shared-foundation changes broaden verification. Missing eligible coverage evidence is an error, not zero changed code.

## Debug session

`debug-start CASE` writes `build/debug/SESSION/session.json`. Fill `expected`, `actual`, nonempty `hypotheses` and a `failure_pattern` regular expression before running a named profile; keep active temporary probe locations in `probes`. The pattern identifies the intended failed assertion, never a generic error marker. `debug-run SESSION PROFILE` records the result and source fingerprint and requires the pattern in actual failed output to establish reproduction. `debug-verify` requires that reproduction of the same profile on a different source. It records `verified-profile` only after that profile passes and probes have been removed. The reviewer still checks fixture equivalence and that the pattern is discriminating. Raw logs are not silently persisted or uploaded. `debug-report` marks evidence stale if source changes.

## UI manifest

`ui-report --manifest build/ui-captures/manifest.json` accepts:

```json
{
  "schema_version": 1,
  "base": "base revision",
  "head": "tested revision",
  "fingerprint": "current source fingerprint",
  "captures": [{
    "scenario": "transaction details expanded actions",
    "device": "named simulator or physical device",
    "runtime": "OS and Flutter/browser version",
    "locale": "en",
    "theme": "light",
    "text_scale": 2,
    "before": "build/ui-captures/before.png",
    "after": "build/ui-captures/after.png",
    "assertions_passed": true
  }]
}
```

The tool requires PNGs of equal dimensions and produces an HTML gallery plus difference images in `build/ui-report/`. Capture actual before/after states under the same environment; do not create synthetic screenshots to satisfy the manifest. The assertion flag is evidence metadata, not independently authenticated execution. Inspect the gallery before accepting a visual change.
