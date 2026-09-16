# Contributing

Use one reviewable behavior change per commit with its tests and interface documentation. Commit and PR titles follow `type(scope)!: imperative summary`; accepted types are `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `build`, `ci`, `chore` and `revert`. Explain incompatible interface changes with a `BREAKING CHANGE:` footer and a consumer migration. Keep subjects at most 100 characters.

Run tests, vet, the coverage gate and the final independent review. PRs state the original failure, new behavior, executed commands, report paths and unverified platforms. Preserve unrelated working changes and stage explicit paths. Do not commit raw debug data, tokens, machine paths or build outputs.

## Consumer release

1. Review and test the tool change, including malformed inputs and failure paths.
2. Run `go run ./cmd/ledger-tool bundle <backend-checkout> <app-checkout>` to produce the deterministic source bundle, generated launcher and immutable digest lock in the named consumers. Inspect those changes together.
3. Update each consumer pin and bundle together; run the consumer's normal gate and affected expensive checks.
4. Publish a release only when requested, then verify its contents and digest. Until a remote release exists, the committed bundle remains the reproducible source of the consumer tool.

## Repository-host setup

After a remote repository is created and authenticated, enable branch protection/rulesets on the default branch: require PRs, prevent force pushes/deletion, require the native matrix and governance checks, and require independent review. Configure action artifact retention appropriate for synthetic test evidence. These settings cannot be established by committing workflow YAML; record them as blocked until actually applied and verified.

For consumers, require their source gate, affected integration/fuzz/mutation checks and independent-review check. If conditional jobs are used, require a stable aggregate status that distinguishes valid not-applicable cases from failures. Do not claim a local run proves hosted CI or branch protection.
