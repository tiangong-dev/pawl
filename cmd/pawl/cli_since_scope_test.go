package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A total-gate dimension has no line to attribute, so --since enforces it at
// full strength: a total regression still fails (exit 1) and the output labels
// it "enforced in full" — it is not silently exempted.
func TestSinceTotalGateEnforcedInFull(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	base5 := buildConfig("", dimDef{id: "t", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	writeFile(t, dir, "pawl.yaml", base5)
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// Worsen the measured total (a total has no diff line to scope against).
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "t", direction: "lower-is-better", command: `echo '{"value": 8}'`}))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (total gate enforced in full)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "enforced in full") {
		t.Errorf("stdout missing the 'enforced in full' label for the total dimension: %s", res.stdout)
	}
}

// A mixed config — a clean total dimension plus a per-file-count dimension with
// a new offender on an added line — fails (exit 1): the live per-file
// regression drives the verdict even though the total dimension is clean.
func TestSinceMixedConfigFailsOnLiveOffender(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	config := buildConfig("",
		dimDef{id: "total", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{
			id: "nolint", direction: "lower-is-better", gate: "per-file-count",
			builtin: "pattern-count", optionLines: []string{`pattern = "NOLINT"`, `include = ["**/*.go"]`},
		},
	)
	writeFile(t, dir, "pawl.yaml", config)
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")
	gitCommitAll(t, dir, homeDir, "new offender on added line")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (live per-file offender)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// check --since --format json sets mode "since" and since "<ref>"; an exempted
// regression carries suppressed true and does NOT push exit_code to 1 (it is
// the only would-be regression, so the run passes).
func TestSinceFormatJSONExemptedSuppressed(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count",
		builtin: "pattern-count", optionLines: []string{`pattern = "NOLINT"`, `include = ["**/*.go"]`},
	}))
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	// A pre-existing marker (line 3) not in the snapshot, committed at base.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT preexisting\n")
	base := gitCommitAll(t, dir, homeDir, "base with stale snapshot")
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT preexisting\nplain tail\n")
	gitCommitAll(t, dir, homeDir, "unrelated change")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (only regression is exempted)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.Mode != "since" {
		t.Errorf("mode = %q, want since", r.Mode)
	}
	if r.Since == nil || *r.Since != base {
		t.Errorf("since = %v, want %q", r.Since, base)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0 (a suppressed regression must not fail the run)", r.ExitCode)
	}
	m, ok := metricByID(r, "nolint")
	if !ok {
		t.Fatalf("metric nolint absent: %+v", r)
	}
	var sawSuppressed bool
	for _, reg := range m.Regressions {
		if reg.Suppressed {
			sawSuppressed = true
		}
	}
	if !sawSuppressed {
		t.Errorf("no suppressed regression recorded: %+v", m.Regressions)
	}
}

// Diff scoping must handle the config dir being a SUBDIRECTORY of the git repo
// root: breakdown keys are config-relative ("a.go:3") while git diff paths are
// repo-relative ("proj/a.go"). A new offender on an added line in the subdir is
// still correctly attributed → exit 1.
func TestSinceConfigInSubdirOfRepo(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	sub := filepath.Join(dir, "proj")

	writeFile(t, dir, "proj/pawl.yaml", buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count",
		builtin: "pattern-count", optionLines: []string{`pattern = "NOLINT"`, `include = ["**/*.go"]`},
	}))
	writeFile(t, dir, "proj/a.go", "package a\n// NOLINT keep\n")
	runPawl(t, sub, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "proj/a.go", "package a\n// NOLINT keep\n// NOLINT added\n")
	gitCommitAll(t, dir, homeDir, "new offender in subdir on added line")

	res := runPawl(t, sub, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (subdir offender on added line)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}
