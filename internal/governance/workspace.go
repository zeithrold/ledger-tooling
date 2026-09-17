package governance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func CopyWorkspace(root, target string, includes []string) error {
	files, e := SourceFiles(root)
	if e != nil {
		return e
	}
	for _, f := range files {
		secret := false
		for _, segment := range strings.Split(filepath.ToSlash(f), "/") {
			if strings.HasPrefix(segment, ".env") {
				secret = true
			}
		}
		if secret {
			continue
		}
		if strings.HasPrefix(f, ".env") || strings.HasPrefix(f, ".git/") || strings.HasPrefix(f, "build/") || strings.HasPrefix(f, "coverage/") || strings.HasPrefix(f, "bin/") {
			continue
		}
		if len(includes) > 0 {
			allowed := f == "go.mod" || f == "go.sum"
			for _, p := range includes {
				if f == p || strings.HasPrefix(f, strings.TrimSuffix(p, "/")+"/") {
					allowed = true
				}
			}
			if !allowed {
				continue
			}
		}
		src, e := safePath(root, f)
		if e != nil {
			return e
		}
		info, e := os.Lstat(src)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular workspace file: %s", f)
		}
		b, e := os.ReadFile(src)
		if e != nil {
			return e
		}
		dest := filepath.Join(target, f)
		if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return e
		}
		if e = os.WriteFile(dest, b, info.Mode().Perm()); e != nil {
			return e
		}
	}
	return nil
}
func CompareTree(a, b string) error {
	left := map[string][]byte{}
	right := map[string][]byte{}
	for dir, m := range map[string]map[string][]byte{a: left, b: right} {
		info, e := os.Stat(dir)
		if e != nil {
			return e
		}
		if !info.IsDir() {
			data, e := os.ReadFile(dir)
			if e != nil {
				return e
			}
			m["."] = data
			continue
		}
		e = filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			// WalkDir only yields paths under dir, so Rel cannot fail here.
			rel, _ := filepath.Rel(dir, p)
			// Slash-form keys keep generate-check stable across Windows and Unix.
			data, e := os.ReadFile(p)
			m[filepath.ToSlash(rel)] = data
			return e
		})
		if e != nil {
			return e
		}
	}
	if len(left) != len(right) {
		return fmt.Errorf("generated file set differs")
	}
	for p, data := range left {
		// CRLF from a Windows checkout or tool rewrite is not content drift.
		if !bytes.Equal(normalizeEndings(data), normalizeEndings(right[p])) {
			return fmt.Errorf("generated output differs: %s", p)
		}
	}
	return nil
}
func Isolated(ctx context.Context, r *Runner, includes []string, output string, argv []string) error {
	tmp, e := os.MkdirTemp("", "ledger-isolated-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	if e = CopyWorkspace(r.Root, tmp, includes); e != nil {
		return e
	}
	child := &Runner{Root: tmp, Out: r.Out, Err: r.Err}
	if e = child.Step(ctx, Step{Argv: argv, TimeoutSeconds: 1800}); e != nil {
		return e
	}
	if output != "" {
		old, e := safePath(r.Root, output)
		if e != nil {
			return e
		}
		next, e := safePath(tmp, output)
		if e != nil {
			return e
		}
		return CompareTree(old, next)
	}
	return nil
}
func Doctor(ctx context.Context, r *Runner) error {
	tools := r.Config.Tools
	if len(tools) == 0 {
		tools = map[string][]string{"go": {"go", "version"}, "just": {"just", "--version"}}
	}
	var failures []string
	for name, argv := range tools {
		if e := r.Step(ctx, Step{Argv: argv, TimeoutSeconds: 30}); e != nil {
			failures = append(failures, name+": "+e.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("blocked capabilities: %s", strings.Join(failures, "; "))
	}
	return nil
}
func WriteOutput(path string, c Changes) error {
	file, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer file.Close()
	_, e = fmt.Fprintf(file, "code=%t\nbackend=%t\nui=%t\nvisual=%t\npolicy=%t\nmutation=%t\nfuzz=%t\nnative=%t\ndocs_only=%t\n", c.Code, c.Backend, c.UI, c.Visual, c.Policy, c.Mutation, c.Fuzz, c.Native, c.DocsOnly)
	return e
}

var _ io.Writer
