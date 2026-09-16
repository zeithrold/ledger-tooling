package governance

import (
	"testing"
	"time"
)

// ratchetBase is a policy whose steps exercise every pinned-version form the
// ratchet must tolerate and every retargeting form it must reject.
func ratchetBase() Config {
	return Config{SchemaVersion: 1, Kind: "go", Commands: map[string]Command{
		"doctor":        {Steps: []Step{{Argv: []string{"@ledger-tool", "version-check", "--pattern", `(?m)^go version go1\.26\.6 `, "--", "go", "version"}}}},
		"doctor-inline": {Steps: []Step{{Argv: []string{"@ledger-tool", "version-check", "--pattern=(?m)^Flutter 3\\.41\\.7 ", "--", "flutter", "--version"}}}},
		"security": {Steps: []Step{{Argv: []string{
			"go", "run", "-ldflags=-X example.test/v8/version.Version=8.30.1", "example.test/scanner@v8.30.1", "dir", ".",
		}}}},
		"check": {Steps: []Step{{Argv: []string{"go", "test", "-race", "./..."}}, {}}},
	}}
}

func TestPolicyStepIdentityAndMigrations(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	run := func(name string, change func(*Config), wantError bool) {
		t.Run(name, func(t *testing.T) {
			previous := ratchetBase()
			current := ratchetBase()
			change(&current)
			if e := compareCommandPolicy(previous, current, now); (e != nil) != wantError {
				t.Fatal(e)
			}
		})
	}
	set := func(c *Config, name string, argv ...string) {
		c.Commands[name] = Command{Steps: []Step{{Argv: argv}}}
	}
	migration := func(key, expires string) Migration {
		return Migration{Step: key, Owner: "maintainer", Reason: "recorded gate change", Expires: expires}
	}
	dropFirstCheckStep := func(c *Config) {
		steps := c.Commands["check"].Steps
		c.Commands["check"] = Command{Steps: []Step{steps[1]}}
	}
	dropEmptyCheckStep := func(c *Config) {
		steps := c.Commands["check"].Steps
		c.Commands["check"] = Command{Steps: []Step{steps[0]}}
	}

	run("pattern-version-bump", func(c *Config) {
		set(c, "doctor", "@ledger-tool", "version-check", "--pattern", `(?m)^go version go1\.27\.0 `, "--", "go", "version")
	}, false)
	run("inline-pattern-version-bump", func(c *Config) {
		set(c, "doctor-inline", "@ledger-tool", "version-check", "--pattern=(?m)^Flutter 3\\.42\\.0 ", "--", "flutter", "--version")
	}, false)
	run("pattern-shape-change", func(c *Config) {
		set(c, "doctor", "@ledger-tool", "version-check", "--pattern", `(?m)^go version `, "--", "go", "version")
	}, true)
	run("scanner-release-bump", func(c *Config) {
		set(c, "security", "go", "run", "-ldflags=-X example.test/v8/version.Version=8.31.0", "example.test/scanner@v8.31.0", "dir", ".")
	}, false)
	run("module-major-change", func(c *Config) {
		set(c, "security", "go", "run", "-ldflags=-X example.test/v9/version.Version=8.30.1", "example.test/scanner@v9.0.0", "dir", ".")
	}, true)
	run("dir-retarget", func(c *Config) {
		steps := c.Commands["check"].Steps
		steps[0].Dir = "internal"
		c.Commands["check"] = Command{Steps: steps}
	}, true)
	run("env-retarget", func(c *Config) {
		steps := c.Commands["check"].Steps
		steps[0].Env = map[string]string{"GOFLAGS": "-run=^$"}
		c.Commands["check"] = Command{Steps: steps}
	}, true)
	run("unused-expired-migration", func(c *Config) {
		c.Migrations = []Migration{migration("go test ./removed/...", "2026-09-15")}
	}, false)
	run("unused-future-migration", func(c *Config) {
		c.Migrations = []Migration{migration("go test ./removed/...", "2026-10-01")}
	}, false)
	run("duplicate-migration", func(c *Config) {
		c.Migrations = []Migration{migration("go test -race ./...", "2026-10-01"), migration("go test -race ./...", "2026-10-02")}
	}, true)
	run("used-expired-migration", func(c *Config) {
		dropFirstCheckStep(c)
		c.Migrations = []Migration{migration("go test -race ./...", "2026-09-15")}
	}, true)
	run("used-future-migration", func(c *Config) {
		dropFirstCheckStep(c)
		c.Migrations = []Migration{migration("go test -race ./...", "2026-10-01")}
	}, false)
	run("empty-step-removal-without-migration", dropEmptyCheckStep, true)
	run("empty-step-removal-with-migration", func(c *Config) {
		dropEmptyCheckStep(c)
		c.Migrations = []Migration{migration("check (no argv)", "2026-10-01")}
	}, false)
}

func TestStepIdentityMasksOnlyPins(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		command string
		index   int
		step    Step
		same    bool
	}{
		{name: "module release", command: "security", step: Step{Argv: []string{
			"go", "run", "-ldflags=-X example.test/v8/version.Version=8.99.0", "example.test/scanner@v8.99.0", "dir", ".",
		}}, same: true},
		{name: "pattern digits", command: "doctor", step: Step{Argv: []string{"@ledger-tool", "version-check", "--pattern", `(?m)^go version go1\.99\.0 `, "--", "go", "version"}}, same: true},
		{name: "pattern shape", command: "doctor", step: Step{Argv: []string{"@ledger-tool", "version-check", "--pattern", `(?m)^go version `, "--", "go", "version"}}, same: false},
		{name: "subcommand", command: "check", step: Step{Argv: []string{"go", "vet", "-race", "./..."}}, same: false},
		{name: "recursive target", command: "check", step: Step{Argv: []string{"go", "test", "-race", "./internal/..."}}, same: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := ratchetBase()
			current := ratchetBase()
			steps := current.Commands[tc.command].Steps
			steps[tc.index] = tc.step
			current.Commands[tc.command] = Command{Steps: steps}
			if got := stepIdentity(tc.command, previous.Commands[tc.command].Steps[tc.index]) == stepIdentity(tc.command, tc.step); got != tc.same {
				t.Fatalf("identity equality %t, want %t", got, tc.same)
			}
			if e := compareCommandPolicy(previous, current, now); (e == nil) != tc.same {
				t.Fatalf("policy result %v, want same=%t", e, tc.same)
			}
		})
	}
}
