package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PolicyCheck prevents a change from making its own coverage gate easier.
// An initial repository or first policy has no previous policy to ratchet.
func PolicyCheck(root, base string) error {
	current, err := Load(root)
	if err != nil {
		return err
	}
	ref, err := Base(root, base)
	if err != nil {
		return err
	}
	if ref == "EMPTY" {
		return nil
	}
	name, err := git(root, "ls-tree", "--name-only", ref, "--", "governance.json")
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return nil
	}
	data, err := git(root, "show", ref+":governance.json")
	if err != nil {
		return err
	}
	var previous Config
	if err := json.Unmarshal([]byte(data), &previous); err != nil {
		return fmt.Errorf("invalid base governance policy: %w", err)
	}
	if previous.SchemaVersion != 1 {
		return fmt.Errorf("unsupported base governance schema %d", previous.SchemaVersion)
	}
	if err := compareCoveragePolicy(previous.Coverage, current.Coverage); err != nil {
		return err
	}
	return compareCommandPolicy(previous, current, time.Now().UTC())
}

// compareCommandPolicy requires every step of a policy command that already
// existed to survive. Deleting or retargeting a gate therefore needs an
// explicit, owner-attributed, expiring migration entry instead of passing as an
// ordinary edit. A migration entry is only validated when it is actually
// needed, so a leftover entry cannot break later, unrelated changes.
func compareCommandPolicy(previous, current Config, now time.Time) error {
	declared := map[string]Migration{}
	for _, m := range current.Migrations {
		if strings.TrimSpace(m.Step) == "" {
			return fmt.Errorf("policy migration needs a step")
		}
		if _, duplicate := declared[m.Step]; duplicate {
			return fmt.Errorf("duplicate policy migration: %s", m.Step)
		}
		declared[m.Step] = m
	}
	require := func(key string) error {
		m, ok := declared[key]
		if !ok {
			return fmt.Errorf("policy step removed without an explicit migration: %s", key)
		}
		if strings.TrimSpace(m.Owner) == "" || strings.TrimSpace(m.Reason) == "" {
			return fmt.Errorf("policy migration %q needs an owner and reason", key)
		}
		expiry, err := time.Parse("2006-01-02", m.Expires)
		if err != nil || !now.Before(expiry) || expiry.Sub(now) > 30*24*time.Hour {
			return fmt.Errorf("policy migration %q needs an unexpired ISO expiry within 30 days: %q", key, m.Expires)
		}
		return nil
	}
	names := make([]string, 0, len(previous.Commands))
	for name := range previous.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		next, ok := current.Commands[name]
		if !ok {
			if err := require("command:" + name); err != nil {
				return err
			}
			continue
		}
		present := map[string]int{}
		for _, step := range next.Steps {
			present[stepIdentity(name, step)]++
		}
		for _, step := range previous.Commands[name].Steps {
			key := stepIdentity(name, step)
			if present[key] > 0 {
				present[key]--
				continue
			}
			if err := require(key); err != nil {
				return err
			}
		}
	}
	return nil
}

// stepIdentity keeps the executable shape of a step: its argument array, its
// working directory and its environment. Pinned versions written as
// `module@version`, as a `version-check --pattern` value or as a linker
// `Version=` assignment are masked, so upgrading a pin is an ordinary change
// while retargeting, silencing or deleting a step is not.
func stepIdentity(command string, step Step) string {
	identity := maskedArgv(step.Argv)
	if identity == "" {
		return fmt.Sprintf("%s (no argv)", command)
	}
	if step.Dir != "" {
		identity += " [dir=" + step.Dir + "]"
	}
	if len(step.Env) > 0 {
		keys := make([]string, 0, len(step.Env))
		for key := range step.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			identity += " [" + key + "=" + step.Env[key] + "]"
		}
	}
	return identity
}

func maskedArgv(argv []string) string {
	parts := make([]string, 0, len(argv))
	pattern := false
	for _, arg := range argv {
		switch {
		case pattern:
			arg = maskDigits(arg)
			pattern = false
		case arg == "--pattern":
			pattern = true
		case strings.HasPrefix(arg, "--pattern="):
			arg = "--pattern=" + maskDigits(strings.TrimPrefix(arg, "--pattern="))
		case !strings.HasPrefix(arg, "-"):
			if i := strings.LastIndex(arg, "@"); i > 0 {
				arg = arg[:i]
			}
		}
		if i := strings.Index(arg, "Version="); i >= 0 {
			arg = arg[:i] + "Version=" + maskDigits(arg[i+len("Version="):])
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

func maskDigits(value string) string {
	return versionDigits.ReplaceAllString(value, "#")
}

var versionDigits = regexp.MustCompile(`[0-9]+`)

func compareCoveragePolicy(previous, current CoverageConfig) error {
	if current.Format != previous.Format {
		return fmt.Errorf("coverage format cannot change without an explicit policy migration")
	}
	if previous.Inventory != "" && current.Inventory != previous.Inventory {
		return fmt.Errorf("coverage inventory cannot be removed or replaced without an explicit policy migration")
	}
	if current.Minimum < previous.Minimum || current.IncrementalMinimum < previous.IncrementalMinimum {
		return fmt.Errorf("coverage thresholds must not decrease relative to the base policy")
	}
	knownExclusions := make(map[string]bool, len(previous.Exclude))
	for _, excluded := range previous.Exclude {
		knownExclusions[excluded] = true
	}
	for _, excluded := range current.Exclude {
		if !knownExclusions[excluded] {
			return fmt.Errorf("new coverage exclusion requires an explicit policy migration: %s", excluded)
		}
	}
	for _, oldRoot := range previous.SourceRoots {
		covered := false
		oldRoot = strings.TrimSuffix(oldRoot, "/")
		for _, newRoot := range current.SourceRoots {
			newRoot = strings.TrimSuffix(newRoot, "/")
			if newRoot == "." || oldRoot == newRoot || strings.HasPrefix(oldRoot, newRoot+"/") {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("coverage source roots must not shrink: %s", oldRoot)
		}
	}
	if current.Baseline == nil {
		return nil
	}
	if previous.Baseline == nil {
		if current.Baseline.Minimum < previous.Minimum {
			return fmt.Errorf("an established policy cannot introduce a lower coverage baseline")
		}
		return nil
	}
	if current.Baseline.Minimum < previous.Baseline.Minimum {
		return fmt.Errorf("coverage baseline minimum must not decrease")
	}
	oldExpiry, oldErr := time.Parse("2006-01-02", previous.Baseline.Expires)
	newExpiry, newErr := time.Parse("2006-01-02", current.Baseline.Expires)
	if oldErr != nil || newErr != nil {
		return fmt.Errorf("coverage baseline expiry must be a valid ISO date")
	}
	if newExpiry.After(oldExpiry) {
		return fmt.Errorf("coverage baseline expiry must not be extended")
	}
	return nil
}

// VersionCheck proves that the executable runs and its actual version output
// matches the repository's pinned capability requirement.
func VersionCheck(ctx context.Context, argv []string, pattern string) error {
	version, err := regexp.Compile(pattern)
	if err != nil || version.MatchString("") {
		return fmt.Errorf("invalid or empty version requirement")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd, err := Process(ctx, argv)
	if err != nil {
		return err
	}
	var output versionOutput
	// The same comparable writer is assigned to both streams; os/exec serializes
	// writes to a shared output rather than concurrently mutating the buffer.
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("version capability command failed: %w", err)
	}
	if output.truncated {
		return fmt.Errorf("version output exceeds one MiB")
	}
	if !version.Match(output.buffer.Bytes()) {
		return fmt.Errorf("installed capability does not match required version pattern %q", pattern)
	}
	return nil
}

type versionOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (b *versionOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := (1 << 20) - b.buffer.Len()
	if n > remaining {
		b.truncated = true
		data = data[:remaining]
	}
	_, _ = b.buffer.Write(data)
	return n, nil
}
