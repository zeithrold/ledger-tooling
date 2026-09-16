package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveEntry struct {
	name, body string
	kind       byte
	size       int64
}

func archiveBytes(t *testing.T, entries []archiveEntry, truncate bool) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		typ := entry.kind
		if typ == 0 {
			typ = tar.TypeReg
		}
		size := int64(len(entry.body))
		if entry.size != 0 {
			size = entry.size
		}
		if typ == tar.TypeDir || typ == tar.TypeSymlink || typ == tar.TypeLink {
			size = 0
		}
		if e := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: size, Typeflag: typ, Linkname: "outside"}); e != nil {
			t.Fatal(e)
		}
		if entry.body != "" {
			if _, e := tw.Write([]byte(entry.body)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if !truncate {
		if e := tw.Close(); e != nil {
			t.Fatal(e)
		}
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func writeFixture(t *testing.T, r, p string, b []byte) {
	t.Helper()
	path := filepath.Join(r, p)
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func lockArchive(t *testing.T, r string, b []byte) {
	t.Helper()
	writeFixture(t, r, "tool/bundle.tar.gz", b)
	sum := sha256.Sum256(b)
	value := lock{Version: "v0.1.0", Source: "tool/bundle.tar.gz", SHA256: hex.EncodeToString(sum[:])}
	data, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	writeFixture(t, r, "tool/tooling.lock.json", data)
}
func TestBootstrapMissingAndInvalidLock(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	if e := run(); e == nil {
		t.Fatal("missing lock passed")
	}
	writeFixture(t, r, "tool/tooling.lock.json", []byte("{"))
	if e := run(); e == nil {
		t.Fatal("malformed lock passed")
	}
	for _, value := range []lock{{}, {Version: "v1", Source: "../escape", SHA256: strings.Repeat("a", 64)}, {Version: "v1", Source: "/absolute", SHA256: strings.Repeat("a", 64)}, {Version: "v1", Source: "a\\b", SHA256: strings.Repeat("a", 64)}, {Version: "v1", Source: "missing", SHA256: strings.Repeat("a", 64)}} {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		writeFixture(t, r, "tool/tooling.lock.json", b)
		if e = run(); e == nil {
			t.Fatal("invalid lock passed", value)
		}
	}
}
func TestBootstrapArchiveRejections(t *testing.T) {
	cases := []struct {
		name     string
		entries  []archiveEntry
		truncate bool
		want     string
	}{
		{"traversal", []archiveEntry{{name: "../escaped", body: "bad"}}, false, "escapes root"},
		{"absolute", []archiveEntry{{name: "/escaped", body: "bad"}}, false, "unsafe archive path"},
		{"backslash", []archiveEntry{{name: "a\\escaped", body: "bad"}}, false, "unsafe archive path"},
		{"symlink", []archiveEntry{{name: "link", kind: tar.TypeSymlink}}, false, "unsupported archive entry"},
		{"hardlink", []archiveEntry{{name: "link", kind: tar.TypeLink}}, false, "unsupported archive entry"},
		{"duplicate", []archiveEntry{{name: "same", body: "one"}, {name: "same", body: "two"}}, false, "exists"},
		{"oversized", []archiveEntry{{name: "large", size: 33 << 20}}, true, "size limit"},
		{"truncated", []archiveEntry{{name: "partial", body: "a", size: 100}}, true, "unexpected EOF"},
		{"parent-file", []archiveEntry{{name: "parent", body: "file"}, {name: "parent/child", body: "bad"}}, false, ""},
		{"directory-over-file", []archiveEntry{{name: "parent", body: "file"}, {name: "parent", kind: tar.TypeDir}}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := t.TempDir()
			t.Chdir(r)
			lockArchive(t, r, archiveBytes(t, tc.entries, tc.truncate))
			e := run()
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q got %v", tc.want, e)
			}
		})
	}
}
func TestBootstrapChecksumAndInvalidArchive(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	lockArchive(t, r, []byte("not gzip"))
	if e := run(); e == nil {
		t.Fatal("non-gzip passed")
	}
	lockArchive(t, r, archiveBytes(t, nil, false))
	writeFixture(t, r, "tool/bundle.tar.gz", []byte("tampered"))
	if e := run(); e == nil || !strings.Contains(e.Error(), "checksum mismatch") {
		t.Fatal(e)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, e := gz.Write([]byte("not tar")); e != nil {
		t.Fatal(e)
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	lockArchive(t, r, compressed.Bytes())
	if e := run(); e == nil {
		t.Fatal("invalid tar passed")
	}
}
func TestBootstrapBuildFailure(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	lockArchive(t, r, archiveBytes(t, []archiveEntry{{name: "go.mod", body: "module test.invalid/tool\n\ngo 1.26.2\n"}, {name: "cmd/ledger-tool/main.go", body: "not valid Go"}}, false))
	if e := run(); e == nil {
		t.Fatal("invalid tool build passed")
	}
}
func TestToolchainRequirement(t *testing.T) {
	dir := t.TempDir()
	if moduleGoVersion(dir) != "" {
		t.Fatal("missing go.mod reported a requirement")
	}
	writeFixture(t, dir, "go.mod", []byte("module test.invalid/tool\n\ngo 1.26.6\n\ntoolchain go1.26.6\n"))
	if got := moduleGoVersion(dir); got != "1.26.6" {
		t.Fatal(got)
	}
	writeFixture(t, dir, "go.mod", []byte("module test.invalid/tool\n\ngo 1.21.0\n\ntoolchain go1.26.6\n"))
	if got := moduleGoVersion(dir); got != "1.26.6" {
		t.Fatal("newer toolchain directive ignored:", got)
	}
	writeFixture(t, dir, "go.mod", []byte("module test.invalid/tool\n\ngo 1.27.0\n\ntoolchain go1.26.6\n"))
	if got := moduleGoVersion(dir); got != "1.27.0" {
		t.Fatal("newer go directive ignored:", got)
	}
	writeFixture(t, dir, "go.mod", []byte("module test.invalid/tool\n\ngo 1.26.2\n"))
	if got := moduleGoVersion(dir); got != "1.26.2" {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		local, required string
		older           bool
	}{{"go1.26.6", "1.26.6", false}, {"go1.26.5", "1.26.6", true}, {"go1.27.0", "1.26.6", false}, {"go1.25", "1.26.6", true}, {"go1.26.10", "1.26.6", false}} {
		if olderThan(tc.local, tc.required) != tc.older {
			t.Fatalf("%s vs %s", tc.local, tc.required)
		}
	}
	if e := requireToolchain(""); e != nil {
		t.Fatal(e)
	}
	if e := requireToolchain("1.0.0"); e != nil {
		t.Fatal("newer local toolchain rejected:", e)
	}
	t.Setenv("GOTOOLCHAIN", "local")
	if e := requireToolchain("1.0.0"); e != nil {
		t.Fatal("satisfied requirement rejected:", e)
	}
	e := requireToolchain("99.0.0")
	if e == nil || !strings.Contains(e.Error(), "blocked") || !strings.Contains(e.Error(), "99.0.0") {
		t.Fatal("impossible offline requirement accepted:", e)
	}
}

func TestBootstrapBlocksUnavailableToolchain(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	t.Setenv("GOTOOLCHAIN", "local")
	lockArchive(t, r, archiveBytes(t, []archiveEntry{{name: "go.mod", body: "module test.invalid/tool\n\ngo 99.0.0\n"}, {name: "cmd/ledger-tool/main.go", body: "package main\nfunc main(){}\n"}}, false))
	e := run()
	if e == nil || !strings.Contains(e.Error(), "blocked") || !strings.Contains(e.Error(), "go 99.0.0") {
		t.Fatalf("want an actionable blocked toolchain error, got %v", e)
	}
}

func TestBootstrapSuccessPreservesArgv(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	out := filepath.Join(r, "args.json")
	t.Setenv("LEDGER_BOOTSTRAP_TEST_OUTPUT", out)
	entries := []archiveEntry{{name: "cmd", kind: tar.TypeDir}, {name: "go.mod", body: "module test.invalid/tool\n\ngo 1.26.2\n"}, {name: "cmd/ledger-tool/main.go", body: `package main
import("encoding/json";"os")
func main(){b,e:=json.Marshal(os.Args[1:]);if e!=nil{panic(e)};if e=os.WriteFile(os.Getenv("LEDGER_BOOTSTRAP_TEST_OUTPUT"),b,0600);e!=nil{panic(e)}}
`}}
	lockArchive(t, r, archiveBytes(t, entries, false))
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"bootstrap", "command", "a b", "$(touch forbidden)", "--flag=value"}
	main()
	b, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	var args []string
	if e = json.Unmarshal(b, &args); e != nil {
		t.Fatal(e)
	}
	want := []string{"--root", r, "command", "a b", "$(touch forbidden)", "--flag=value"}
	if len(args) != len(want) {
		t.Fatal(args)
	}
	for i, v := range want {
		if args[i] != v {
			t.Fatalf("argument %d: %q != %q", i, args[i], v)
		}
	}
	if _, e = os.Stat(filepath.Join(r, "forbidden")); !os.IsNotExist(e) {
		t.Fatal("shell expansion executed")
	}
}
func TestBootstrapPropagatesToolFailure(t *testing.T) {
	r := t.TempDir()
	t.Chdir(r)
	lockArchive(t, r, archiveBytes(t, []archiveEntry{{name: "go.mod", body: "module test.invalid/tool\n\ngo 1.26.2\n"}, {name: "cmd/ledger-tool/main.go", body: "package main\nimport \"os\"\nfunc main(){os.Exit(23)}\n"}}, false))
	if e := run(); e == nil {
		t.Fatal("failed child passed")
	}
}
