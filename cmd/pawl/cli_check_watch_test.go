package main

import (
	"fmt"
	"testing"
)

func fileLengthWatchConfig(threshold int) string {
	return buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{
			fmt.Sprintf("threshold = %d", threshold),
			`include = ["**/*.txt"]`,
		},
	})
}

func watchByPath(r jsonReport) map[string]jsonWatch {
	out := map[string]jsonWatch{}
	for _, w := range r.Watch {
		out[w.Path] = w
	}
	return out
}

// check --format json lists a dirty near-threshold file in watch, with
// remaining headroom, and does not list an equally-near file the agent did
// not touch. Watch must not change the exit code.
func TestCheckJSONWatchNearTouchedFile(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "small.txt", nLines(2))
	writeFile(t, dir, "untouched-near.txt", nLines(10))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "small.txt", nLines(10))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (watch is advisory)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	byPath := watchByPath(r)
	w, ok := byPath["small.txt"]
	if !ok {
		t.Fatalf("watch missing touched near file small.txt: %+v", r.Watch)
	}
	if w.Status != "near" {
		t.Errorf("small.txt status = %q, want near", w.Status)
	}
	if w.Value != 10 || w.Threshold != 11 {
		t.Errorf("small.txt value/threshold = %v/%v, want 10/11", w.Value, w.Threshold)
	}
	if w.Headroom != 1 {
		t.Errorf("small.txt headroom = %v, want 1", w.Headroom)
	}
	if w.Kind != "lines" || w.ID != "fl" {
		t.Errorf("small.txt id/kind = %s/%s, want fl/lines", w.ID, w.Kind)
	}
	if _, ok := byPath["untouched-near.txt"]; ok {
		t.Errorf("watch listed untouched near file: %+v", r.Watch)
	}
}

func TestCheckJSONWatchOmitsOkTouchedFile(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "ok.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "ok.txt", nLines(3))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 0 {
		t.Errorf("watch = %+v, want omitted for an ok touched file", r.Watch)
	}
}

func TestCheckJSONWatchOverHasNegativeHeadroom(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "a.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.txt", nLines(20))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (file now over threshold, total gate)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	w, ok := watchByPath(r)["a.txt"]
	if !ok {
		t.Fatalf("watch missing over file a.txt: %+v", r.Watch)
	}
	if w.Status != "over" {
		t.Errorf("a.txt status = %q, want over", w.Status)
	}
	if w.Headroom != -9 {
		t.Errorf("a.txt headroom = %v, want -9 (11-20)", w.Headroom)
	}
}

func TestCheckJSONWatchOmittedOnCleanTree(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "near.txt", nLines(10))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 0 {
		t.Errorf("clean tree must omit watch, got %+v", r.Watch)
	}
}

func TestRecordJSONOmitsWatch(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "a.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")
	writeFile(t, dir, "a.txt", nLines(10))

	res := runPawl(t, dir, gitEnv(homeDir), "record", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 0 {
		t.Errorf("record must omit watch, got %+v", r.Watch)
	}
}

// --since uses the merge-base changed set, so a committed near file still
// appears in watch on a clean tree.
func TestCheckJSONWatchDedupesSamePathKind(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{
			id: "fl", direction: "lower-is-better", builtin: "file-length",
			optionLines: []string{"threshold = 11", `include = ["**/*.txt"]`},
		},
		dimDef{
			id: "fl-growth", direction: "lower-is-better", gate: "per-key-value",
			builtin:     "file-length",
			optionLines: []string{"threshold = 11", `include = ["**/*.txt"]`},
		},
	))
	writeFile(t, dir, "a.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")
	writeFile(t, dir, "a.txt", nLines(10))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 1 {
		t.Fatalf("watch = %+v, want one entry (same path+kind deduped)", r.Watch)
	}
	if r.Watch[0].Path != "a.txt" || r.Watch[0].ID != "fl" {
		t.Errorf("watch[0] = %+v, want a.txt / fl (first dimension)", r.Watch[0])
	}
}

func TestCheckJSONWatchSinceIncludesCommittedChange(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "a.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.txt", nLines(10))
	gitCommitAll(t, dir, homeDir, "grow a.txt")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	w, ok := watchByPath(parseReport(t, res.stdout))["a.txt"]
	if !ok {
		t.Fatalf("watch missing a.txt under --since on a clean tree")
	}
	if w.Status != "near" || w.Headroom != 1 {
		t.Errorf("a.txt = %+v, want near headroom 1", w)
	}
}
