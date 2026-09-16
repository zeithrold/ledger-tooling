# Ledger tooling engineering rules

- Keep source, documentation and commit messages in English. Preserve unrelated changes.
- Keep this repository a small Go CLI that composes existing tools. Do not add a second language runtime, shell framework, universal debugger or hidden network service.
- Treat `governance.json` and evidence JSON as versioned public interfaces. Reject malformed or unsupported inputs, missing evidence, stale fingerprints and out-of-root paths. Never turn infrastructure failures into passes.
- Use argument arrays and Go process/filesystem APIs. Keep Unix and Windows process handling behind platform files; validate on actual OS runners. Cross-compilation alone is not native Windows validation.
- Keep tests deterministic and credential-free. Use temporary Git repositories and local subprocess fixtures for coverage/diff, path containment, timeout, review freshness, red/green sessions and UI report tests.
- Do not copy `.env*`, Git metadata, raw secrets or unrelated source into isolated tool workspaces. Record only the minimum evidence needed; do not automatically upload debug data.
- Maintain 70% overall and 90% incremental coverage, with only measured expiring migration baselines. Never exclude handwritten business logic or tests of the gate itself to inflate scores.
- Every code change receives final-diff review by an independent Subagent. Use `templates/skills/ledger-architecture-review.md`; do not self-assert independent approval. Reports are tracked at `.governance/review.json` and checked by CI.
- Changes to CLI/config/evidence behavior require compatibility notes and meaningful negative tests. Consumer bundle updates must include both producer and consumer checks. Report local, native-OS and remote results separately.
