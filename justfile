# All recipes run the Go CLI under the repository-pinned toolchain.
set windows-shell := ["powershell.exe", "-NoLogo", "-NoProfile", "-Command"]
export GOTOOLCHAIN := "go1.26.6"

default:
    @just --list

check:
    go run ./cmd/ledger-tool check

test:
    go run ./cmd/ledger-tool test

vet:
    go run ./cmd/ledger-tool vet

format-check:
    go run ./cmd/ledger-tool format-check

coverage-check base="":
    go run ./cmd/ledger-tool coverage-check {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

policy-check base="":
    go run ./cmd/ledger-tool policy-check {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

security:
    go run ./cmd/ledger-tool security

doctor:
    go run ./cmd/ledger-tool doctor

changes base="":
    go run ./cmd/ledger-tool changes {{if base == "" { "" } else { "--base " + if os() == "windows" { "'" + replace(base, "'", "''") + "'" } else { quote(base) } }}}

review-check file=".governance/review.json":
    go run ./cmd/ledger-tool review-check --file {{if os() == "windows" { "'" + replace(file, "'", "''") + "'" } else { quote(file) }}}

bundle checkout:
    go run ./cmd/ledger-tool bundle {{if os() == "windows" { "'" + replace(checkout, "'", "''") + "'" } else { quote(checkout) }}}
