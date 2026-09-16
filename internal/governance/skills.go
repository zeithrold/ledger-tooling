package governance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillsCheck requires every shared project skill to be byte-identical to the
// template shipped in the pinned bundle. Consumers copy the template into
// `.agents/skills/<name>/SKILL.md`; without this comparison the copies drift
// silently and each repository documents a different procedure. Templates must
// stay flat: a nested template is a configuration error rather than a file that
// is silently never enforced. Line endings are not drift, because a Windows
// checkout may materialise the same content as CRLF.
func SkillsCheck(root, templates string) error {
	dir, err := templateDir(templates)
	if err != nil {
		return err
	}
	checked := 0
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if filepath.Ext(rel) != ".md" {
			return nil
		}
		if strings.Contains(rel, "/") {
			return fmt.Errorf("nested skill template is not supported: %s", rel)
		}
		name := strings.TrimSuffix(rel, ".md")
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("invalid skill template name: %s", rel)
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target, err := safePath(root, ".agents/skills/"+name+"/SKILL.md")
		if err != nil {
			return err
		}
		got, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("skill copy missing for template %s: %w", rel, err)
		}
		if !bytes.Equal(normalizeEndings(got), normalizeEndings(want)) {
			return fmt.Errorf("skill copy has drifted from the pinned template: %s", rel)
		}
		checked++
		return nil
	})
	if err != nil {
		return err
	}
	if checked == 0 {
		return fmt.Errorf("no skill templates available in %s", dir)
	}
	return nil
}

func normalizeEndings(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// templateDir prefers an explicit path and otherwise locates the templates
// beside the executable, which is where the pinned bundle unpacks them.
func templateDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(exe), "templates", "skills")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("skill templates unavailable: run through the pinned bundle or pass --templates")
}
