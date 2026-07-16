package main

// Integration tests for the SPEC.md contract that every command rejects
// extra positional operands: check/diff/record/init/version take none,
// trend takes at most one (the metric id), baseline-guard takes at most
// one (the ref). A stray extra operand is a usage error (exit 2, stderr
// names the problem), not something the command silently ignores.

import (
	"path/filepath"
	"strings"
	"testing"
)

const arityConfig = `dimensions:
  - id: "m"
    title: "M"
    direction: "lower-is-better"
    command: "echo '{\"value\": 1}'"
`

// check has no operand: a stray extra one is rejected before any
// measurement is attempted.
func TestCheckRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, arityConfig)

	res := runPawl(t, dir, baseEnv(), "check", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if res.stderr == "" {
		t.Errorf("stderr is empty, want a message naming the unexpected operand")
	}
}

// diff has no operand.
func TestDiffRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, arityConfig)

	res := runPawl(t, dir, baseEnv(), "diff", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// init has no operand.
func TestInitRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "init", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// version has no operand.
func TestVersionRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "version", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// record has no operand. This is also the case a user hits by forgetting
// the dashes of `--only`: `pawl record only x` must be rejected as a usage
// error, not silently treated as a full record — a full re-measure would
// bless whatever regressions are sitting in the working tree.
func TestRecordRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, arityConfig)

	res := runPawl(t, dir, baseEnv(), "record", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// `record only passing-tests` — a user who forgot the `--` of `--only` —
// must be rejected outright and must not write or re-bless the snapshot.
func TestRecordOnlyTypoWithoutDashesRejectedAndSnapshotUnchanged(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, arityConfig)

	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	before := readFile(t, snapPath)

	res := runPawl(t, dir, baseEnv(), "record", "only", "passing-tests")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	after := readFile(t, snapPath)
	if before != after {
		t.Errorf("snapshot content changed after a rejected invocation\nbefore=%s\nafter=%s", before, after)
	}
}

// trend takes at most one operand (the metric id); a second one is rejected.
func TestTrendRejectsExtraOperand(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir,
		map[string]trendMetricValue{"file-length": {direction: "lower-is-better", unit: "count", value: 1}},
		"snapshot", "2026-01-01T00:00:00")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "file-length", "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// baseline-guard takes at most one operand (the ref); a second one is
// rejected. The fixture commits a real baseline snapshot so a would-be
// exit 0 (e.g. "ref predates the snapshot", a legitimate skip in the
// no-baseline case) cannot be confused with the arity rejection this test
// is pinning.
func TestBaselineGuardRejectsExtraOperand(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	res := runPawl(t, dir, gitEnv(homeDir), "baseline-guard", base, "extra")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Controls: one operand is still accepted where the contract allows it,
// and no operand is still accepted where the contract requires none.

// check with no operand and a valid recorded config passes.
func TestCheckControlNoOperandPasses(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, arityConfig)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// baseline-guard with exactly one operand (the ref) is not rejected for
// arity — it runs the real comparison and passes on an equal working tree.
func TestBaselineGuardControlSingleOperandNotRejectedForArity(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	res := runPawl(t, dir, gitEnv(homeDir), "baseline-guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// trend with exactly one operand (an existing metric id) is not rejected
// for arity.
func TestTrendControlSingleOperandNotRejectedForArity(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir,
		map[string]trendMetricValue{"file-length": {direction: "lower-is-better", unit: "count", value: 1}},
		"snapshot", "2026-01-01T00:00:00")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "file-length")
	if res.exit == 2 && strings.Contains(res.stderr, "unexpected") {
		t.Fatalf("single operand rejected as arity error: exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}
