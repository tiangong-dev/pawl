package main

// Shared fixtures and helpers for the `pawl trend` integration tests: building
// a throwaway git repo, committing hand-authored snapshots at fixed dates, and
// parsing the text / json trend output.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// trendConfig is a minimal valid config: trend reads it only to resolve the
// snapshot path, never to measure, so the dimension's command is never run
// by these tests (except the ones that deliberately call record/check/diff
// to prove --limit is rejected there).
const trendConfig = `dimensions:
  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    command: "echo '{\"value\": 1}'"
`

// newTrendRepo creates a throwaway git repo with a committed identity and an
// (uncommitted) trend config, ready for tests to layer snapshot commits on.
func newTrendRepo(t *testing.T) (dir, homeDir string) {
	t.Helper()
	dir = t.TempDir()
	homeDir = initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", trendConfig)
	return dir, homeDir
}

// trendMetricValue is one metric's shape for a hand-built historical
// snapshot commit.
type trendMetricValue struct {
	direction string
	unit      string
	value     float64
}

// trendSnapshotJSON renders a minimal valid pawl.snapshot.json body (ids
// sorted, like a real `record` would write) from the given metrics.
func trendSnapshotJSON(metrics map[string]trendMetricValue) string {
	ids := make([]string, 0, len(metrics))
	for id := range metrics {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(`{"metrics":{`)
	for i, id := range ids {
		m := metrics[id]
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"%s":{"direction":%q,"value":%v,"unit":%q,"breakdown":null}`, id, m.direction, m.value, m.unit)
	}
	b.WriteString("}}\n")
	return b.String()
}

// gitCommitAllDated stages and commits everything under dir with a fixed
// author/committer date, so the trend output's <YYYY-MM-DD> column is
// assertable rather than depending on wall-clock test run time.
func gitCommitAllDated(t *testing.T, dir, homeDir, message, date string) string {
	t.Helper()
	runGit(t, dir, homeDir, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(homeDir), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return strings.TrimSpace(runGit(t, dir, homeDir, "rev-parse", "HEAD"))
}

// commitSnapshotAt writes the snapshot for the given metrics and commits it
// at a fixed date, returning the resulting commit sha.
func commitSnapshotAt(t *testing.T, dir, homeDir string, metrics map[string]trendMetricValue, message, date string) string {
	t.Helper()
	writeFile(t, dir, "pawl.snapshot.json", trendSnapshotJSON(metrics))
	return gitCommitAllDated(t, dir, homeDir, message, date)
}

func gitShortSHA(t *testing.T, dir, homeDir, sha string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, dir, homeDir, "rev-parse", "--short", sha))
}

// trendRowFields locates the text-output row mentioning shortSHA and splits
// it on whitespace, tolerant of whatever column padding the implementation
// uses — only field content and order are asserted.
func trendRowFields(t *testing.T, stdout, shortSHA string) []string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, shortSHA) {
			return strings.Fields(line)
		}
	}
	t.Fatalf("stdout has no row mentioning commit %s:\n%s", shortSHA, stdout)
	return nil
}

// trendReport is the `trend --format json` schema (SPEC.md § Trend).
type trendReport struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Snapshot      string        `json:"snapshot"`
	Metrics       []trendMetric `json:"metrics"`
}

type trendMetric struct {
	ID        string       `json:"id"`
	Direction string       `json:"direction"`
	Unit      string       `json:"unit"`
	Points    []trendPoint `json:"points"`
}

type trendPoint struct {
	Commit string  `json:"commit"`
	Date   string  `json:"date"`
	Value  float64 `json:"value"`
}

func parseTrendReport(t *testing.T, stdout string) trendReport {
	t.Helper()
	var r trendReport
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%s", err, stdout)
	}
	return r
}
