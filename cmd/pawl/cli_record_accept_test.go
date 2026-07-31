package main

// Integration tests for record's default write gate: a full (or --only)
// record that would write a dimension worse than the committed baseline is
// refused unless --accept-worse says otherwise, and --dry-run previews
// without writing either way. See SPEC.md § Accepted debt.

import (
	"strings"
	"testing"
)

// A plain record that would regress a dimension is refused: exit 1, the
// snapshot on disk is byte-for-byte unchanged, and the refusal names the
// dimension and points at --accept-worse.
func TestRecordRefusesWorseByDefault(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "❌ record refused") || !strings.Contains(res.stdout, "complexity") {
		t.Errorf("stdout missing refusal naming the regressed dimension: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "--accept-worse") {
		t.Errorf("stdout does not point at --accept-worse: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("snapshot was written despite the refusal:\nbefore=%s\nafter=%s", before, after)
	}
}

// --accept-worse writes the regressed value and prints a Pawl-Accept trailer
// line for the caller to add to their commit message.
func TestRecordAcceptWorseWritesAndPrintsTrailer(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--accept-worse")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Pawl-Accept: complexity 15") {
		t.Errorf("stdout missing the Pawl-Accept trailer hint: %s", res.stdout)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["complexity"].Value; got != 15 {
		t.Errorf("complexity = %v, want 15 (written as accepted debt)", got)
	}
}

// --dry-run with no regression previews the table and writes nothing.
func TestRecordDryRunPreviewsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", command: `echo '{"value": 80}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", command: `echo '{"value": 83}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "🔍 dry run") || !strings.Contains(res.stdout, "coverage 80→83") {
		t.Errorf("stdout missing the dry-run preview line: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("--dry-run wrote to the snapshot:\nbefore=%s\nafter=%s", before, after)
	}
}

// --dry-run on a regression that was NOT also given --accept-worse still
// exits 1 (matching what a real record would do) and writes nothing.
func TestRecordDryRunMatchesRefusalExitCodeWithoutAcceptWorse(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "❌ record refused") {
		t.Errorf("stdout missing refusal: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("snapshot was written despite the refusal:\nbefore=%s\nafter=%s", before, after)
	}
}

// --dry-run together with --accept-worse previews the accepted-debt trailer
// hint without writing.
func TestRecordDryRunWithAcceptWorsePreviewsTrailerWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run", "--accept-worse")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Pawl-Accept: complexity 15") {
		t.Errorf("stdout missing the previewed trailer: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("--dry-run wrote to the snapshot:\nbefore=%s\nafter=%s", before, after)
	}
}

// A per-file-count dimension whose file-level breakdown regressed while the
// scalar total held steady (findings moved between files) is still refused —
// the write gate uses the same gate-aware predicate `check` uses, not a
// scalar-only comparison — and the refusal names the actual regressed file
// instead of a confusing "2 → 2".
func TestRecordRefusesNetZeroScalarPerFileCountRegression(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "findings", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:1": 1}}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "findings", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 2}}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (net-zero scalar per-file-count regression)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "a.go") {
		t.Errorf("stdout missing the regressed file detail: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("snapshot was written despite the refusal:\nbefore=%s\nafter=%s", before, after)
	}
}

// The write gate honors higher-is-better direction too: a drop is a
// regression that gets refused.
func TestRecordRefusesWorseHigherIsBetterDirection(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", command: `echo '{"value": 80}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", command: `echo '{"value": 75}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A regression within the dimension's declared tolerance does not trigger
// the write gate — check would pass it too, so record must not refuse it.
func TestRecordDoesNotRefuseWithinTolerance(t *testing.T) {
	dir := t.TempDir()
	tol := 2.0
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", tolerance: &tol,
		command: `echo '{"value": 10}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", tolerance: &tol,
		command: `echo '{"value": 11}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (within declared tolerance)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A refusal under --format json still emits exactly one JSON object (pure
// machine output) at exit 1, mirroring what check does on a regression, and
// dry_run stays false since --dry-run wasn't given.
func TestRecordRefusalJSONStaysPureJSON(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 10}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if rep.ExitCode != 1 {
		t.Errorf("exit_code in report = %d, want 1", rep.ExitCode)
	}
	if rep.DryRun {
		t.Errorf("dry_run = true, want false (no --dry-run given)")
	}
}

// A refusal under --dry-run --format json sets dry_run: true — distinct from
// the same refusal without --dry-run, which must stay false.
func TestRecordRefusalDryRunJSONSetsDryRunField(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 10}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if !rep.DryRun {
		t.Errorf("dry_run = false, want true under --dry-run even on a refusal")
	}
}

// --accept-worse under --format json exposes the accepted dimension(s) in
// accepted_worse, so an automated caller can build the Pawl-Accept trailer
// without re-deriving it from metric status.
func TestRecordAcceptWorseJSONExposesAcceptedWorse(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--accept-worse", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if len(rep.AcceptedWorse) != 1 || rep.AcceptedWorse[0].ID != "complexity" || rep.AcceptedWorse[0].Value != 15 {
		t.Errorf("accepted_worse = %+v, want one entry {complexity 15}", rep.AcceptedWorse)
	}
}

// --dry-run --accept-worse --format json previews accepted_worse without
// writing.
func TestRecordDryRunAcceptWorseJSONPreviewsAcceptedWorse(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 12}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "complexity", direction: "lower-is-better", command: `echo '{"value": 15}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run", "--accept-worse", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if !rep.DryRun {
		t.Errorf("dry_run = false, want true")
	}
	if len(rep.AcceptedWorse) != 1 || rep.AcceptedWorse[0].ID != "complexity" || rep.AcceptedWorse[0].Value != 15 {
		t.Errorf("accepted_worse = %+v, want one entry {complexity 15}", rep.AcceptedWorse)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("--dry-run wrote to the snapshot:\nbefore=%s\nafter=%s", before, after)
	}
}

// A dry-run whose fresh measurement differs only in breakdown, not scalar
// value, must not claim "record would change nothing" — the written bytes
// would differ even though the scalar total held.
func TestRecordDryRunReportsNetZeroScalarBreakdownChange(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "findings", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:1": 1}}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "findings", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 2}}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run", "--accept-worse")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, "would change nothing") {
		t.Errorf("dry-run falsely claims nothing would change despite a breakdown difference: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "breakdown changed") {
		t.Errorf("stdout does not flag the breakdown-only change: %s", res.stdout)
	}
}

// --format json under --dry-run sets dry_run: true and still writes nothing.
func TestRecordJSONDryRunFieldSet(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`,
	}))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 3}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "record", "--dry-run", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	rep := parseReport(t, res.stdout)
	if !rep.DryRun {
		t.Errorf("dry_run = false, want true: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("--dry-run wrote to the snapshot:\nbefore=%s\nafter=%s", before, after)
	}
}

// --dry-run and --accept-worse are record-only flags, rejected as a usage
// error (exit 2) on every other command — mirroring how --only is scoped.
func TestRecordDryRunAndAcceptWorseRejectedOnOtherCommands(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`,
	}))
	for _, args := range [][]string{
		{"check", "--dry-run"},
		{"diff", "--dry-run"},
		{"trend", "--accept-worse"},
		{"baseline-guard", "--dry-run"},
		{"version", "--accept-worse"},
	} {
		res := runPawl(t, dir, baseEnv(), args...)
		if res.exit != 2 {
			t.Errorf("pawl %v exit = %d, want 2\nstdout=%s\nstderr=%s", args, res.exit, res.stdout, res.stderr)
		}
	}
}

// record --only applies the same accepted-debt gate, scoped to the listed
// dimensions: a listed regression is refused without --accept-worse.
func TestRecordOnlyRefusesWorseByDefault(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 9}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "❌ record --only refused") {
		t.Errorf("stdout missing refusal: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("snapshot was written despite the refusal:\nbefore=%s\nafter=%s", before, after)
	}
}

// record --only --dry-run previews the partial-record table without writing.
func TestRecordOnlyDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	))
	before := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 0}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--dry-run")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "🔍 dry run") {
		t.Errorf("stdout missing dry-run marker: %s", res.stdout)
	}
	after := readFile(t, dirJoin(dir, "pawl.snapshot.json"))
	if after != before {
		t.Errorf("--dry-run wrote to the snapshot:\nbefore=%s\nafter=%s", before, after)
	}
}
