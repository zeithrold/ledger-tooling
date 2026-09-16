package governance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPolicyRatchet(t *testing.T) {
	previous := CoverageConfig{Minimum: 70, IncrementalMinimum: 90, SourceRoots: []string{"internal/core/"}, Exclude: []string{"generated/**"}}
	for _, tc := range []struct {
		name      string
		change    func(*CoverageConfig)
		wantError bool
	}{
		{"same", func(*CoverageConfig) {}, false},
		{"switch-format", func(c *CoverageConfig) { c.Format = "lcov" }, true},
		{"increase", func(c *CoverageConfig) { c.Minimum = 80; c.IncrementalMinimum = 95 }, false},
		{"lower-total", func(c *CoverageConfig) { c.Minimum = 69 }, true},
		{"lower-diff", func(c *CoverageConfig) { c.IncrementalMinimum = 89 }, true},
		{"new-exclusion", func(c *CoverageConfig) { c.Exclude = append(c.Exclude, "internal/core/**") }, true},
		{"drop-exclusion", func(c *CoverageConfig) { c.Exclude = nil }, false},
		{"narrow-roots", func(c *CoverageConfig) { c.SourceRoots = []string{"internal/core/sub"} }, true},
		{"expand-roots", func(c *CoverageConfig) { c.SourceRoots = []string{"internal/"} }, false},
		{"all-roots", func(c *CoverageConfig) { c.SourceRoots = []string{"."} }, false},
		{"new-low-baseline", func(c *CoverageConfig) { c.Baseline = &Baseline{Minimum: 50} }, true},
		{"new-high-baseline", func(c *CoverageConfig) { c.Baseline = &Baseline{Minimum: 75} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := previous
			current.Exclude = append([]string{}, previous.Exclude...)
			tc.change(&current)
			if e := compareCoveragePolicy(previous, current); (e != nil) != tc.wantError {
				t.Fatal(e)
			}
		})
	}
	previous.Baseline = &Baseline{Started: "2026-09-16", Minimum: 50, Expires: "2026-10-16", Reason: "migration"}
	for _, tc := range []struct {
		name      string
		value     *Baseline
		wantError bool
	}{
		{"removed", nil, false}, {"higher-earlier", &Baseline{Minimum: 60, Expires: "2026-10-01"}, false}, {"lower", &Baseline{Minimum: 49, Expires: "2026-10-16"}, true}, {"extended", &Baseline{Minimum: 50, Expires: "2026-10-17"}, true}, {"malformed", &Baseline{Minimum: 50, Expires: "bad"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := previous
			current.Baseline = tc.value
			if e := compareCoveragePolicy(previous, current); (e != nil) != tc.wantError {
				t.Fatal(e)
			}
		})
	}
	previous.Baseline.Expires = "bad"
	if e := compareCoveragePolicy(previous, previous); e == nil {
		t.Fatal("malformed prior expiry accepted")
	}
}

func TestPolicyInventoryCannotDisappear(t *testing.T) {
	previous := CoverageConfig{Format: "lcov", Minimum: 70, IncrementalMinimum: 90, SourceRoots: []string{"lib"}, Inventory: "coverage/inventory.json"}
	for _, path := range []string{"", "coverage/different.json", "coverage/inventory.json"} {
		current := previous
		current.Inventory = path
		if err := compareCoveragePolicy(previous, current); (err != nil) != (path != previous.Inventory) {
			t.Fatalf("inventory %q: %v", path, err)
		}
	}
	current := previous
	previous.Inventory = ""
	if err := compareCoveragePolicy(previous, current); err != nil {
		t.Fatal("adding complete inventory should strengthen policy:", err)
	}
}
func TestPolicyCheckHistory(t *testing.T) {
	r := repo(t)
	if e := PolicyCheck(r, ""); e == nil {
		t.Fatal("missing policy accepted")
	}
	cfg := Config{SchemaVersion: 1, Coverage: goCoverage()}
	if e := WriteJSON(filepath.Join(r, "governance.json"), cfg); e != nil {
		t.Fatal(e)
	}
	if e := PolicyCheck(r, ""); e != nil {
		t.Fatal(e)
	}
	gitOK(t, r, "add", "governance.json")
	gitOK(t, r, "commit", "-qm", "build(policy): Establish coverage")
	if e := PolicyCheck(r, ""); e != nil {
		t.Fatal(e)
	}
	cfg.Coverage.Minimum = 60
	if e := WriteJSON(filepath.Join(r, "governance.json"), cfg); e != nil {
		t.Fatal(e)
	}
	if e := PolicyCheck(r, ""); e == nil {
		t.Fatal("weakened policy passed")
	}
	if e := PolicyCheck(r, "not-a-ref"); e == nil {
		t.Fatal("invalid base passed")
	}
	for _, data := range []string{"{", `{"schema_version":2}`} {
		put(t, r, "governance.json", data)
		gitOK(t, r, "add", "governance.json")
		gitOK(t, r, "commit", "-qm", "build(policy): Invalid base fixture")
		cfg.SchemaVersion = 1
		if e := WriteJSON(filepath.Join(r, "governance.json"), cfg); e != nil {
			t.Fatal(e)
		}
		if e := PolicyCheck(r, ""); e == nil {
			t.Fatal("invalid previous policy accepted")
		}
	}
	empty := t.TempDir()
	gitOK(t, empty, "init", "-q")
	if e := WriteJSON(filepath.Join(empty, "governance.json"), Config{SchemaVersion: 1}); e != nil {
		t.Fatal(e)
	}
	if e := PolicyCheck(empty, ""); e != nil {
		t.Fatal(e)
	}
}
func TestVersionCapability(t *testing.T) {
	t.Setenv("LEDGER_TEST_PROCESS", "1")
	argv := helperStep("echo", "v1.2.3").Argv
	for _, tc := range []struct {
		pattern string
		fail    bool
	}{{`v1\.2\.3`, false}, {`v2`, true}, {`[`, true}, {`.*`, true}} {
		if e := VersionCheck(context.Background(), argv, tc.pattern); (e != nil) != tc.fail {
			t.Fatal(tc, e)
		}
	}
	if e := VersionCheck(context.Background(), []string{"ledger-impossible-command"}, "v1"); e == nil {
		t.Fatal("missing capability passed")
	}
	if e := VersionCheck(context.Background(), helperStep("fail").Argv, "v1"); e == nil {
		t.Fatal("failed capability passed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := VersionCheck(ctx, helperStep("wait").Argv, "v1"); e == nil {
		t.Fatal("cancelled capability passed")
	}
	var output versionOutput
	data := []byte(strings.Repeat("x", (1<<20)+20))
	n, e := output.Write(data)
	if e != nil || n != len(data) || !output.truncated || output.buffer.Len() != 1<<20 {
		t.Fatal(n, e, output.buffer.Len())
	}
	if _, e = output.Write([]byte("more")); e != nil || output.buffer.Len() != 1<<20 {
		t.Fatal(e)
	}
}

func TestVersionOutputProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--" || i+1 >= len(os.Args) {
			continue
		}
		if os.Args[i+1] == "large" {
			fmt.Fprint(os.Stdout, strings.Repeat("x", (1<<20)+100))
		} else {
			fmt.Fprintln(os.Stderr, "SDK v1.2.3")
		}
		os.Exit(0)
	}
}

func TestVersionOutputBounds(t *testing.T) {
	argv := []string{os.Args[0], "-test.run=^TestVersionOutputProcess$", "--", "large"}
	if err := VersionCheck(context.Background(), argv, "x"); err == nil {
		t.Fatal("oversized actual process output accepted")
	}
	argv[len(argv)-1] = "stderr"
	if err := VersionCheck(context.Background(), argv, `SDK v1\.2\.3`); err != nil {
		t.Fatal("stderr version output rejected:", err)
	}
}
func TestNestedSecretsAndSymlinks(t *testing.T) {
	r := repo(t)
	put(t, r, "internal/core/.env.local", "secret")
	dest := t.TempDir()
	if e := CopyWorkspace(r, dest, []string{"internal/core"}); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dest, "internal/core/.env.local")); !os.IsNotExist(e) {
		t.Fatal("copied nested secret")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if e := os.WriteFile(outside, []byte("outside"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(r, "escape.txt")); e != nil {
		t.Skipf("symlinks unavailable: %v", e)
	}
	if _, e := Fingerprint(r); e == nil {
		t.Fatal("fingerprint read outside symlink")
	}
	if _, e := ChangedLines(r, ""); e == nil {
		t.Fatal("changed lines read outside symlink")
	}
}
