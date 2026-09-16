# All recipes run the Go CLI under the repository-pinned toolchain.
set windows-shell := ["powershell.exe", "-NoLogo", "-NoProfile", "-Command"]
export GOTOOLCHAIN := "go1.26.6"

[private]
default:
    @just --list

check:
    go run ./cmd/ledger-tool check

[private]
recipes-check:
    go run ./cmd/ledger-tool recipes-check

[private]
build:
    go run ./cmd/ledger-tool build

[private]
security-secrets:
    go run ./cmd/ledger-tool security-secrets

[private]
security-dependencies:
    go run ./cmd/ledger-tool security-dependencies

[private]
commit-check title:
    go run ./cmd/ledger-tool commit-check {{if os() == "windows" { "'" + replace(title, "'", "''") + "'" } else { quote(title) }}}

test:
    go run ./cmd/ledger-tool test

[private]
vet:
    go run ./cmd/ledger-tool vet

format-check:
    go run ./cmd/ledger-tool format-check

[private]
coverage-check base="":
    go run ./cmd/ledger-tool coverage-check {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

[private]
policy-check base="":
    go run ./cmd/ledger-tool policy-check {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

[private]
security:
    go run ./cmd/ledger-tool security

[private]
doctor:
    go run ./cmd/ledger-tool doctor

[private]
changes base="":
    go run ./cmd/ledger-tool changes {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

[private]
review-check file=".governance/review.json":
    go run ./cmd/ledger-tool review-check --file {{if os() == "windows" { "'" + replace(file, "'", "''") + "'" } else { quote(file) }}}

[private]
bundle checkout:
    go run ./cmd/ledger-tool bundle {{if os() == "windows" { "'" + replace(checkout, "'", "''") + "'" } else { quote(checkout) }}}
