package governance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsCheck(t *testing.T) {
	root := t.TempDir()
	templates := t.TempDir()
	put(t, templates, "one.md", "template body\nsecond line\n")
	put(t, templates, "notes.txt", "not a template\n")
	put(t, templates, "nested/notes.txt", "not a template either\n")
	if e := SkillsCheck(root, templates); e == nil {
		t.Fatal("missing skill copy passed")
	}
	put(t, root, ".agents/skills/one/SKILL.md", "template body\nsecond line\n")
	if e := SkillsCheck(root, templates); e != nil {
		t.Fatal(e)
	}
	put(t, root, ".agents/skills/one/SKILL.md", "template body\nsecond line drifted\n")
	if e := SkillsCheck(root, templates); e == nil {
		t.Fatal("drifted skill copy passed")
	}
	put(t, root, ".agents/skills/one/SKILL.md", "template body\r\nsecond line\r\n")
	if e := SkillsCheck(root, templates); e != nil {
		t.Fatal("a CRLF copy of the same content must not count as drift:", e)
	}
	put(t, root, ".agents/skills/one/SKILL.md", "template body\nsecond line\n")
	put(t, root, ".agents/skills/extra/SKILL.md", "consumer-only skill\n")
	if e := SkillsCheck(root, templates); e != nil {
		t.Fatal("consumer-only skill rejected:", e)
	}
	put(t, templates, "nested/two.md", "unreachable template\n")
	if e := SkillsCheck(root, templates); e == nil {
		t.Fatal("nested template silently ignored")
	}
	if e := os.Remove(filepath.Join(templates, "nested", "two.md")); e != nil {
		t.Fatal(e)
	}
	empty := t.TempDir()
	if e := SkillsCheck(root, empty); e == nil {
		t.Fatal("empty template set passed")
	}
	if e := SkillsCheck(root, filepath.Join(empty, "absent")); e == nil {
		t.Fatal("missing template directory passed")
	}
	if _, e := templateDir(""); e == nil {
		t.Fatal("template discovery succeeded without a bundle")
	}
	if dir, e := templateDir(templates); e != nil || dir != templates {
		t.Fatal(dir, e)
	}
}
