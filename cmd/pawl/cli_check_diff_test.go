package main

import (
	"strings"
	"testing"
)

func mustRecord(t *testing.T, dir, config string) {
	t.Helper()
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// check and diff both refuse to run without a snapshot to compare against —
// there is no honest verdict to give.
func TestCheckAndDiffRequireSnapshot(t *testing.T) {
	config := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`})

	for _, cmd := range []string{"check", "diff", ""} {
		t.Run("command="+cmd, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", config)
			args := []string{}
			if cmd != "" {
				args = append(args, cmd)
			}
			res := runPawl(t, dir, baseEnv(), args...)
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// No command given behaves exactly as `check`, including on the happy path.
func TestNoCommandDefaultsToCheck(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`})
	mustRecord(t, dir, config)

	withCmd := runPawl(t, dir, baseEnv(), "check")
	bare := runPawl(t, dir, baseEnv())
	if withCmd.exit != bare.exit {
		t.Errorf("exit for explicit check (%d) != exit for no command (%d)", withCmd.exit, bare.exit)
	}
	if bare.exit != 0 {
		t.Fatalf("no-command exit = %d, want 0\nstdout=%s\nstderr=%s", bare.exit, bare.stdout, bare.stderr)
	}
	if !strings.Contains(bare.stdout, "✅ same") {
		t.Errorf("no-command stdout missing same-status: %s", bare.stdout)
	}
}

// Unchanged measurements pass with an explicit "same" status.
func TestCheckGreen(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "✅ same") {
		t.Errorf("stdout missing same-status: %s", res.stdout)
	}
}

// A scalar regression fails check (exit 1) with the pinned detail line, but
// never fails diff (always exit 0) even though it prints the same detail.
func TestCheckRegressionScalarFailsDiffDoesNot(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	mustRecord(t, dir, base)

	worse := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 8}'`})
	writeFile(t, dir, "pawl.yaml", worse)

	check := runPawl(t, dir, baseEnv(), "check")
	if check.exit != 1 {
		t.Fatalf("check exit = %d, want 1\nstdout=%s\nstderr=%s", check.exit, check.stdout, check.stderr)
	}
	if !strings.Contains(check.stdout, "❌ regressions:") {
		t.Errorf("check stdout missing regressions block: %s", check.stdout)
	}
	if !strings.Contains(check.stdout, "total 5 → 8") {
		t.Errorf("check stdout missing detail line: %s", check.stdout)
	}

	diff := runPawl(t, dir, baseEnv(), "diff")
	if diff.exit != 0 {
		t.Fatalf("diff exit = %d, want 0 (diff always exits 0)\nstdout=%s\nstderr=%s", diff.exit, diff.stdout, diff.stderr)
	}
	if !strings.Contains(diff.stdout, "❌ regressions:") || !strings.Contains(diff.stdout, "total 5 → 8") {
		t.Errorf("diff stdout missing regressions detail: %s", diff.stdout)
	}
}

// Tolerance absorbs a small scalar regression (exit 0, distinct status) but
// not one past the declared slack (exit 1).
func TestCheckToleranceBoundary(t *testing.T) {
	dir := t.TempDir()
	tol := 1.0
	base := buildConfig("", dimDef{id: "m", direction: "lower-is-better", tolerance: &tol, command: `echo '{"value": 10}'`})
	mustRecord(t, dir, base)

	within := buildConfig("", dimDef{id: "m", direction: "lower-is-better", tolerance: &tol, command: `echo '{"value": 11}'`})
	writeFile(t, dir, "pawl.yaml", within)
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("within-tolerance check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "✅ within tolerance") {
		t.Errorf("stdout missing within-tolerance status: %s", res.stdout)
	}

	beyond := buildConfig("", dimDef{id: "m", direction: "lower-is-better", tolerance: &tol, command: `echo '{"value": 12}'`})
	writeFile(t, dir, "pawl.yaml", beyond)
	res2 := runPawl(t, dir, baseEnv(), "check")
	if res2.exit != 1 {
		t.Fatalf("beyond-tolerance check exit = %d, want 1\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
}

// A strict improvement is reported in the table and the summary block, and
// additionally as a CI annotation when GITHUB_ACTIONS is set.
func TestCheckImprovementReporting(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 10}'`})
	mustRecord(t, dir, base)

	improved := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	writeFile(t, dir, "pawl.yaml", improved)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "🎉 better") {
		t.Errorf("stdout missing per-row better status: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "🎉 improved:") {
		t.Errorf("stdout missing improved summary block: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "pawl record") {
		t.Errorf("stdout missing hint to run pawl record: %s", res.stdout)
	}
	if strings.Contains(res.stdout, "::notice::") {
		t.Errorf("stdout should not contain the CI notice when GITHUB_ACTIONS is unset: %s", res.stdout)
	}

	ci := runPawl(t, dir, baseEnv("GITHUB_ACTIONS=true"), "check")
	if ci.exit != 0 {
		t.Fatalf("check under CI exit = %d, want 0\nstdout=%s\nstderr=%s", ci.exit, ci.stdout, ci.stderr)
	}
	want := "::notice::pawl improved: m — run `pawl record` to lock in the gains."
	if !strings.Contains(ci.stdout, want) {
		t.Errorf("CI stdout missing exact notice line %q: %s", want, ci.stdout)
	}
}

// A dimension present in config but absent from the snapshot is "new" — it
// cannot regress, and does not fail the run on its own.
func TestCheckNewDimension(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})
	mustRecord(t, dir, base)

	withNew := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 9}'`},
	)
	writeFile(t, dir, "pawl.yaml", withNew)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "🆕 new") {
		t.Errorf("stdout missing new-dimension status: %s", res.stdout)
	}
}

// A snapshot metric whose id matches no configured dimension refuses to run
// — deleting a dimension must also drop it from the snapshot.
func TestCheckOrphanedMetric(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	)
	mustRecord(t, dir, base)

	withoutB := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})
	writeFile(t, dir, "pawl.yaml", withoutB)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "b") {
		t.Errorf("orphan message does not mention the orphaned id \"b\": stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// A malformed snapshot shape is refused, not silently treated as consistent.
func TestCheckMalformedSnapshotShape(t *testing.T) {
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})

	cases := []struct {
		name     string
		snapshot string
		wantMsg  string
	}{
		{"empty metrics object", `{"metrics": {}}`, "snapshot.metrics is empty"},
		{"metric with no numeric value", `{"metrics": {"a": {}}}`, `metric "a" has no numeric value`},
		{"metrics not an object", `{"metrics": "nope"}`, "snapshot.metrics is missing or not an object"},
		{"snapshot not an object", `[1,2,3]`, "snapshot is not an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", config)
			writeFile(t, dir, "pawl.snapshot.json", tc.snapshot)
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout+res.stderr, tc.wantMsg) {
				t.Errorf("output missing %q: stdout=%s stderr=%s", tc.wantMsg, res.stdout, res.stderr)
			}
		})
	}
}
