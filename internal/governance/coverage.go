package governance

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type unit struct {
	File                   string
	Start, End, Weight     int
	Hit                    bool
	StartColumn, EndColumn int
}
type CoverageReport struct {
	SchemaVersion                        int    `json:"schema_version"`
	Metric                               string `json:"metric"`
	Total, Covered                       int
	Percent                              float64
	IncrementalTotal, IncrementalCovered int
	IncrementalPercent                   *float64
	Minimum, IncrementalMinimum          float64
	Status                               string   `json:"status"`
	Missing                              []string `json:"missing,omitempty"`
	Errors                               []string `json:"errors,omitempty"`
}

var goLine = regexp.MustCompile(`^(.*):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

func normalizeSource(root, f string) string {
	f = filepath.ToSlash(f)
	if filepath.IsAbs(f) {
		if r, e := filepath.Rel(root, f); e == nil {
			return filepath.ToSlash(r)
		}
	}
	b, e := os.ReadFile(filepath.Join(root, "go.mod"))
	if e == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "module ") {
				f = strings.TrimPrefix(f, strings.TrimSpace(strings.TrimPrefix(l, "module "))+"/")
				break
			}
		}
	}
	return strings.TrimPrefix(f, "./")
}
func excluded(f string, patterns []string) bool {
	for _, p := range patterns {
		if strings.HasSuffix(p, "/**") && strings.HasPrefix(f, strings.TrimSuffix(p, "**")) {
			return true
		}
		if ok, _ := filepath.Match(p, f); ok {
			return true
		}
		if f == p {
			return true
		}
	}
	return false
}
func parseProfile(root, path, format string) (map[string]unit, error) {
	file, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer file.Close()
	out := map[string]unit{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	source := ""
	header := false
	for scanner.Scan() {
		s := scanner.Text()
		if s == "" {
			continue
		}
		if format == "go" {
			if strings.HasPrefix(s, "mode: ") {
				if s != "mode: atomic" && s != "mode: count" && s != "mode: set" {
					return nil, fmt.Errorf("invalid cover mode")
				}
				header = true
				continue
			}
			m := goLine.FindStringSubmatch(s)
			if m == nil {
				return nil, fmt.Errorf("invalid Go coverage record %q", s)
			}
			start, _ := strconv.Atoi(m[2])
			end, _ := strconv.Atoi(m[4])
			weight, _ := strconv.Atoi(m[6])
			hits, _ := strconv.Atoi(m[7])
			f := normalizeSource(root, m[1])
			key := f + ":" + strings.Join(m[2:6], ":")
			old := out[key]
			startColumn, _ := strconv.Atoi(m[3])
			endColumn, _ := strconv.Atoi(m[5])
			out[key] = unit{f, start, end, weight, hits > 0 || old.Hit, startColumn, endColumn}
		} else if format == "lcov" {
			if strings.HasPrefix(s, "SF:") {
				source = normalizeSource(root, s[3:])
				header = true
			} else if strings.HasPrefix(s, "DA:") {
				p := strings.Split(s[3:], ",")
				if len(p) < 2 || source == "" {
					return nil, fmt.Errorf("invalid LCOV record")
				}
				line, e := strconv.Atoi(p[0])
				if e != nil || line < 1 {
					return nil, fmt.Errorf("invalid LCOV line")
				}
				hits, e := strconv.Atoi(p[1])
				if e != nil || hits < 0 {
					return nil, fmt.Errorf("invalid LCOV hits")
				}
				key := fmt.Sprintf("%s:%d", source, line)
				out[key] = unit{source, line, line, 1, hits > 0 || out[key].Hit, 0, 0}
			} else if s == "end_of_record" {
				source = ""
			}
		} else {
			return nil, fmt.Errorf("unsupported coverage format: %s", format)
		}
	}
	if e = scanner.Err(); e != nil {
		return nil, e
	}
	if !header || len(out) == 0 {
		return nil, fmt.Errorf("empty coverage report: %s", path)
	}
	return out, nil
}
func productionFiles(root string, c CoverageConfig) ([]string, error) {
	var files []string
	ext := ".go"
	if c.Format == "lcov" {
		ext = ".dart"
	}
	for _, dir := range c.SourceRoots {
		p, e := safePath(root, dir)
		if e != nil {
			return nil, e
		}
		e = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("source symlink not supported: %s", path)
			}
			if d.IsDir() {
				return nil
			}
			r, _ := filepath.Rel(root, path)
			r = filepath.ToSlash(r)
			if filepath.Ext(r) == ext && !strings.HasSuffix(r, "_test.go") && !excluded(r, c.Exclude) {
				if ext == ".go" {
					matches, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
					if err != nil {
						return err
					}
					if !matches {
						return nil
					}
				}
				files = append(files, r)
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	return files, nil
}
func hasGoCode(root, f string) bool {
	tree, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, f), nil, 0)
	if e != nil {
		return true
	}
	found := false
	ast.Inspect(tree, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Body != nil && len(v.Body.List) > 0 {
				found = true
			}
		case *ast.FuncLit:
			found = true
		}
		return true
	})
	return found
}

func executableGoLines(root string, files []string) (map[string]map[int][]int, error) {
	out := map[string]map[int][]int{}
	for _, file := range files {
		set := token.NewFileSet()
		tree, err := parser.ParseFile(set, filepath.Join(root, file), nil, 0)
		if err != nil {
			return nil, err
		}
		lines := map[int][]int{}
		ast.Inspect(tree, func(n ast.Node) bool {
			if stmt, ok := n.(ast.Stmt); ok {
				switch stmt.(type) {
				case *ast.BlockStmt, *ast.EmptyStmt, *ast.CaseClause, *ast.CommClause:
				default:
					p := set.Position(stmt.Pos())
					lines[p.Line] = append(lines[p.Line], p.Column)
				}
			}
			return true
		})
		out[file] = lines
	}
	return out, nil
}
func Coverage(root string, c CoverageConfig, base string, now time.Time) (CoverageReport, error) {
	report := CoverageReport{SchemaVersion: 1, Metric: c.Format, Minimum: c.Minimum, IncrementalMinimum: c.IncrementalMinimum, Status: "failed"}
	files, e := productionFiles(root, c)
	if e != nil {
		return report, e
	}
	allowed := map[string]bool{}
	for _, file := range files {
		allowed[file] = true
	}
	if c.Minimum < 70 || c.IncrementalMinimum < 90 || c.Minimum > 100 || c.IncrementalMinimum > 100 {
		return report, fmt.Errorf("coverage thresholds must be >=70 total and >=90 incremental and <=100")
	}
	if c.Baseline != nil {
		b := c.Baseline
		expiry, e := time.Parse("2006-01-02", b.Expires)
		started, startErr := time.Parse("2006-01-02", b.Started)
		if e != nil || startErr != nil || started.After(now) || expiry.Sub(started) > 30*24*time.Hour || !expiry.After(started) || b.Reason == "" || b.Minimum < 0 || b.Minimum > 100 {
			return report, fmt.Errorf("invalid coverage baseline")
		}
		if now.Before(expiry) && b.Minimum < c.Minimum {
			report.Minimum = b.Minimum
		}
	}
	merged := map[string]unit{}
	for _, p := range c.Profiles {
		path, e := safePath(root, p)
		if e != nil {
			return report, e
		}
		units, e := parseProfile(root, path, c.Format)
		if e != nil {
			return report, e
		}
		for k, u := range units {
			if excluded(u.File, c.Exclude) {
				continue
			}
			if !allowed[u.File] {
				return report, fmt.Errorf("coverage source outside production inventory: %s", u.File)
			}
			if old, ok := merged[k]; ok {
				if old.Weight != u.Weight {
					return report, fmt.Errorf("incompatible profiles")
				}
				u.Hit = u.Hit || old.Hit
			}
			merged[k] = u
		}
	}
	seen := map[string]bool{}
	for _, u := range merged {
		seen[u.File] = true
	}
	inventory := map[string][]int{}
	if c.Inventory != "" {
		p, e := safePath(root, c.Inventory)
		if e != nil {
			return report, e
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return report, e
		}
		var inv struct {
			Files map[string][]int `json:"files"`
		}
		if e = json.Unmarshal(b, &inv); e != nil {
			return report, e
		}
		inventory = inv.Files
	}
	for _, f := range files {
		if c.Format == "lcov" && c.Inventory != "" {
			lines, ok := inventory[f]
			if !ok {
				return report, fmt.Errorf("source missing from coverage inventory: %s", f)
			}
			// VM records are authoritative for an instrumented file. AST inventory
			// is a conservative fallback only for sources absent from the VM report.
			if !seen[f] {
				for _, line := range lines {
					key := fmt.Sprintf("%s:%d", f, line)
					if _, ok := merged[key]; !ok {
						merged[key] = unit{f, line, line, 1, false, 0, 0}
					}
				}
			}
			seen[f] = true // Inventory represents unreported executable lines explicitly as zero.
			if len(lines) == 0 {
				continue
			}
		}
		if c.Format == "go" && !hasGoCode(root, f) {
			continue
		}
		if !seen[f] {
			report.Missing = append(report.Missing, f)
		}
	}
	changes, e := ChangedLines(root, base)
	if e != nil {
		return report, e
	}
	var executable map[string]map[int][]int
	if c.Format == "go" {
		executable, e = executableGoLines(root, files)
		if e != nil {
			return report, e
		}
	}
	changed := map[string]bool{}
	hits := map[string]bool{}
	for _, u := range merged {
		report.Total += u.Weight
		if u.Hit {
			report.Covered += u.Weight
		}
		for l := u.Start; l <= u.End; l++ {
			if c.Format == "go" {
				belongs := false
				for _, column := range executable[u.File][l] {
					if (l > u.Start || column >= u.StartColumn) && (l < u.End || column < u.EndColumn) {
						belongs = true
					}
				}
				if !belongs {
					continue
				}
			}
			if changes[u.File][l] {
				key := fmt.Sprintf("%s:%d", u.File, l)
				changed[key] = true
				if previous, ok := hits[key]; ok {
					hits[key] = previous && u.Hit
				} else {
					hits[key] = u.Hit
				}
			}
		}
	}
	report.IncrementalTotal = len(changed)
	for k := range changed {
		if hits[k] {
			report.IncrementalCovered++
		}
	}
	if report.Total == 0 {
		return report, fmt.Errorf("empty coverage denominator")
	}
	report.Percent = 100 * float64(report.Covered) / float64(report.Total)
	if report.IncrementalTotal > 0 {
		v := 100 * float64(report.IncrementalCovered) / float64(report.IncrementalTotal)
		report.IncrementalPercent = &v
		if v+1e-9 < c.IncrementalMinimum {
			report.Errors = append(report.Errors, "incremental coverage below threshold")
		}
	}
	if report.Percent+1e-9 < report.Minimum {
		report.Errors = append(report.Errors, "total coverage below threshold")
	}
	if len(report.Missing) > 0 {
		sort.Strings(report.Missing)
		report.Errors = append(report.Errors, "production files absent from coverage")
	}
	if len(report.Errors) > 0 {
		return report, fmt.Errorf("coverage gate: %s", strings.Join(report.Errors, ", "))
	}
	report.Status = "passed"
	return report, nil
}
