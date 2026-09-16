package governance

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Runner struct {
	Root     string
	Config   Config
	Out, Err io.Writer
	Dispatch func(context.Context, string, []string) error
	active   map[string]bool
}

func (r *Runner) Run(ctx context.Context, name string, args []string) error {
	for _, dir := range []string{"build", "coverage", "mutation-results", "bin"} {
		p, err := safePath(r.Root, dir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}
	if r.active == nil {
		r.active = map[string]bool{}
	}
	if r.active[name] {
		return fmt.Errorf("recursive command: %s", name)
	}
	c, ok := r.Config.Commands[name]
	if !ok {
		return fmt.Errorf("blocked: command %q is not configured", name)
	}
	if len(c.Steps) == 0 {
		return fmt.Errorf("blocked: command %q has no steps", name)
	}
	r.active[name] = true
	defer delete(r.active, name)
	for i, s := range c.Steps {
		if len(args) > 0 && i == len(c.Steps)-1 {
			s.Argv = append(append([]string{}, s.Argv...), args...)
		}
		if e := r.Step(ctx, s); e != nil {
			return fmt.Errorf("%s step %d: %w", name, i+1, e)
		}
	}
	return nil
}
func (r *Runner) Step(ctx context.Context, s Step) error {
	if len(s.Argv) == 0 {
		return fmt.Errorf("empty command")
	}
	expand := func(v string) string { return strings.ReplaceAll(v, "{root}", r.Root) }
	argv := make([]string, len(s.Argv))
	for i, a := range s.Argv {
		argv[i] = expand(a)
	}
	seconds := s.TimeoutSeconds
	if seconds <= 0 {
		seconds = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	if argv[0] == "@ledger-tool" {
		if len(argv) < 2 || r.Dispatch == nil {
			return fmt.Errorf("invalid nested command")
		}
		return r.Dispatch(ctx, argv[1], argv[2:])
	}
	cmd, err := Process(ctx, argv)
	if err != nil {
		return err
	}
	cmd.Dir = r.Root
	if s.Dir != "" {
		cmd.Dir = expand(s.Dir)
		if !filepath.IsAbs(cmd.Dir) {
			cmd.Dir = filepath.Join(r.Root, cmd.Dir)
		}
	}
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+expand(v))
	}
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err
	fmt.Fprintf(r.Err, "+ %s\n", strings.Join(argv, " "))
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}
func Process(ctx context.Context, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty argv")
	}
	resolved, e := exec.LookPath(argv[0])
	if e != nil {
		return nil, fmt.Errorf("blocked: %w", e)
	}
	cmd, e := platformProcess(ctx, resolved, argv[1:])
	if e != nil {
		return nil, e
	}
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	return cmd, nil
}
