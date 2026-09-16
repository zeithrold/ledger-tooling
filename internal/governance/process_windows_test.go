package governance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsBatchArguments(t *testing.T) {
	r := t.TempDir()
	path := filepath.Join(r, "SDK path", "fixture.cmd")
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("@echo off\r\necho [%~1]\r\necho [%~2]\r\nexit /b 0\r\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cmd, e := Process(context.Background(), []string{path, "a b", "literal$()"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v: %s", e, b)
	}
	if strings.ReplaceAll(string(b), "\r\n", "\n") != "[a b]\n[literal$()]\n" {
		t.Fatalf("batch argv altered: %q", b)
	}
	for _, arg := range []string{`"quote`, "%PATH%", "!VAR!", "a&b", "a|b", "a<b", "a>b", "a^b", "line\nnext", "line\rnext"} {
		if _, e := Process(context.Background(), []string{path, arg}); e == nil {
			t.Errorf("accepted cmd expansion %q", arg)
		}
	}
}
func TestWindowsBatchCancellation(t *testing.T) {
	r := t.TempDir()
	path := filepath.Join(r, "wait.cmd")
	if e := os.WriteFile(path, []byte("@echo off\r\nping -n 30 127.0.0.1 >NUL\r\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cmd, e := Process(ctx, []string{path})
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if e = cmd.Run(); e == nil {
		t.Fatal("cancelled batch passed")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("batch process tree survived cancellation")
	}
}
