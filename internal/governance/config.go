package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	SchemaVersion int                 `json:"schema_version"`
	Kind          string              `json:"kind"`
	Commands      map[string]Command  `json:"commands"`
	Coverage      CoverageConfig      `json:"coverage"`
	Tools         map[string][]string `json:"tools,omitempty"`
	DebugProfiles map[string]Command  `json:"debug_profiles,omitempty"`
}
type Command struct {
	Description string `json:"description,omitempty"`
	Steps       []Step `json:"steps"`
}
type Step struct {
	Argv           []string          `json:"argv"`
	Env            map[string]string `json:"env,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}
type Baseline struct {
	Started string  `json:"started"`
	Minimum float64 `json:"minimum"`
	Expires string  `json:"expires"`
	Reason  string  `json:"reason"`
}
type CoverageConfig struct {
	Format             string    `json:"format"`
	Profiles           []string  `json:"profiles"`
	SourceRoots        []string  `json:"source_roots"`
	Exclude            []string  `json:"exclude"`
	Inventory          string    `json:"inventory,omitempty"`
	Minimum            float64   `json:"minimum"`
	IncrementalMinimum float64   `json:"incremental_minimum"`
	Baseline           *Baseline `json:"baseline,omitempty"`
}

func Load(root string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(root, "governance.json"))
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.SchemaVersion != 1 {
		return c, fmt.Errorf("unsupported governance schema %d", c.SchemaVersion)
	}
	return c, nil
}
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func safePath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("expected relative slash path: %q", name)
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", name)
	}
	resolvedRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	ancestor := p
	for {
		if _, e = os.Lstat(ancestor); e == nil {
			break
		}
		next := filepath.Dir(ancestor)
		if next == ancestor {
			return "", e
		}
		ancestor = next
	}
	resolved, e := filepath.EvalSymlinks(ancestor)
	if e != nil {
		return "", e
	}
	r, e := filepath.Rel(resolvedRoot, resolved)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("symlink escapes root: %q", name)
	}
	return p, nil
}
