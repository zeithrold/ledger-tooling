package governance

import (
	"strings"
	"testing"
)

// gatedPolicy mirrors how a repository declares its expensive gates: the
// classifier reads the packages and isolated-copy includes the policy names.
func gatedPolicy(kind string) Config {
	return Config{SchemaVersion: 1, Kind: kind, Commands: map[string]Command{
		"fuzz": {Steps: []Step{
			{Argv: []string{"go", "test", "./internal/config", "-run=^$", "-fuzz=^FuzzGinMode$", "-fuzztime=30s"}},
			{Argv: []string{"go", "test", "./internal/money", "-run=^$", "-fuzz=^FuzzBalance$", "-fuzztime=30s"}},
		}},
		"mutation-accounting": {Steps: []Step{
			{Argv: []string{"@ledger-tool", "isolated-run", "--include", "internal/money", "--", "go", "run", "example.test/gremlins@v0.6.0", "unleash", "./internal/money"}},
		}},
		"build": {Steps: []Step{{Argv: []string{"go", "build", "./..."}}}},
	}}
}

func TestClassificationFollowsPolicyPaths(t *testing.T) {
	for _, tc := range []struct {
		name         string
		kind         string
		path         string
		wantMutation bool
		wantFuzz     bool
		wantCode     bool
		wantVisual   bool
	}{
		{name: "renamed file inside the mutation target", kind: "go", path: "internal/money/renamed.go", wantMutation: true, wantFuzz: true, wantCode: true},
		{name: "unrelated name inside the fuzz target", kind: "go", path: "internal/config/anything.go", wantFuzz: true, wantCode: true},
		{name: "money keyword outside any gate", kind: "go", path: "lib/money_view.dart", wantCode: true},
		{name: "accounting helper is not a mutation target", kind: "go", path: "internal/accounting/money_helpers.go", wantCode: true},
		{name: "production UI still counts as visual", kind: "flutter", path: "lib/features/accounts/money_row.dart", wantCode: true, wantVisual: true},
		{name: "documentation under assets is not UI evidence", kind: "flutter", path: "assets/reference/README.md", wantCode: false},
		{name: "documentation under a UI tree is not UI evidence", kind: "flutter", path: "lib/features/accounts/notes.md", wantCode: false},
		{name: "native documentation is not a native change", kind: "flutter", path: "android/README.txt", wantCode: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := repo(t)
			put(t, r, tc.path, "package placeholder\n")
			c, e := Classify(r, "", gatedPolicy(tc.kind))
			if e != nil {
				t.Fatal(e)
			}
			if c.Mutation != tc.wantMutation || c.Fuzz != tc.wantFuzz || c.Code != tc.wantCode || c.Visual != tc.wantVisual {
				t.Fatalf("classification %+v for %s", c, tc.path)
			}
		})
	}
	r := repo(t)
	put(t, r, "internal/money/value.go", "package money\n")
	c, e := Classify(r, "", Config{Kind: "go", Commands: map[string]Command{
		"fuzz": {Steps: []Step{{Argv: []string{"go", "test", "-run=^$"}}}},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if !c.Fuzz {
		t.Fatal("a configured gate with no extractable path must be treated as affected")
	}
	if c.Mutation {
		t.Fatal("an absent gate must not be triggered")
	}
}

// TestClassificationNeverSilentlySkipsAGate covers the target shapes that a
// path-derived classifier must not mistake for "affects nothing": recursive
// patterns, hyphenated directories, inline includes, gates mixed with
// path-less steps, and a file moved out of a gated package.
func TestClassificationNeverSilentlySkipsAGate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy Config
		path   string
	}{
		{name: "recursive root pattern", policy: gatePolicy("fuzz", []string{"go", "test", "./...", "-fuzz=^F$"}), path: "internal/anything.go"},
		{name: "recursive package pattern", policy: gatePolicy("fuzz", []string{"go", "test", "./internal/money/...", "-fuzz=^F$"}), path: "internal/money/deep/value.go"},
		{name: "hyphenated directory", policy: gatePolicy("fuzz", []string{"go", "test", "./internal/money-rounding"}), path: "internal/money-rounding/value.go"},
		{name: "inline include", policy: gatePolicy("mutation-accounting", []string{"isolated-run", "--include=internal/money", "--", "gremlins"}), path: "internal/money/value.go"},
		{name: "path-less step beside a target", policy: Config{Kind: "go", Commands: map[string]Command{"fuzz": {Steps: []Step{{Argv: []string{"go", "build", "."}}, {Argv: []string{"go", "test", "-run=^$"}}}}}}, path: "internal/money/value.go"},
		{name: "current directory include", policy: gatePolicy("mutation-accounting", []string{"isolated-run", "--include", "./", "--", "gremlins"}), path: "cmd/api/main.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := repo(t)
			put(t, r, tc.path, "package placeholder\n")
			c, e := Classify(r, "", tc.policy)
			if e != nil {
				t.Fatal(e)
			}
			if !c.Fuzz && !c.Mutation {
				t.Fatalf("gate silently skipped for %s: %+v", tc.path, c)
			}
		})
	}
	r := repo(t)
	put(t, r, "internal/money/value.go", "package money\n")
	gitOK(t, r, "add", ".")
	gitOK(t, r, "commit", "-qm", "feat(money): Add a gated fixture")
	gitOK(t, r, "mv", "internal/money/value.go", "internal/moved.go")
	c, e := Classify(r, "", gatedPolicy("go"))
	if e != nil {
		t.Fatal(e)
	}
	if !c.Fuzz || !c.Mutation || !c.Code {
		t.Fatalf("rename out of a gated area hid the gate: %+v", c)
	}
	if len(c.Files) != 2 {
		t.Fatalf("expected both sides of the rename to be reported: %v", c.Files)
	}
}

func TestCommandPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		argv    []string
		paths   []string
		whole   bool
		unknown bool
	}{
		{name: "package arguments", argv: []string{"go", "test", "./internal/money", "-run=^$"}, paths: []string{"internal/money"}},
		{name: "recursive package", argv: []string{"go", "test", "./internal/money/..."}, paths: []string{"internal/money"}},
		{name: "repository wide", argv: []string{"go", "test", "./..."}, whole: true},
		{name: "include flag", argv: []string{"isolated-run", "--include", "internal/money", "--", "gremlins"}, paths: []string{"internal/money"}},
		{name: "inline include", argv: []string{"isolated-run", "--include=internal/money", "--", "gremlins"}, paths: []string{"internal/money"}},
		{name: "hyphenated path", argv: []string{"go", "test", "./internal/money-rounding"}, paths: []string{"internal/money-rounding"}},
		{name: "no target", argv: []string{"go", "vet", "-n"}, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths, whole, unknown := commandPaths(Command{Steps: []Step{{Argv: tc.argv}}})
			if whole != tc.whole || unknown != tc.unknown || strings.Join(paths, ",") != strings.Join(tc.paths, ",") {
				t.Fatalf("paths=%v whole=%t unknown=%t", paths, whole, unknown)
			}
		})
	}
}

func gatePolicy(name string, argv []string) Config {
	return Config{SchemaVersion: 1, Kind: "go", Commands: map[string]Command{name: {Steps: []Step{{Argv: argv}}}}}
}
