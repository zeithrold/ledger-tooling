package governance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// builtinCommands are the CLI's own commands. A justfile may invoke them or a
// command configured in governance.json.
var builtinCommands = []string{
	"bundle", "changes", "commit-check", "coverage-check", "currency",
	"debug-report", "debug-run", "debug-start", "debug-verify", "doctor",
	"fingerprint", "generate-check", "isolated-run", "mutation-check",
	"policy-check", "recipes-check", "review-check", "skills-check",
	"ui-report", "version", "version-check",
}

var (
	justKeyword = regexp.MustCompile(`^(set|export|unexport|import|mod|alias|shell)\s`)
	recipeName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	invocation  = []*regexp.Regexp{
		regexp.MustCompile(`bootstrap\.go\s+([A-Za-z][A-Za-z0-9_-]*)`),
		regexp.MustCompile(`cmd/ledger-tool\s+(?:--root\s+\S+\s+)?([A-Za-z][A-Za-z0-9_-]*)`),
		regexp.MustCompile(`@ledger-tool\s+([A-Za-z][A-Za-z0-9_-]*)`),
	}
)

type recipeSheet struct {
	deps     map[string][]string
	commands map[string][]string
}

// RecipesCheck keeps the documented developer entrypoint honest: every command
// a recipe runs must exist, every command CI runs directly must be reachable
// from a recipe, and every configured command must be reachable from a recipe
// so that what a developer can run matches what CI enforces.
func RecipesCheck(root string, config Config) error {
	sheet, err := readJustfile(filepath.Join(root, "justfile"))
	if err != nil {
		return err
	}
	ci, err := ciCommands(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, name := range builtinCommands {
		known[name] = true
	}
	names := make([]string, 0, len(config.Commands))
	for name := range config.Commands {
		known[name] = true
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range sortedKeys(sheet.commands) {
		for _, command := range sheet.commands[name] {
			if !known[command] {
				return fmt.Errorf("recipe %s invokes unknown command %s", name, command)
			}
		}
	}

	delegates := map[string][]string{}
	for name, command := range config.Commands {
		for _, step := range command.Steps {
			for i, arg := range step.Argv {
				if arg == "@ledger-tool" && i+1 < len(step.Argv) {
					delegates[name] = append(delegates[name], step.Argv[i+1])
				}
			}
		}
	}
	reached := map[string]bool{}
	var visitCommand func(string)
	visitCommand = func(name string) {
		if reached[name] {
			return
		}
		reached[name] = true
		for _, next := range delegates[name] {
			visitCommand(next)
		}
	}
	visited := map[string]bool{}
	var visitRecipe func(string)
	visitRecipe = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dep := range sheet.deps[name] {
			visitRecipe(dep)
		}
		for _, command := range sheet.commands[name] {
			visitCommand(command)
		}
	}
	for _, name := range sortedKeys(sheet.commands) {
		visitRecipe(name)
	}

	ciNames := make([]string, 0, len(ci))
	for name := range ci {
		ciNames = append(ciNames, name)
	}
	sort.Strings(ciNames)
	for _, name := range ciNames {
		if !reached[name] {
			return fmt.Errorf("CI runs %s but no just recipe reaches it", name)
		}
	}
	for _, name := range names {
		if !reached[name] {
			return fmt.Errorf("configured command %s is not reachable from any just recipe", name)
		}
	}
	return nil
}

// readJustfile extracts recipe dependencies and the commands each recipe runs.
func readJustfile(path string) (recipeSheet, error) {
	sheet := recipeSheet{deps: map[string][]string{}, commands: map[string][]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return sheet, fmt.Errorf("justfile is required for the documented entrypoint: %w", err)
	}
	current := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			if justKeyword.MatchString(line) {
				current = ""
				continue
			}
			name, deps, ok := parseRecipeHeader(line)
			if !ok {
				current = ""
				continue
			}
			current = name
			if _, seen := sheet.deps[current]; !seen {
				sheet.deps[current] = nil
				sheet.commands[current] = nil
			}
			sheet.deps[current] = append(sheet.deps[current], deps...)
			continue
		}
		if current == "" {
			continue
		}
		for _, pattern := range invocation {
			for _, match := range pattern.FindAllStringSubmatch(line, -1) {
				sheet.commands[current] = append(sheet.commands[current], match[1])
			}
		}
	}
	return sheet, nil
}

// ciCommands collects the commands the workflows invoke directly.
func ciCommands(dir string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, pattern := range invocation {
			for _, match := range pattern.FindAllStringSubmatch(string(b), -1) {
				out[match[1]] = true
			}
		}
	}
	return out, nil
}

// parseRecipeHeader reads `name params: dependencies`, tolerating defaults that
// contain separators (`review-check file=".governance/review.json":`).
func parseRecipeHeader(line string) (string, []string, bool) {
	single, double := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !double {
				single = !single
			}
		case '"':
			if !single {
				double = !double
			}
		case ':':
			if single || double {
				continue
			}
			if i+1 < len(line) && line[i+1] == '=' {
				return "", nil, false
			}
			fields := strings.Fields(strings.TrimSpace(line[:i]))
			if len(fields) == 0 || !recipeName.MatchString(fields[0]) {
				return "", nil, false
			}
			rest := line[i+1:]
			if index := strings.Index(rest, "#"); index >= 0 {
				rest = rest[:index]
			}
			return fields[0], strings.Fields(rest), true
		}
	}
	return "", nil, false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
