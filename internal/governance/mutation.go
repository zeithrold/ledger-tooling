package governance

import (
	"encoding/json"
	"fmt"
	"os"
)

type MutationSummary struct {
	SchemaVersion int            `json:"schema_version"`
	Counts        map[string]int `json:"counts"`
	Score         float64        `json:"score"`
	Minimum       float64        `json:"minimum"`
	Status        string         `json:"status"`
}

func MutationCheck(root, path string) error {
	p, e := safePath(root, path)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	var report struct {
		Files []struct {
			FileName  string `json:"file_name"`
			Mutations []struct {
				Status string `json:"status"`
				Line   int    `json:"line"`
			} `json:"mutations"`
		} `json:"files"`
	}
	if e = json.Unmarshal(b, &report); e != nil {
		return e
	}
	s := MutationSummary{SchemaVersion: 1, Counts: map[string]int{}, Minimum: 90, Status: "failed"}
	for _, f := range report.Files {
		for _, m := range f.Mutations {
			switch m.Status {
			case "KILLED", "LIVED", "NOT_COVERED", "TIMED_OUT", "NOT_VIABLE", "UNCOVERED", "TIMEOUT":
				s.Counts[m.Status]++
			default:
				return fmt.Errorf("unknown mutation status %q", m.Status)
			}
		}
	}
	total := s.Counts["KILLED"] + s.Counts["LIVED"] + s.Counts["NOT_COVERED"] + s.Counts["UNCOVERED"] + s.Counts["TIMED_OUT"] + s.Counts["TIMEOUT"]
	if total == 0 {
		return fmt.Errorf("empty viable mutation denominator")
	}
	s.Score = 100 * float64(s.Counts["KILLED"]) / float64(total)
	if s.Score >= 90 && s.Counts["TIMED_OUT"]+s.Counts["TIMEOUT"] == 0 {
		s.Status = "passed"
	}
	output, e := safePath(root, "build/governance/mutation.json")
	if e != nil {
		return e
	}
	if e = WriteJSON(output, s); e != nil {
		return e
	}
	if s.Status != "passed" {
		return fmt.Errorf("mutation gate failed: score %.2f%%, timeouts %d", s.Score, s.Counts["TIMED_OUT"]+s.Counts["TIMEOUT"])
	}
	return nil
}
