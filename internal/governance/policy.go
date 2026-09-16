package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	return compareCoveragePolicy(previous.Coverage, current.Coverage)
}

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
