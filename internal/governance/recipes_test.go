package governance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func recipesFixture(t *testing.T, justfile, workflow string) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "governance.json", `{"schema_version":1,"kind":"go","commands":{"check":{"steps":[{"argv":["@ledger-tool","security"]}]},"security":{"steps":[{"argv":["go","vet","./..."]}]},"fuzz":{"steps":[{"argv":["go","test","./..."]}]}}}`)
	put(t, root, "justfile", justfile)
	if workflow != "" {
		put(t, root, ".github/workflows/ci.yml", workflow)
	}
	return root
}

func loadPolicy(t *testing.T, root string) Config {
	t.Helper()
	c, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestRecipesCheck(t *testing.T) {
	complete := "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n"
	for _, tc := range []struct {
		name     string
		justfile string
		workflow string
		wantErr  string
	}{
		{name: "aligned", justfile: complete, workflow: "run: go run ./tool/bootstrap.go check\n"},
		{name: "official workflow forms", justfile: complete, workflow: "run: go run ./cmd/ledger-tool --root . check\nrun: go run ./cmd/ledger-tool security\n"},
		{name: "unknown command in recipe", justfile: "check:\n    go run ./tool/bootstrap.go check\n\ndeploy:\n    go run ./tool/bootstrap.go deploy-production\n", wantErr: "invokes unknown command"},
		{name: "unreachable configured command", justfile: "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n", wantErr: "fuzz is not reachable"},
		{name: "ci command without recipe", justfile: "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n", workflow: "run: go run ./tool/bootstrap.go mutation-check --report x\n", wantErr: "CI runs mutation-check"},
		{name: "dependency chain counts", justfile: "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n\nquality: security\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n", workflow: ""},
		{name: "defaults with separators", justfile: "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n\nreview-check file=\".governance/review.json\":\n    go run ./tool/bootstrap.go review-check --file {{file}}\n", workflow: "run: go run ./tool/bootstrap.go review-check --file .governance/review.json\n"},
		{name: "delegated command counts", justfile: "check:\n    go run ./tool/bootstrap.go check\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n", workflow: ""},
		{name: "missing justfile", justfile: "", workflow: "", wantErr: "justfile is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := recipesFixture(t, tc.justfile, tc.workflow)
			if tc.justfile == "" {
				if e := os.Remove(filepath.Join(root, "justfile")); e != nil {
					t.Fatal(e)
				}
			}
			e := RecipesCheck(root, loadPolicy(t, root))
			if tc.wantErr == "" {
				if e != nil {
					t.Fatal(e)
				}
				return
			}
			if e == nil || !strings.Contains(e.Error(), tc.wantErr) {
				t.Fatalf("want %q, got %v", tc.wantErr, e)
			}
		})
	}
}

func TestParseRecipeHeader(t *testing.T) {
	for _, tc := range []struct {
		line string
		name string
		deps []string
		ok   bool
	}{
		{line: "check:", name: "check", ok: true},
		{line: "review-check file=\".governance/review.json\":", name: "review-check", ok: true},
		{line: "deploy url=\"https://example.test/a\" note='a:b':", name: "deploy", ok: true},
		{line: "quality: security fuzz # trailing comment", name: "quality", deps: []string{"security", "fuzz"}, ok: true},
		{line: "value := \"not a recipe\"", ok: false},
		{line: "set shell := [\"sh\", \"-cu\"]", ok: false},
		{line: "this is not a recipe", ok: false},
		{line: ": deps-only", ok: false},
		{line: "1invalid: target", ok: false},
	} {
		t.Run(tc.line, func(t *testing.T) {
			name, deps, ok := parseRecipeHeader(tc.line)
			if ok != tc.ok || name != tc.name || strings.Join(deps, ",") != strings.Join(tc.deps, ",") {
				t.Fatalf("got %q %v %t", name, deps, ok)
			}
		})
	}
}

func TestReadJustfileSkipsConfiguration(t *testing.T) {
	root := t.TempDir()
	put(t, root, "justfile", `set shell := ["sh", "-cu"]
export GOTOOLCHAIN := "go1.26.6"

# a comment
    indented before any recipe

this line is not a recipe

check:
    go run ./tool/bootstrap.go check

# recipe names that merely start with a configuration keyword
export-currencies app:
    go run ./tool/bootstrap.go currency --app {{app}}

mod-check:
    go run ./tool/bootstrap.go mod-check

shell-check:
    go run ./tool/bootstrap.go shell-check

alias c := check
`)
	sheet, e := readJustfile(filepath.Join(root, "justfile"))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"check", "export-currencies", "mod-check", "shell-check"} {
		if len(sheet.commands[name]) != 1 {
			t.Fatalf("recipe %s not parsed: %v / %v", name, sheet.deps, sheet.commands)
		}
	}
	if len(sheet.commands) != 4 {
		t.Fatalf("parsed %d recipes, want 4: %v", len(sheet.commands), sheet.deps)
	}
}

func TestCICommandsSources(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".github/workflows/ci.yaml", "run: go run ./tool/bootstrap.go check\n")
	put(t, root, ".github/workflows/notes.txt", "run: go run ./tool/bootstrap.go ignored\n")
	put(t, root, ".github/workflows/nested/ci.yml", "run: go run ./tool/bootstrap.go also-ignored\n")
	commands, e := ciCommands(filepath.Join(root, ".github", "workflows"))
	if e != nil {
		t.Fatal(e)
	}
	if len(commands) != 1 || !commands["check"] {
		t.Fatalf("workflow commands %v", commands)
	}
	missing, e := ciCommands(filepath.Join(root, "absent"))
	if e != nil || len(missing) != 0 {
		t.Fatalf("%v %v", missing, e)
	}
}

func TestRecipesCheckReportsUnreadableWorkflows(t *testing.T) {
	root := recipesFixture(t, "check:\n    go run ./tool/bootstrap.go check\n\nsecurity:\n    go run ./tool/bootstrap.go security\n\nfuzz:\n    go run ./tool/bootstrap.go fuzz\n", "")
	workflows := filepath.Join(root, ".github", "workflows")
	// recipesFixture / earlier puts may leave workflows as a directory; replace
	// it with a file so RecipesCheck must reject a non-directory path.
	if e := os.RemoveAll(workflows); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Dir(workflows), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(workflows, []byte("not a directory\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := RecipesCheck(root, loadPolicy(t, root)); e == nil {
		t.Fatal("unreadable workflow directory accepted")
	}
}

func TestCICommandsReportsUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workflows")
	if e := os.Mkdir(dir, 0755); e != nil {
		t.Fatal(e)
	}
	// Unix: deny directory search bits. Windows: inject a ReadDir failure —
	// chmod does not block directory reads the same way, and the coverage
	// gate still requires this error path on every native runner.
	if runtime.GOOS == "windows" {
		orig := readDir
		readDir = func(string) ([]os.DirEntry, error) {
			return nil, errors.New("forced read dir failure")
		}
		t.Cleanup(func() { readDir = orig })
	} else {
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
		if e := os.Chmod(dir, 0); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ciCommands(dir); e == nil {
		t.Fatal("unreadable workflow directory accepted")
	}
}
