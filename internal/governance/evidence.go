package governance

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Finding struct {
	Severity       string `json:"severity"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Rule           string `json:"rule"`
	Trigger        string `json:"trigger"`
	Impact         string `json:"impact"`
	Recommendation string `json:"recommendation"`
	Resolved       bool   `json:"resolved"`
}
type Review struct {
	SchemaVersion int       `json:"schema_version"`
	Fingerprint   string    `json:"fingerprint"`
	Reviewer      string    `json:"reviewer"`
	Independent   bool      `json:"independent"`
	Scope         []string  `json:"scope"`
	Findings      []Finding `json:"findings"`
	Limitations   []string  `json:"limitations"`
}

func ReviewCheck(root, path string) error {
	if path == "" {
		path = ".governance/review.json"
	}
	p, e := safePath(root, path)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return fmt.Errorf("blocked: independent review missing: %w", e)
	}
	var r Review
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	fp, e := Fingerprint(root)
	if e != nil {
		return e
	}
	if r.SchemaVersion != 1 || r.Fingerprint != fp || r.Reviewer == "" || !r.Independent || len(r.Scope) == 0 {
		return fmt.Errorf("review invalid, not independent, or stale")
	}
	for _, f := range r.Findings {
		if f.File == "" || f.Line < 1 || f.Rule == "" || f.Trigger == "" || f.Impact == "" || f.Recommendation == "" {
			return fmt.Errorf("incomplete review finding")
		}
		if f.Severity != "P0" && f.Severity != "P1" && f.Severity != "P2" && f.Severity != "P3" {
			return fmt.Errorf("invalid review severity")
		}
		if !f.Resolved && (f.Severity == "P0" || f.Severity == "P1") {
			return fmt.Errorf("unresolved blocking review finding: %s", f.Rule)
		}
	}
	return nil
}

type DebugRun struct {
	ProfileHash    string `json:"profile_hash"`
	FailureMatched bool   `json:"failure_matched"`
	Phase          string `json:"phase"`
	Profile        string `json:"profile"`
	Fingerprint    string `json:"fingerprint"`
	Passed         bool   `json:"passed"`
	Error          string `json:"error,omitempty"`
	Time           string `json:"time"`
}
type Session struct {
	FailurePattern string     `json:"failure_pattern"`
	SchemaVersion  int        `json:"schema_version"`
	ID             string     `json:"id"`
	Case           string     `json:"case"`
	Head           string     `json:"head"`
	Fingerprint    string     `json:"fingerprint"`
	Expected       string     `json:"expected"`
	Actual         string     `json:"actual"`
	Hypotheses     []string   `json:"hypotheses"`
	Runs           []DebugRun `json:"runs"`
	Status         string     `json:"status"`
	Probes         []string   `json:"probes"`
}

var sessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Bound in-memory debug output and serialize stdout/stderr writes.
type debugCapture struct {
	mu   sync.Mutex
	data bytes.Buffer
	sink io.Writer
}

func (c *debugCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	const limit = 1 << 20
	tail := p
	if len(tail) > limit {
		tail = tail[len(tail)-limit:]
	}
	if c.data.Len()+len(tail) > limit {
		c.data.Next(c.data.Len() + len(tail) - limit)
	}
	_, _ = c.data.Write(tail)
	if c.sink != nil {
		_, err := c.sink.Write(p)
		if err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func sessionPath(root, id string) (string, error) {
	if !sessionID.MatchString(id) {
		return "", fmt.Errorf("invalid session id")
	}
	return safePath(root, "build/debug/"+id+"/session.json")
}
func DebugStart(ctx context.Context, root, name string) (Session, error) {
	var token [8]byte
	if _, e := rand.Read(token[:]); e != nil {
		return Session{}, e
	}
	fp, e := Fingerprint(root)
	if e != nil {
		return Session{}, e
	}
	s := Session{SchemaVersion: 1, ID: time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(token[:]), Case: name, Head: GitVersion(ctx, root), Fingerprint: fp, Status: "unconfirmed", Hypotheses: []string{}, Runs: []DebugRun{}, Probes: []string{}}
	p, e := sessionPath(root, s.ID)
	if e != nil {
		return s, e
	}
	return s, WriteJSON(p, s)
}
func LoadSession(root, id string) (Session, string, error) {
	var s Session
	p, e := sessionPath(root, id)
	if e != nil {
		return s, p, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return s, p, e
	}
	e = json.Unmarshal(b, &s)
	return s, p, e
}
func DebugRunProfile(ctx context.Context, r *Runner, id, profile, phase string) error {
	s, p, e := LoadSession(r.Root, id)
	if e != nil {
		return e
	}
	c, ok := r.Config.DebugProfiles[profile]
	if !ok || len(c.Steps) == 0 {
		return fmt.Errorf("blocked: unknown or empty debug profile %q", profile)
	}
	profileBytes, _ := json.Marshal(c)
	profileHash := fmt.Sprintf("%x", sha256.Sum256(profileBytes))
	if s.Expected == "" || s.Actual == "" || len(s.Hypotheses) == 0 || s.FailurePattern == "" {
		return fmt.Errorf("record expected, actual and hypotheses in %s before reproduction", p)
	}
	failure, err := regexp.Compile(s.FailurePattern)
	if err != nil || failure.MatchString("") {
		return fmt.Errorf("failure_pattern must identify the specific failing assertion")
	}
	fp, e := Fingerprint(r.Root)
	if e != nil {
		return e
	}
	entry := DebugRun{Phase: phase, Profile: profile, ProfileHash: profileHash, Fingerprint: fp, Time: time.Now().UTC().Format(time.RFC3339)}
	if phase == "verify" {
		red := false
		for _, run := range s.Runs {
			if run.Phase == "reproduce" && run.Profile == profile && run.ProfileHash == profileHash && !run.Passed && run.FailureMatched && run.Fingerprint != fp {
				red = true
			}
		}
		if !red {
			return fmt.Errorf("blocked: verification needs a failed reproduction of the same profile on different source")
		}
	}
	// Raw process streams remain on the local terminal; no automatic trace upload or credential-bearing log file.
	captured := &debugCapture{sink: r.Out}
	child := *r
	child.Out = captured
	child.Err = captured
	for _, step := range c.Steps {
		if e = child.Step(ctx, step); e != nil {
			entry.Error = e.Error()
			break
		}
	}
	entry.Passed = e == nil
	entry.FailureMatched = !entry.Passed && failure.Match(captured.data.Bytes())
	s.Runs = append(s.Runs, entry)
	s.Status = "unconfirmed"
	if phase == "reproduce" && entry.FailureMatched {
		s.Status = "reproduced"
	}
	if phase == "verify" && entry.Passed {
		if len(s.Probes) > 0 {
			s.Status = "probe-cleanup-required"
		} else {
			s.Status = "verified-profile"
		}
	}
	if err := WriteJSON(p, s); err != nil {
		return err
	}
	return e
}
func DebugReport(root, id string, w io.Writer) error {
	s, p, e := LoadSession(root, id)
	if e != nil {
		return e
	}
	fp, e := Fingerprint(root)
	if e != nil {
		return e
	}
	if len(s.Runs) > 0 && s.Runs[len(s.Runs)-1].Fingerprint != fp {
		s.Status = "stale"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Debug case: %s\n\nStatus: **%s**\n\nExpected: %s\n\nActual: %s\n\n", s.Case, s.Status, s.Expected, s.Actual)
	for _, v := range s.Runs {
		fmt.Fprintf(&b, "- %s / %s: passed=%t; source `%s`\n", v.Phase, v.Profile, v.Passed, v.Fingerprint)
	}
	b.WriteString("\nA failed process alone does not establish the root cause. Confirm assertion failure, identical fixture, probe removal, affected gates and independent review before declaring the issue fixed.\n")
	if e = os.WriteFile(filepath.Join(filepath.Dir(p), "report.md"), []byte(b.String()), 0600); e != nil {
		return e
	}
	_, e = io.WriteString(w, b.String())
	return e
}
