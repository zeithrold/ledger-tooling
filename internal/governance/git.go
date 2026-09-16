package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func git(root string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = root
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), e, b)
	}
	return string(b), nil
}
func Base(root, base string) (string, error) {
	if base != "" && strings.Trim(base, "0") == "" {
		return "EMPTY", nil
	}
	if _, err := git(root, "rev-parse", "--verify", "HEAD"); err != nil {
		return "EMPTY", nil
	}
	if base == "HEAD^" {
		if _, err := git(root, "rev-parse", "--verify", base); err != nil {
			return "EMPTY", nil
		}
	}
	if base == "" {
		return "HEAD", nil
	}
	s, e := git(root, "merge-base", base, "HEAD")
	return strings.TrimSpace(s), e
}
func SourceFiles(root string) ([]string, error) {
	s, e := git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}
func Fingerprint(root string) (string, error) {
	files, e := SourceFiles(root)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	for _, p := range files {
		if strings.HasPrefix(p, ".governance/reviews/") || p == ".governance/review.json" || p == ".governance/ui.json" {
			continue
		}
		if _, err := safePath(root, p); err != nil {
			return "", err
		}
		b, e := os.ReadFile(filepath.Join(root, p))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var hunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func ChangedLines(root, base string) (map[string]map[int]bool, error) {
	b, e := Base(root, base)
	if e != nil {
		return nil, e
	}
	if b == "EMPTY" {
		out := map[string]map[int]bool{}
		files, err := SourceFiles(root)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if _, err := safePath(root, f); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(filepath.Join(root, f))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			out[f] = map[int]bool{}
			for i := 1; i <= strings.Count(string(data), "\n")+1; i++ {
				out[f][i] = true
			}
		}
		return out, nil
	}
	diff, e := git(root, "-c", "core.quotePath=false", "diff", "--no-ext-diff", "--no-renames", "--unified=0", b, "--")
	if e != nil {
		return nil, e
	}
	out := map[string]map[int]bool{}
	file := ""
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			file = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		m := hunk.FindStringSubmatch(line)
		if m == nil || file == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		if out[file] == nil {
			out[file] = map[int]bool{}
		}
		for i := 0; i < n; i++ {
			out[file][start+i] = true
		}
	}
	untracked, e := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if e != nil {
		return nil, e
	}
	for _, f := range strings.Split(untracked, "\x00") {
		if f == "" {
			continue
		}
		if _, err := safePath(root, f); err != nil {
			return nil, err
		}
		data, e := os.ReadFile(filepath.Join(root, f))
		if e != nil {
			return nil, e
		}
		out[f] = map[int]bool{}
		for i := 1; i <= strings.Count(string(data), "\n")+1; i++ {
			out[f][i] = true
		}
	}
	return out, nil
}

type Changes struct {
	Visual   bool     `json:"visual"`
	Code     bool     `json:"code"`
	Backend  bool     `json:"backend"`
	UI       bool     `json:"ui"`
	Policy   bool     `json:"policy"`
	Mutation bool     `json:"mutation"`
	Fuzz     bool     `json:"fuzz"`
	Native   bool     `json:"native"`
	DocsOnly bool     `json:"docs_only"`
	Files    []string `json:"files"`
}

func Classify(root, base, kind string) (Changes, error) {
	var c Changes
	b, e := Base(root, base)
	if e != nil {
		return c, e
	}
	s, e := git(root, "diff", "--name-only", "-z", b, "--")
	if b == "EMPTY" {
		files, err := SourceFiles(root)
		e = err
		s = strings.Join(files, "\x00") + "\x00"
	}
	if e != nil {
		return c, e
	}
	u, e := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if e != nil {
		return c, e
	}
	seen := map[string]bool{}
	for _, f := range strings.Split(s+u, "\x00") {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		c.Files = append(c.Files, f)
		ext := filepath.Ext(f)
		doc := ext == ".md" || ext == ".txt"
		if !doc {
			c.Code = true
		}
		if strings.HasPrefix(f, ".github/") || strings.HasPrefix(f, ".agents/") || strings.HasPrefix(f, "tool/") || strings.HasPrefix(f, ".governance/") || strings.HasPrefix(f, ".go") || f == "governance.json" || f == "justfile" || f == "go.mod" || f == "go.sum" || strings.HasPrefix(f, "pubspec.") || f == "analysis_options.yaml" || f == "AGENTS.md" || f == "DESIGN.md" || f == "CONTRIBUTING.md" || strings.HasPrefix(f, "docs/") {
			c.Policy = true
		}
		if kind == "go" && !doc {
			c.Backend = true
		}
		if strings.HasPrefix(f, "lib/") || strings.HasPrefix(f, "assets/") || strings.HasPrefix(f, "integration_test/") || strings.HasPrefix(f, "test_driver/") {
			c.UI = true
		}
		if strings.HasPrefix(f, "lib/features/") || strings.HasPrefix(f, "lib/shared/") || strings.HasPrefix(f, "lib/app/") || strings.HasPrefix(f, "lib/l10n/") || strings.HasPrefix(f, "assets/") {
			c.Visual = true
		}
		if strings.HasPrefix(f, "ios/") || strings.HasPrefix(f, "android/") {
			c.Native = true
			c.UI = true
		}
		if strings.Contains(f, "money") || strings.Contains(f, "accounting") || strings.Contains(f, "auth") || strings.Contains(f, "identity") || strings.Contains(f, "config") {
			c.Fuzz = true
		}
		if strings.Contains(f, "money") {
			c.Mutation = true
		}
	}
	sort.Strings(c.Files)
	if c.Policy {
		c.Code = true
		c.Fuzz = true
		c.Mutation = true
		c.Backend = kind == "go"
		c.UI = kind == "flutter"
		c.Native = kind == "flutter"
	}
	c.DocsOnly = !c.Code && !c.Policy
	return c, nil
}

var commitPattern = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9-]*\))?!?: [A-Za-z][^\r\n]+$`)

func CommitCheck(title string) error {
	if len(title) > 100 || !commitPattern.MatchString(title) {
		return fmt.Errorf("expected Conventional Commit title, at most 100 characters: type(scope): English summary")
	}
	return nil
}
func GitVersion(ctx context.Context, root string) string {
	s, e := git(root, "rev-parse", "HEAD")
	if e != nil {
		return "uncommitted"
	}
	return strings.TrimSpace(s)
}
