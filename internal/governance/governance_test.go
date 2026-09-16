package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, path, text string) {
	t.Helper()
	full := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(full), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(full, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func gitOK(t *testing.T, root string, args ...string) string {
	t.Helper()
	s, e := git(root, args...)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func repo(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	gitOK(t, r, "init", "-q")
	gitOK(t, r, "config", "user.name", "Fixture")
	gitOK(t, r, "config", "user.email", "fixture@example.test")
	put(t, r, ".gitignore", "build/\ncoverage/\n.governance/\n")
	put(t, r, "go.mod", "module example.test/project\n\ngo 1.26.2\n")
	put(t, r, "internal/core/core.go", "package core\nfunc Value() int {\n return 1\n}\n")
	gitOK(t, r, "add", ".")
	gitOK(t, r, "commit", "-qm", "feat(core): Initialize fixture")
	return r
}
func TestConfigurationAndPaths(t *testing.T) {
	r := repo(t)
	if _, e := Load(r); e == nil {
		t.Fatal("missing config passed")
	}
	put(t, r, "governance.json", "{")
	if _, e := Load(r); e == nil {
		t.Fatal("invalid JSON passed")
	}
	put(t, r, "governance.json", `{"schema_version":2}`)
	if _, e := Load(r); e == nil {
		t.Fatal("unknown schema passed")
	}
	put(t, r, "governance.json", `{"schema_version":1,"kind":"go"}`)
	c, e := Load(r)
	if e != nil || c.Kind != "go" {
		t.Fatal(c, e)
	}
	for _, p := range []string{"", "../outside", "/absolute", "a\\b"} {
		if _, e := safePath(r, p); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if _, e := safePath(r, "new/nested/file"); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(r, "escape")); e == nil {
		if _, e = safePath(r, "escape/secret"); e == nil {
			t.Fatal("symlink escaped")
		}
	}
	if e := WriteJSON(filepath.Join(r, "build/a.json"), objectForTest{Value: "<tag>"}); e != nil {
		t.Fatal(e)
	}
	if e := WriteJSON(filepath.Join(r, "build/b.json"), make(chan int)); e == nil {
		t.Fatal("encoded channel")
	}
}

type objectForTest struct{ Value string }

func TestGitChangesFingerprintAndClassification(t *testing.T) {
	r := repo(t)
	fp, e := Fingerprint(r)
	if e != nil {
		t.Fatal(e)
	}
	put(t, r, "internal/core/core.go", "package core\nfunc Value() int {\n return 2\n}\n")
	put(t, r, "lib/new file.dart", "first\nsecond\n")
	lines, e := ChangedLines(r, "")
	if e != nil {
		t.Fatal(e)
	}
	if !lines["internal/core/core.go"][3] || !lines["lib/new file.dart"][1] {
		t.Fatalf("unexpected lines: %#v", lines)
	}
	next, e := Fingerprint(r)
	if e != nil || next == fp {
		t.Fatal("fingerprint stale", e)
	}
	c, e := Classify(r, "", "flutter")
	if e != nil || !c.Code || !c.UI || c.DocsOnly {
		t.Fatal(c, e)
	}
	put(t, r, "governance.json", "{}")
	c, e = Classify(r, "", "flutter")
	if e != nil || !c.Policy || !c.Native || !c.Fuzz || !c.Mutation {
		t.Fatal(c, e)
	}
	if _, e = Base(r, "not-a-ref"); e == nil {
		t.Fatal("invalid base passed")
	}
	if b, e := Base(r, "HEAD"); e != nil || b == "" {
		t.Fatal(b, e)
	}
	if e = os.Remove(filepath.Join(r, "internal/core/core.go")); e != nil {
		t.Fatal(e)
	}
	if _, e = Fingerprint(r); e != nil {
		t.Fatal(e)
	}
	docs := repo(t)
	put(t, docs, "README.md", "documentation")
	c, e = Classify(docs, "", "go")
	if e != nil || !c.DocsOnly || c.Code {
		t.Fatal(c, e)
	}
	money := repo(t)
	put(t, money, "internal/money/value.go", "package money")
	c, e = Classify(money, "", "go")
	if e != nil || !c.Backend || !c.Mutation || !c.Fuzz {
		t.Fatal(c, e)
	}
	out := filepath.Join(r, "build/outputs")
	if e = os.MkdirAll(filepath.Dir(out), 0755); e != nil {
		t.Fatal(e)
	}
	if e = WriteOutput(out, c); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(out)
	if e != nil || !strings.Contains(string(data), "mutation=true") {
		t.Fatal(string(data), e)
	}
}
func TestCommitTitles(t *testing.T) {
	for _, s := range []string{"fix(auth): Refresh expired sessions", "feat(api)!: Revise contract", "docs: Explain gates"} {
		if e := CommitCheck(s); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"fix something", "feat(UI): Invalid scope", "fix: 中文说明", "fix: ", "fix: x\nother", strings.Repeat("x", 101)} {
		if e := CommitCheck(s); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if GitVersion(context.Background(), t.TempDir()) != "uncommitted" {
		t.Fatal("expected uncommitted")
	}
}
func goCoverage() CoverageConfig {
	return CoverageConfig{Format: "go", Profiles: []string{"coverage/unit.out"}, SourceRoots: []string{"internal"}, Minimum: 70, IncrementalMinimum: 90}
}
func TestCoverageMergingAndIncremental(t *testing.T) {
	r := repo(t)
	put(t, r, "coverage/unit.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 0\n")
	put(t, r, "coverage/integration.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 2\n")
	c := goCoverage()
	c.Profiles = append(c.Profiles, "coverage/integration.out")
	report, e := Coverage(r, c, "", time.Now())
	if e != nil || report.Total != 1 || report.Covered != 1 || report.IncrementalPercent != nil {
		t.Fatal(report, e)
	}
	put(t, r, "internal/core/core.go", "package core\nfunc Value() int {\n return 2\n}\n")
	report, e = Coverage(r, c, "", time.Now())
	if e != nil || report.IncrementalPercent == nil || *report.IncrementalPercent != 100 {
		t.Fatal(report, e)
	}
	c.Profiles = c.Profiles[:1]
	report, e = Coverage(r, c, "", time.Now())
	if e == nil || report.Status != "failed" || report.IncrementalCovered != 0 {
		t.Fatal(report, e)
	}
	put(t, r, "coverage/integration.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 2 2\n")
	c.Profiles = append(c.Profiles, "coverage/integration.out")
	if _, e = Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("incompatible weights accepted")
	}
}
func TestCoverageMissingAndBaseline(t *testing.T) {
	r := repo(t)
	c := goCoverage()
	if _, e := Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("missing report passed")
	}
	put(t, r, "coverage/unit.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 0\n")
	c.Baseline = &Baseline{Started: "2026-09-16", Minimum: 0, Expires: "2026-10-01", Reason: "measured migration baseline"}
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	report, e := Coverage(r, c, "", now)
	if e != nil || report.Minimum != 0 {
		t.Fatal(report, e)
	}
	if _, e = Coverage(r, c, "", now.AddDate(0, 1, 0)); e == nil {
		t.Fatal("expired baseline passed")
	}
	c.Baseline.Expires = "invalid"
	if _, e = Coverage(r, c, "", now); e == nil {
		t.Fatal("invalid baseline passed")
	}
	c.Baseline = nil
	put(t, r, "coverage/unit.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 1\n")
	put(t, r, "internal/core/untracked.go", "package core\nfunc Missing() int {return 9}\n")
	report, e = Coverage(r, c, "", now)
	if e == nil || len(report.Missing) != 1 {
		t.Fatal(report, e)
	}
	c.Minimum = 69
	if _, e = Coverage(r, c, "", now); e == nil {
		t.Fatal("weak threshold passed")
	}
	c = goCoverage()
	c.Profiles = []string{"../outside"}
	if _, e = Coverage(r, c, "", now); e == nil {
		t.Fatal("profile path escaped")
	}
}
func TestMalformedProfiles(t *testing.T) {
	r := repo(t)
	for _, tc := range []struct{ format, data string }{{"go", ""}, {"go", "mode: wrong\n"}, {"go", "mode: atomic\nbad"}, {"go", "example.test/project/f.go:1.1,2.1 1 1"}, {"lcov", "DA:1,1\n"}, {"lcov", "SF:a.dart\nDA:x,1\n"}, {"lcov", "SF:a.dart\nDA:1,-1\n"}, {"lcov", "SF:a.dart\nDA:0,1\n"}, {"other", "anything"}} {
		put(t, r, "coverage/bad", tc.data)
		if _, e := parseProfile(r, filepath.Join(r, "coverage/bad"), tc.format); e == nil {
			t.Errorf("accepted %q", tc.data)
		}
	}
	if !excluded("internal/generated/a.go", []string{"internal/generated/**"}) || !excluded("a.g.dart", []string{"*.g.dart"}) || excluded("handwritten.go", []string{"generated/**"}) {
		t.Fatal("exclusion mismatch")
	}
}
func TestLCOVInventory(t *testing.T) {
	r := repo(t)
	put(t, r, "lib/a.dart", "int f() {\n return 1;\n}\n")
	gitOK(t, r, "add", "lib")
	gitOK(t, r, "commit", "-qm", "feat(ui): Add fixture")
	put(t, r, "coverage/lcov.info", "SF:lib/a.dart\nDA:1,1\nDA:2,0\nend_of_record\n")
	put(t, r, "coverage/inventory.json", `{"files":{"lib/a.dart":[1,2]}}`)
	c := CoverageConfig{Format: "lcov", Profiles: []string{"coverage/lcov.info"}, SourceRoots: []string{"lib"}, Inventory: "coverage/inventory.json", Minimum: 70, IncrementalMinimum: 90}
	report, e := Coverage(r, c, "", time.Now())
	if e == nil || report.Total != 2 || report.Covered != 1 {
		t.Fatal(report, e)
	}
	put(t, r, "coverage/lcov.info", "SF:lib/a.dart\nDA:1,1\nDA:2,1\nend_of_record\n")
	report, e = Coverage(r, c, "", time.Now())
	if e != nil || report.Percent != 100 {
		t.Fatal(report, e)
	}
	put(t, r, "coverage/inventory.json", `{"files":{}}`)
	if _, e = Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("missing inventory source passed")
	}
}
func TestReviewEvidence(t *testing.T) {
	r := repo(t)
	if e := ReviewCheck(r, ""); e == nil {
		t.Fatal("missing review passed")
	}
	fp, e := Fingerprint(r)
	if e != nil {
		t.Fatal(e)
	}
	review := Review{SchemaVersion: 1, Fingerprint: fp, Reviewer: "independent-agent", Independent: true, Scope: []string{"internal"}}
	path := filepath.Join(r, ".governance/review.json")
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	if e = ReviewCheck(r, ""); e != nil {
		t.Fatal(e)
	}
	review.Findings = []Finding{{Severity: "P1", File: "internal/core/core.go", Line: 3, Rule: "isolation", Trigger: "cross tenant", Impact: "leak", Recommendation: "scope query"}}
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	if e = ReviewCheck(r, ""); e == nil {
		t.Fatal("blocking review passed")
	}
	review.Findings[0].Resolved = true
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	if e = ReviewCheck(r, ""); e != nil {
		t.Fatal(e)
	}
	review.Findings[0].Severity = "unknown"
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	if e = ReviewCheck(r, ""); e == nil {
		t.Fatal("invalid severity passed")
	}
	review.Findings[0].Severity = "P2"
	review.Findings[0].Rule = ""
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	if e = ReviewCheck(r, ""); e == nil {
		t.Fatal("incomplete finding passed")
	}
	review.Findings = nil
	if e = WriteJSON(path, review); e != nil {
		t.Fatal(e)
	}
	put(t, r, "internal/core/core.go", "package core\nfunc Value() int {return 3}\n")
	if e = ReviewCheck(r, ""); e == nil {
		t.Fatal("stale review passed")
	}
	if e = ReviewCheck(r, "../escape"); e == nil {
		t.Fatal("review path escaped")
	}
}

// Subprocess fixtures avoid shell parsing and external tool dependencies.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("LEDGER_TEST_PROCESS") != "1" {
		return
	}
	args := os.Args
	index := 0
	for i, v := range args {
		if v == "--" {
			index = i + 1
			break
		}
	}
	args = args[index:]
	switch args[0] {
	case "echo":
		fmt.Print(strings.Join(args[1:], "|"))
	case "env":
		fmt.Print(os.Getenv("LEDGER_FIXTURE_VALUE"))
	case "wait":
		time.Sleep(30 * time.Second)
	case "fail":
		os.Exit(7)
	case "fixture":
		b, e := os.ReadFile(args[1])
		if e != nil || string(b) != "fixed" {
			fmt.Print("ASSERT fixture is fixed")
			os.Exit(3)
		}
	}
	os.Exit(0)
}
func helperStep(mode string, args ...string) Step {
	argv := []string{os.Args[0], "-test.run=^TestProcessHelper$", "--", mode}
	argv = append(argv, args...)
	return Step{Argv: argv, Env: map[string]string{"LEDGER_TEST_PROCESS": "1"}}
}
func TestRunner(t *testing.T) {
	r := repo(t)
	var output bytes.Buffer
	runner := &Runner{Root: r, Out: &output, Err: io.Discard, Config: Config{Commands: map[string]Command{"echo": {Steps: []Step{helperStep("echo", "fixed")}}, "empty": {}}}}
	if e := runner.Run(context.Background(), "echo", []string{"a b", "$(touch never)", "{root}"}); e != nil {
		t.Fatal(e)
	}
	if output.String() != "fixed|a b|$(touch never)|"+r {
		t.Fatalf("argv altered: %q", output.String())
	}
	for _, name := range []string{"unknown", "empty"} {
		if e := runner.Run(context.Background(), name, nil); e == nil {
			t.Fatal("invalid command passed")
		}
	}
	if e := runner.Step(context.Background(), Step{}); e == nil {
		t.Fatal("empty argv passed")
	}
	if e := runner.Step(context.Background(), Step{Argv: []string{"@ledger-tool"}}); e == nil {
		t.Fatal("invalid nested command passed")
	}
	runner.Config.Commands["recursive"] = Command{Steps: []Step{{Argv: []string{"@ledger-tool", "recursive"}}}}
	runner.Dispatch = runner.Run
	if e := runner.Run(context.Background(), "recursive", nil); e == nil {
		t.Fatal("recursive command passed")
	}
	output.Reset()
	s := helperStep("env")
	s.Env["LEDGER_FIXTURE_VALUE"] = "value:{root}"
	s.Dir = "internal"
	if e := runner.Step(context.Background(), s); e != nil || output.String() != "value:"+r {
		t.Fatal(output.String(), e)
	}
	s = helperStep("wait")
	s.TimeoutSeconds = 1
	start := time.Now()
	if e := runner.Step(context.Background(), s); e == nil {
		t.Fatal("timeout passed")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("timeout did not stop process")
	}
	if _, e := Process(context.Background(), nil); e == nil {
		t.Fatal("empty process passed")
	}
	if _, e := Process(context.Background(), []string{"ledger-impossible-test-command"}); e == nil {
		t.Fatal("missing tool passed")
	}
}
func TestWorkspaceAndDoctor(t *testing.T) {
	r := repo(t)
	put(t, r, ".env.local", "SECRET")
	put(t, r, "internal/other/file.go", "package other")
	dest := t.TempDir()
	if e := CopyWorkspace(r, dest, []string{"internal/core"}); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dest, "go.mod")); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{".env.local", "internal/other/file.go"} {
		if _, e := os.Stat(filepath.Join(dest, p)); !os.IsNotExist(e) {
			t.Fatalf("copied excluded file %s", p)
		}
	}
	if e := CompareTree(filepath.Join(r, "internal/core"), filepath.Join(dest, "internal/core")); e != nil {
		t.Fatal(e)
	}
	put(t, dest, "internal/core/core.go", "changed")
	if e := CompareTree(filepath.Join(r, "internal/core"), filepath.Join(dest, "internal/core")); e == nil {
		t.Fatal("drift passed")
	}
	put(t, dest, "internal/core/new", "extra")
	if e := CompareTree(filepath.Join(r, "internal/core"), filepath.Join(dest, "internal/core")); e == nil {
		t.Fatal("file set drift passed")
	}
	runner := &Runner{Root: r, Out: io.Discard, Err: io.Discard, Config: Config{Tools: map[string][]string{"missing": {"ledger-impossible-command"}}}}
	if e := Doctor(context.Background(), runner); e == nil {
		t.Fatal("doctor passed missing capability")
	}
	runner.Config.Tools = map[string][]string{"git": {"git", "--version"}}
	if e := Doctor(context.Background(), runner); e != nil {
		t.Fatal(e)
	}
	if e := Isolated(context.Background(), runner, []string{"internal/core"}, "internal/core", []string{"git", "--version"}); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(r, "internal/core/core.go"), filepath.Join(r, "internal/core/link.go")); e == nil {
		if e = CopyWorkspace(r, t.TempDir(), []string{"internal/core"}); e == nil {
			t.Fatal("copied symlink")
		}
	}
}
func TestDebugLifecycle(t *testing.T) {
	r := repo(t)
	put(t, r, "fixture.txt", "broken")
	runner := &Runner{Root: r, Out: io.Discard, Err: io.Discard, Config: Config{DebugProfiles: map[string]Command{"case": {Steps: []Step{helperStep("fixture", "{root}/fixture.txt")}}}}}
	session, e := DebugStart(context.Background(), r, "regression")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = LoadSession(r, "../escape"); e == nil {
		t.Fatal("invalid session id passed")
	}
	if e = DebugRunProfile(context.Background(), runner, session.ID, "unknown", "reproduce"); e == nil {
		t.Fatal("unknown profile passed")
	}
	if e = DebugRunProfile(context.Background(), runner, session.ID, "case", "reproduce"); e == nil {
		t.Fatal("missing hypothesis passed")
	}
	session.Expected = "fixed fixture"
	session.Actual = "broken fixture"
	session.Hypotheses = []string{"wrong state"}
	session.FailurePattern = "ASSERT fixture is fixed"
	path, e := sessionPath(r, session.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = WriteJSON(path, session); e != nil {
		t.Fatal(e)
	}
	if e = DebugRunProfile(context.Background(), runner, session.ID, "case", "verify"); e == nil {
		t.Fatal("unreproduced verify passed")
	}
	if e = DebugRunProfile(context.Background(), runner, session.ID, "case", "reproduce"); e == nil {
		t.Fatal("broken fixture unexpectedly passed")
	}
	session, _, e = LoadSession(r, session.ID)
	if e != nil || session.Status != "reproduced" {
		t.Fatal(session, e)
	}
	if e = DebugRunProfile(context.Background(), runner, session.ID, "case", "verify"); e == nil {
		t.Fatal("same source verify passed")
	}
	put(t, r, "fixture.txt", "fixed")
	if e = DebugRunProfile(context.Background(), runner, session.ID, "case", "verify"); e != nil {
		t.Fatal(e)
	}
	session, _, e = LoadSession(r, session.ID)
	if e != nil || session.Status != "verified-profile" {
		t.Fatal(session, e)
	}
	var report bytes.Buffer
	if e = DebugReport(r, session.ID, &report); e != nil || !strings.Contains(report.String(), "verified-profile") {
		t.Fatal(report.String(), e)
	}
	put(t, r, "fixture.txt", "later change")
	report.Reset()
	if e = DebugReport(r, session.ID, &report); e != nil || !strings.Contains(report.String(), "stale") {
		t.Fatal(report.String(), e)
	}
}
func pngFixture(t *testing.T, root, path string, w int, changed bool) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, 2))
	if changed {
		img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	}
	var b bytes.Buffer
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	put(t, root, path, b.String())
}
func TestUIReport(t *testing.T) {
	r := repo(t)
	pngFixture(t, r, "build/before.png", 2, false)
	pngFixture(t, r, "build/after.png", 2, true)
	fp, e := Fingerprint(r)
	if e != nil {
		t.Fatal(e)
	}
	m := UIManifest{SchemaVersion: 1, Base: "base", Head: "head", Fingerprint: fp, Captures: []Capture{{Scenario: "<script>bad</script>", Device: "phone", Runtime: "test", Locale: "en", Theme: "light", TextScale: 1, Before: "build/before.png", After: "build/after.png", AssertionsPassed: true}}}
	path := filepath.Join(r, ".governance/ui.json")
	if e = WriteJSON(path, m); e != nil {
		t.Fatal(e)
	}
	gitOK(t, r, "add", "-f", ".governance/ui.json")
	if e = UIReport(r, ".governance/ui.json"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(r, "build/ui-report/manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var result UIManifest
	if e = json.Unmarshal(b, &result); e != nil || result.Captures[0].ChangedPixels != 1 {
		t.Fatal(result, e)
	}
	html, e := os.ReadFile(filepath.Join(r, "build/ui-report/index.html"))
	if e != nil || strings.Contains(string(html), "<script>bad") {
		t.Fatal("unsafe html", e)
	}
	m.Captures[0].AssertionsPassed = false
	if e = WriteJSON(path, m); e != nil {
		t.Fatal(e)
	}
	if e = UIReport(r, ".governance/ui.json"); e == nil {
		t.Fatal("failing assertions accepted")
	}
	m.Captures[0].AssertionsPassed = true
	m.Captures[0].Before = "../escape"
	if e = WriteJSON(path, m); e != nil {
		t.Fatal(e)
	}
	if e = UIReport(r, ".governance/ui.json"); e == nil {
		t.Fatal("escaping capture accepted")
	}
	m.Captures[0].Before = "build/before.png"
	pngFixture(t, r, "build/after.png", 3, true)
	if e = WriteJSON(path, m); e != nil {
		t.Fatal(e)
	}
	if e = UIReport(r, ".governance/ui.json"); e == nil {
		t.Fatal("geometry mismatch accepted")
	}
	m.Fingerprint = "stale"
	if e = WriteJSON(path, m); e != nil {
		t.Fatal(e)
	}
	if e = UIReport(r, ".governance/ui.json"); e == nil {
		t.Fatal("stale manifest accepted")
	}
}

func TestMutationGate(t *testing.T) {
	r := repo(t)
	if e := MutationCheck(r, "../escape"); e == nil {
		t.Fatal("mutation path escaped")
	}
	if e := MutationCheck(r, "build/missing.json"); e == nil {
		t.Fatal("missing report passed")
	}
	for _, tc := range []struct {
		data string
		pass bool
	}{
		{`{`, false}, {`{}`, false}, {`{"files":[{"file_name":"money.go","mutations":[{"status":"UNKNOWN"}]}]}`, false},
		{`{"files":[{"file_name":"money.go","mutations":[{"status":"NOT_VIABLE"}]}]}`, false},
		{`{"files":[{"file_name":"money.go","mutations":[{"status":"KILLED","line":1}]}]}`, true},
		{`{"files":[{"file_name":"money.go","mutations":[{"status":"KILLED"},{"status":"LIVED"},{"status":"NOT_COVERED"},{"status":"TIMED_OUT"},{"status":"UNCOVERED"},{"status":"TIMEOUT"}]}]}`, false},
	} {
		put(t, r, "build/mutation.json", tc.data)
		e := MutationCheck(r, "build/mutation.json")
		if (e == nil) != tc.pass {
			t.Fatalf("pass=%t got %v for %s", tc.pass, e, tc.data)
		}
	}
}
func TestCoverageCannotBeInflated(t *testing.T) {
	r := repo(t)
	for _, source := range []string{"outside.go", "../outside.go"} {
		put(t, r, "coverage/unit.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 0\n"+source+":1.1,2.10 1000 100\n")
		if _, e := Coverage(r, goCoverage(), "", time.Now()); e == nil {
			t.Fatal("out-of-inventory coverage inflated score")
		}
	}
}
func TestCoverageSourceAndInventoryFailures(t *testing.T) {
	r := repo(t)
	put(t, r, "coverage/unit.out", "mode: atomic\nexample.test/project/internal/core/core.go:2.1,3.10 1 1\n")
	c := goCoverage()
	for _, roots := range [][]string{{"missing"}, {"../escape"}} {
		c.SourceRoots = roots
		if _, e := Coverage(r, c, "", time.Now()); e == nil {
			t.Fatal("invalid source root passed")
		}
	}
	c = goCoverage()
	c.Inventory = "../escape"
	if _, e := Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("inventory path escaped")
	}
	c.Inventory = "build/missing.json"
	if _, e := Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("missing inventory passed")
	}
	put(t, r, c.Inventory, "{")
	if _, e := Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("malformed inventory passed")
	}
	c = goCoverage()
	c.Profiles = nil
	if _, e := Coverage(r, c, "", time.Now()); e == nil {
		t.Fatal("empty denominator passed")
	}
	c = goCoverage()
	if _, e := Coverage(r, c, "invalid-base", time.Now()); e == nil {
		t.Fatal("invalid base passed")
	}
	if normalized := normalizeSource(r, filepath.Join(r, "internal/core/core.go")); normalized != "internal/core/core.go" {
		t.Fatal(normalized)
	}
	put(t, r, "internal/core/declarations.go", "package core\ntype Plain struct{ V int }\n")
	if hasGoCode(r, "internal/core/declarations.go") {
		t.Fatal("declaration is executable")
	}
	put(t, r, "internal/core/literal.go", "package core\nvar F = func() {}\n")
	if !hasGoCode(r, "internal/core/literal.go") {
		t.Fatal("missed function literal")
	}
	put(t, r, "internal/core/bad.go", "not Go")
	if !hasGoCode(r, "internal/core/bad.go") {
		t.Fatal("parse error concealed")
	}
	if e := os.Symlink(filepath.Join(r, "internal/core/core.go"), filepath.Join(r, "internal/core/link.go")); e == nil {
		if _, e = productionFiles(r, c); e == nil {
			t.Fatal("source symlink accepted")
		}
	}
}
func TestErrorSurfaces(t *testing.T) {
	r := t.TempDir()
	if _, e := SourceFiles(r); e == nil {
		t.Fatal("nonrepo passed")
	}
	if _, e := Fingerprint(r); e == nil {
		t.Fatal("nonrepo fingerprint passed")
	}
	if _, e := ChangedLines(r, ""); e == nil {
		t.Fatal("nonrepo changes passed")
	}
	if _, e := Classify(r, "invalid", "go"); e == nil {
		t.Fatal("invalid base passed")
	}
	if _, e := Classify(r, "", "go"); e == nil {
		t.Fatal("nonrepo classification passed")
	}
	if _, e := DebugStart(context.Background(), r, "bad"); e == nil {
		t.Fatal("nonrepo debug passed")
	}
	if _, _, e := LoadSession(r, "missing"); e == nil {
		t.Fatal("missing session passed")
	}
	if e := DebugReport(r, "missing", io.Discard); e == nil {
		t.Fatal("missing debug report passed")
	}
	if e := ReviewCheck(r, ""); e == nil {
		t.Fatal("missing review passed")
	}
	if e := CopyWorkspace(r, t.TempDir(), nil); e == nil {
		t.Fatal("nonrepo copy passed")
	}
	put(t, r, "file", "content")
	if e := WriteJSON(filepath.Join(r, "file/child"), map[string]int{"a": 1}); e == nil {
		t.Fatal("non-directory write passed")
	}
	if e := WriteOutput(filepath.Join(r, "missing/output"), Changes{}); e == nil {
		t.Fatal("missing output directory passed")
	}
	if e := CompareTree(filepath.Join(r, "missing"), r); e == nil {
		t.Fatal("missing comparison passed")
	}
	put(t, r, "same", "content")
	if e := CompareTree(filepath.Join(r, "file"), filepath.Join(r, "same")); e != nil {
		t.Fatal(e)
	}
	repoRoot := repo(t)
	put(t, repoRoot, ".governance/review.json", "{")
	if e := ReviewCheck(repoRoot, ""); e == nil {
		t.Fatal("malformed review passed")
	}
	put(t, repoRoot, "build/debug/bad/session.json", "{")
	if _, _, e := LoadSession(repoRoot, "bad"); e == nil {
		t.Fatal("malformed session passed")
	}
	runner := &Runner{Root: repoRoot, Out: io.Discard, Err: io.Discard}
	if e := Isolated(context.Background(), runner, nil, "", []string{"ledger-impossible-command"}); e == nil {
		t.Fatal("missing isolated tool passed")
	}
	if e := Isolated(context.Background(), runner, nil, "../escape", []string{"git", "--version"}); e == nil {
		t.Fatal("isolated output escaped")
	}
}
func TestDebugSpecificFailureAndProfileIdentity(t *testing.T) {
	r := repo(t)
	put(t, r, "fixture.txt", "broken")
	step := helperStep("fixture", "{root}/fixture.txt")
	runner := &Runner{Root: r, Out: io.Discard, Err: io.Discard, Config: Config{DebugProfiles: map[string]Command{"case": {Steps: []Step{step}}}}}
	s, e := DebugStart(context.Background(), r, "case")
	if e != nil {
		t.Fatal(e)
	}
	s.Expected = "fixed"
	s.Actual = "broken"
	s.Hypotheses = []string{"fixture"}
	path, e := sessionPath(r, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, pattern := range []string{"[", ".*"} {
		s.FailurePattern = pattern
		if e = WriteJSON(path, s); e != nil {
			t.Fatal(e)
		}
		if e = DebugRunProfile(context.Background(), runner, s.ID, "case", "reproduce"); e == nil {
			t.Fatal("invalid failure signature passed")
		}
	}
	s.FailurePattern = "unrelated failure"
	if e = WriteJSON(path, s); e != nil {
		t.Fatal(e)
	}
	if e = DebugRunProfile(context.Background(), runner, s.ID, "case", "reproduce"); e == nil {
		t.Fatal("broken process passed")
	}
	stored, _, e := LoadSession(r, s.ID)
	if e != nil || stored.Status != "unconfirmed" || stored.Runs[0].FailureMatched {
		t.Fatal(stored, e)
	}
	s.FailurePattern = "ASSERT fixture is fixed"
	if e = WriteJSON(path, s); e != nil {
		t.Fatal(e)
	}
	if e = DebugRunProfile(context.Background(), runner, s.ID, "case", "reproduce"); e == nil {
		t.Fatal("broken process passed")
	}
	put(t, r, "fixture.txt", "fixed")
	runner.Config.DebugProfiles["case"] = Command{Steps: []Step{helperStep("echo", "different test")}}
	if e = DebugRunProfile(context.Background(), runner, s.ID, "case", "verify"); e == nil {
		t.Fatal("changed profile passed verify")
	}
	runner.Config.DebugProfiles["case"] = Command{Steps: []Step{step}}
	s, _, e = LoadSession(r, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	s.Probes = []string{"temporary logging"}
	if e = WriteJSON(path, s); e != nil {
		t.Fatal(e)
	}
	if e = DebugRunProfile(context.Background(), runner, s.ID, "case", "verify"); e != nil {
		t.Fatal(e)
	}
	s, _, e = LoadSession(r, s.ID)
	if e != nil || s.Status != "probe-cleanup-required" {
		t.Fatal(s, e)
	}
}
func TestUIMalformedEvidence(t *testing.T) {
	r := repo(t)
	if e := UIReport(r, "../escape"); e == nil {
		t.Fatal("UI manifest escaped")
	}
	if e := UIReport(r, "build/missing.json"); e == nil {
		t.Fatal("missing UI manifest passed")
	}
	put(t, r, "build/bad.json", "{")
	if e := UIReport(r, "build/bad.json"); e == nil {
		t.Fatal("bad UI JSON passed")
	}
	if _, e := readPNG(r, "build/missing.png"); e == nil {
		t.Fatal("missing PNG passed")
	}
	put(t, r, "build/bad.png", "not png")
	if _, e := readPNG(r, "build/bad.png"); e == nil {
		t.Fatal("invalid PNG passed")
	}
}

func TestLCOVUnreportedFileIsUncovered(t *testing.T) {
	r := repo(t)
	put(t, r, "lib/a.dart", "int a() => 1;\n")
	put(t, r, "lib/b.dart", "int b() => 2;\n")
	gitOK(t, r, "add", "lib")
	gitOK(t, r, "commit", "-qm", "feat(ui): Add test source")
	put(t, r, "coverage/lcov.info", "SF:lib/a.dart\nDA:1,1\nend_of_record\n")
	put(t, r, "coverage/inventory.json", `{"files":{"lib/a.dart":[1],"lib/b.dart":[1]}}`)
	c := CoverageConfig{Format: "lcov", Profiles: []string{"coverage/lcov.info"}, SourceRoots: []string{"lib"}, Inventory: "coverage/inventory.json", Minimum: 70, IncrementalMinimum: 90}
	report, e := Coverage(r, c, "", time.Now())
	if e == nil || report.Total != 2 || report.Covered != 1 {
		t.Fatal(report, e)
	}
}

func TestProcessCancellationBeforeStart(t *testing.T) {
	cmd, err := Process(context.Background(), helperStep("echo", "unused").Argv)
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Cancel(); err != nil {
		t.Fatalf("cancelling an unstarted process: %v", err)
	}
}
