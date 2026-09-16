package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOption(t *testing.T) {
	v, a := option([]string{"--base", "main", "x"}, "--base")
	if v != "main" || len(a) != 1 || a[0] != "x" {
		t.Fatal(v, a)
	}
	v, a = option([]string{"x"}, "--base")
	if v != "" || len(a) != 1 {
		t.Fatal(v, a)
	}
	v, a = option([]string{"--", "tool", "--output", "report"}, "--output")
	if v != "" || len(a) != 4 {
		t.Fatal("consumed child argument", v, a)
	}
}
func TestDispatch(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "test@example.test"}, {"config", "user.name", "Test"}} {
		c := exec.Command("git", args...)
		c.Dir = root
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatal(e, string(b))
		}
	}
	write := func(p, s string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755)
		if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("governance.json", `{"schema_version":1,"kind":"go","commands":{"custom":{"steps":[{"argv":["go","version"]}]}},"tools":{"go":["go","version"]}}`)
	write(".gitignore", "build/\n")
	c := exec.Command("git", "add", ".")
	c.Dir = root
	if e := c.Run(); e != nil {
		t.Fatal(e)
	}
	c = exec.Command("git", "commit", "-m", "initial")
	c.Dir = root
	if e := c.Run(); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		args []string
		fail bool
	}{
		{[]string{"version"}, false}, {[]string{"fingerprint"}, false}, {[]string{"doctor"}, false}, {[]string{"custom"}, false}, {[]string{"changes", "--json"}, true}, {[]string{"changes", "--github-output", filepath.Join(root, "build", "out")}, false}, {[]string{"skills-check"}, true}, {[]string{"commit-check", "fix(tool): validate commands"}, false}, {[]string{"commit-check", "invalid"}, true}, {[]string{"coverage-check"}, true}, {[]string{"review-check"}, true}, {[]string{"generate-check"}, true}, {[]string{"isolated-run"}, true}, {[]string{"debug-start"}, true}, {[]string{"debug-start", "sample"}, false}, {[]string{"debug-run"}, true}, {[]string{"debug-run", "missing", "unit"}, true}, {[]string{"debug-verify", "missing", "unit"}, true}, {[]string{"debug-report"}, true}, {[]string{"debug-report", "missing"}, true}, {[]string{"ui-report"}, true}, {[]string{"mutation-check", "--report", "missing.json"}, true}, {[]string{"unknown"}, true}, {[]string{"currency", "--app-check"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.args[0], func(t *testing.T) {
			e := run(context.Background(), append([]string{"--root", root}, tc.args...))
			if (e != nil) != tc.fail {
				t.Fatalf("%v: %v", tc.args, e)
			}
		})
	}
	for _, args := range [][]string{{"policy-check"}, {"version-check", "--pattern", "^go version", "--", "go", "version"}, {"isolated-run", "--include", "governance.json", "--", "go", "version"}, {"generate-check", "--output", "governance.json", "--", "go", "version"}} {
		if e := run(context.Background(), append([]string{"--root", root}, args...)); e != nil {
			t.Fatal(args, e)
		}
	}
	if e := run(context.Background(), []string{"--root", root, "bundle"}); e == nil {
		t.Fatal("bundle without destinations")
	}
	t.Setenv("LEDGER_BASE", "HEAD")
	if e := run(context.Background(), []string{"--root", root, "policy-check"}); e != nil {
		t.Fatal(e)
	}
	t.Setenv("LEDGER_BASE", "")
	t.Setenv("LEDGER_BASE_REF", "HEAD")
	if e := run(context.Background(), []string{"--root", root, "policy-check"}); e != nil {
		t.Fatal(e)
	}
	write("governance.json", `{"schema_version":1,"kind":"go","commands":{"doctor":{"steps":[{"argv":["go","version"]}]}}}`)
	if e := run(context.Background(), []string{"--root", root, "doctor"}); e != nil {
		t.Fatal(e)
	}
}
func TestRunInvalid(t *testing.T) {
	if run(context.Background(), nil) == nil {
		t.Fatal("expected usage")
	}
	if run(context.Background(), []string{"--root", t.TempDir(), "check"}) == nil {
		t.Fatal("missing config")
	}
}
