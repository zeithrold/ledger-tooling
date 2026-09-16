package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zeithrold/ledger-tooling/internal/currency"
	g "github.com/zeithrold/ledger-tooling/internal/governance"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

func option(args []string, key string) (string, []string) {
	for i, a := range args {
		if a == "--" {
			break
		}
		if a == key && i+1 < len(args) {
			return args[i+1], append(args[:i:i], args[i+2:]...)
		}
	}
	return "", args
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	root, args := option(args, "--root")
	if root == "" {
		root = "."
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: ledger-tool [--root path] command [arguments]")
	}
	if args[0] == "version" {
		fmt.Println("ledger-tooling 0.1.0")
		return nil
	}
	c, e := g.Load(root)
	if e != nil {
		return e
	}
	r := &g.Runner{Root: root, Config: c, Out: os.Stdout, Err: os.Stderr}
	var dispatch func(context.Context, string, []string) error
	dispatch = func(ctx context.Context, name string, args []string) error {
		switch name {
		case "policy-check":
			base, _ := option(args, "--base")
			if base == "" {
				base = os.Getenv("LEDGER_BASE")
				if base == "" {
					base = os.Getenv("LEDGER_BASE_REF")
				}
			}
			return g.PolicyCheck(root, base)
		case "version-check":
			pattern, args := option(args, "--pattern")
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			return g.VersionCheck(ctx, args, pattern)
		case "bundle":
			return g.Bundle(root, args)
		case "mutation-check":
			p, _ := option(args, "--report")
			return g.MutationCheck(root, p)
		case "currency":
			return currency.Run(root, args)
		case "fingerprint":
			v, e := g.Fingerprint(root)
			if e == nil {
				fmt.Println(v)
			}
			return e
		case "doctor":
			if _, ok := c.Commands["doctor"]; ok {
				return r.Run(ctx, name, args)
			}
			return g.Doctor(ctx, r)
		case "commit-check":
			return g.CommitCheck(strings.Join(args, " "))
		case "skills-check":
			templates, args := option(args, "--templates")
			if len(args) > 0 {
				return fmt.Errorf("skills-check: unexpected argument %q", args[0])
			}
			return g.SkillsCheck(root, templates)
		case "changes":
			base, args := option(args, "--base")
			out, rest := option(args, "--github-output")
			if len(rest) > 0 {
				return fmt.Errorf("changes: unexpected argument %q", rest[0])
			}
			v, e := g.Classify(root, base, c)
			if e != nil {
				return e
			}
			if out != "" {
				if e = g.WriteOutput(out, v); e != nil {
					return e
				}
			}
			return json.NewEncoder(os.Stdout).Encode(v)
		case "coverage-check":
			base, _ := option(args, "--base")
			if base == "" {
				base = os.Getenv("LEDGER_BASE")
				if base == "" {
					base = os.Getenv("LEDGER_BASE_REF")
				}
			}
			if e := g.PolicyCheck(root, base); e != nil {
				return e
			}
			v, e := g.Coverage(root, c.Coverage, base, time.Now().UTC())
			if w := g.WriteJSON(filepath.Join(root, "build", "governance", "coverage.json"), v); w != nil {
				return w
			}
			_ = json.NewEncoder(os.Stdout).Encode(v)
			return e
		case "review-check":
			p, _ := option(args, "--file")
			return g.ReviewCheck(root, p)
		case "generate-check", "isolated-run":
			output, args := option(args, "--output")
			var includes []string
			for {
				p, next := option(args, "--include")
				args = next
				if p == "" {
					break
				}
				includes = append(includes, p)
			}
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			if len(args) == 0 {
				return fmt.Errorf("missing isolated command")
			}
			if name == "generate-check" && output == "" {
				return fmt.Errorf("missing --output")
			}
			return g.Isolated(ctx, r, includes, output, args)
		case "debug-start":
			if len(args) != 1 {
				return fmt.Errorf("debug-start CASE")
			}
			s, e := g.DebugStart(ctx, root, args[0])
			if e == nil {
				fmt.Println(s.ID)
			}
			return e
		case "debug-run", "debug-verify":
			if len(args) != 2 {
				return fmt.Errorf("%s SESSION PROFILE", name)
			}
			phase := "reproduce"
			if name == "debug-verify" {
				phase = "verify"
			}
			return g.DebugRunProfile(ctx, r, args[0], args[1], phase)
		case "debug-report":
			if len(args) != 1 {
				return fmt.Errorf("debug-report SESSION")
			}
			return g.DebugReport(root, args[0], os.Stdout)
		case "ui-report":
			p, _ := option(args, "--manifest")
			return g.UIReport(root, p)
		default:
			return r.Run(ctx, name, args)
		}
	}
	r.Dispatch = dispatch
	return dispatch(ctx, args[0], args[1:])
}
