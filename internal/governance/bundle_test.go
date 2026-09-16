package governance

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBundle(t *testing.T) {
	src := t.TempDir()
	gitOK(t, src, "init", "-q")
	dest := t.TempDir()
	write := func(root, p, s string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755)
		if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(src, "go.mod", "module example\n")
	write(src, "bootstrap/bootstrap.go", "package main\n")
	write(src, "cmd/main.go", "package main\n")
	write(src, "build/secrets.go", "excluded\n")
	write(src, "secret.txt", "not source\n")
	write(src, ".gitignore", "private.go\n")
	write(src, "private.go", "must not ship\n")
	write(dest, "governance.json", "{}")
	if Bundle(src, nil) == nil {
		t.Fatal("missing destinations")
	}
	if Bundle(src, []string{t.TempDir()}) == nil {
		t.Fatal("unconfigured checkout")
	}
	if e := Bundle(src, []string{dest}); e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(dest, "tool/ledger-tooling-v0.1.0.tar.gz")
	data, e := os.ReadFile(archive)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	var lock map[string]string
	b, _ := os.ReadFile(filepath.Join(dest, "tool/tooling.lock.json"))
	if e = json.Unmarshal(b, &lock); e != nil || lock["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal(e, lock)
	}
	f, e := os.Open(archive)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	n := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if h.Name == "build/secrets.go" {
			t.Fatal("build included")
		}
		n++
	}
	if n != 3 {
		t.Fatal(n)
	}
	if e = Bundle(src, []string{dest}); e != nil {
		t.Fatal(e)
	}
	next, _ := os.ReadFile(archive)
	if string(next) != string(data) {
		t.Fatal("nondeterministic archive")
	}
	empty := t.TempDir()
	if Bundle(empty, []string{dest}) == nil {
		t.Fatal("missing bootstrap")
	}
}

func TestBundleFailureBoundaries(t *testing.T) {
	src := t.TempDir()
	gitOK(t, src, "init", "-q")
	put(t, src, "go.mod", "module example\n")
	put(t, src, "bootstrap/bootstrap.go", "package main\n")
	dest := t.TempDir()
	put(t, dest, "governance.json", "{}")
	if e := Bundle(filepath.Join(src, "missing"), []string{dest}); e == nil {
		t.Fatal("missing source passed")
	}
	put(t, dest, "tool", "file")
	if e := Bundle(src, []string{dest}); e == nil {
		t.Fatal("non-directory destination tool passed")
	}
	if e := os.Remove(filepath.Join(dest, "tool")); e != nil {
		t.Fatal(e)
	}
	for _, blocked := range []string{"ledger-tooling-v0.1.0.tar.gz", "bootstrap.go", "tooling.lock.json"} {
		t.Run(blocked, func(t *testing.T) {
			d := t.TempDir()
			put(t, d, "governance.json", "{}")
			if e := os.MkdirAll(filepath.Join(d, "tool", blocked), 0755); e != nil {
				t.Fatal(e)
			}
			if e := Bundle(src, []string{d}); e == nil {
				t.Fatal("directory used as output file")
			}
		})
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(dest, "tool")); e == nil {
		if e = Bundle(src, []string{dest}); e == nil {
			t.Fatal("bundle escaped destination through symlink")
		}
	}
	if e := os.Symlink(filepath.Join(src, "bootstrap/bootstrap.go"), filepath.Join(src, "linked.go")); e == nil {
		if e = Bundle(src, []string{dest}); e == nil {
			t.Fatal("bundled source symlink")
		}
	}
}

func TestBundleOutputSymlink(t *testing.T) {
	src := t.TempDir()
	gitOK(t, src, "init", "-q")
	put(t, src, "go.mod", "module example\n")
	put(t, src, "bootstrap/bootstrap.go", "package main\n")
	for _, name := range []string{"bootstrap.go", "tooling.lock.json", "ledger-tooling-v0.1.0.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			outside := t.TempDir()
			put(t, dest, "governance.json", "{}")
			put(t, outside, "sentinel", "unchanged")
			if e := os.MkdirAll(filepath.Join(dest, "tool"), 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(filepath.Join(outside, "sentinel"), filepath.Join(dest, "tool", name)); e != nil {
				t.Skip("symlink capability unavailable")
			}
			if e := Bundle(src, []string{dest}); e == nil {
				t.Fatal("escaped output")
			}
			data, e := os.ReadFile(filepath.Join(outside, "sentinel"))
			if e != nil || string(data) != "unchanged" {
				t.Fatal("outside file modified", e)
			}
		})
	}
}
