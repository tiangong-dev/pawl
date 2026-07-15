package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// record --only re-measures only the named dimension(s) and preserves every
// other configured dimension's committed value verbatim — a regression on an
// unlisted dimension must not be silently blessed the way a full record
// would bless it.
func TestRecordOnlyPreservesUnlistedDimensionsVerbatim(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 3}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 5}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 7}'`},
	))

	// a improves to 2; b regresses to 9 — but b is not in --only, so its
	// regression must not be locked in.
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 2}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 9}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 7}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("record --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["a"].Value; got != 2 {
		t.Errorf("a = %v, want 2 (re-measured)", got)
	}
	if got := snap.Metrics["b"].Value; got != 5 {
		t.Errorf("b = %v, want 5 (preserved, NOT the new regressed 9)", got)
	}
	if got := snap.Metrics["c"].Value; got != 7 {
		t.Errorf("c = %v, want 7 (preserved, untouched)", got)
	}
}

// --only accepts a comma-separated list; every listed id is re-measured and
// every other configured dimension is preserved.
func TestRecordOnlyMultipleIDs(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 2}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 3}'`},
	))

	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 11}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 22}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 33}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a,b")
	if res.exit != 0 {
		t.Fatalf("record --only a,b exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["a"].Value; got != 11 {
		t.Errorf("a = %v, want 11 (re-measured)", got)
	}
	if got := snap.Metrics["b"].Value; got != 22 {
		t.Errorf("b = %v, want 22 (re-measured)", got)
	}
	if got := snap.Metrics["c"].Value; got != 3 {
		t.Errorf("c = %v, want 3 (preserved, untouched)", got)
	}
}

// Only the listed dimensions are measured: an unrelated dimension whose
// adapter is currently broken does not block --only from locking in a win
// elsewhere, even though a plain full record would fail outright.
func TestRecordOnlySkipsMeasuringUnlistedBrokenAdapter(t *testing.T) {
	dir := t.TempDir()
	// Both dims start healthy so a full record can establish the baseline.
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 4}'`},
		dimDef{id: "broken", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))

	// Now break "broken"'s adapter and improve "a".
	config := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "broken", direction: "lower-is-better", command: `exit 1`},
	)
	writeFile(t, dir, "pawl.yaml", config)

	// A plain full record is impossible: the broken adapter fails the run.
	plain := runPawl(t, dir, baseEnv(), "record")
	if plain.exit != 2 {
		t.Fatalf("plain record with a broken adapter exit = %d, want 2\nstdout=%s\nstderr=%s",
			plain.exit, plain.stdout, plain.stderr)
	}

	// --only a never touches the broken adapter, so it succeeds.
	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("record --only a exit = %d, want 0 (broken unrelated dim must not block)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["a"].Value; got != 1 {
		t.Errorf("a = %v, want 1 (re-measured)", got)
	}
	if got := snap.Metrics["broken"].Value; got != 1 {
		t.Errorf("broken = %v, want 1 (preserved from the prior good measurement)", got)
	}
}

// A dimension that IS listed in --only is still measured, so its own broken
// adapter aborts the run exactly as it would in a full record.
func TestRecordOnlyListedBrokenAdapterFails(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 4}'`},
		dimDef{id: "broken", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))

	config := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "broken", direction: "lower-is-better", command: `exit 1`},
	)
	writeFile(t, dir, "pawl.yaml", config)

	res := runPawl(t, dir, baseEnv(), "record", "--only", "broken")
	if res.exit != 2 {
		t.Fatalf("record --only broken exit = %d, want 2 (the listed dim's own adapter is broken)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["broken"].Value; got != 1 {
		t.Errorf("broken = %v, want unchanged 1 (failed measurement must not clobber the snapshot)", got)
	}
	if got := snap.Metrics["a"].Value; got != 4 {
		t.Errorf("a = %v, want unchanged 4 (whole run aborted, nothing written)", got)
	}
}

// An id in --only that names no configured dimension is a usage error,
// exit 2, naming the id — checked before anything is measured or written.
func TestRecordOnlyUnknownIDExitsTwoBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))
	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	before := readFile(t, snapPath)

	res := runPawl(t, dir, baseEnv(), "record", "--only", "nope")
	if res.exit != 2 {
		t.Fatalf("record --only nope exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "nope") {
		t.Errorf("output does not name the unknown id \"nope\": stdout=%s stderr=%s", res.stdout, res.stderr)
	}

	after := readFile(t, snapPath)
	if after != before {
		t.Errorf("snapshot was modified by an unknown-id --only run: before=%s after=%s", before, after)
	}
}

// "Preserve the rest" is meaningless without a baseline: --only against a
// config with no existing snapshot is a usage error, exit 2.
func TestRecordOnlyMissingSnapshotExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 2 {
		t.Fatalf("record --only a with no snapshot exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "pawl.snapshot.json")); err == nil {
		t.Errorf("no snapshot should have been created")
	}
}

// A shape error in the existing snapshot is refused rather than silently
// treated as an empty baseline, and the malformed file is left untouched.
func TestRecordOnlyMalformedSnapshotExitsTwoAndDoesNotOverwrite(t *testing.T) {
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})

	cases := []struct {
		name     string
		snapshot string
	}{
		{"empty metrics object", `{"metrics": {}}`},
		{"metric with no numeric value", `{"metrics": {"a": {}}}`},
		{"metrics not an object", `{"metrics": "nope"}`},
		{"snapshot not an object", `[1,2,3]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", config)
			snapPath := writeFile(t, dir, "pawl.snapshot.json", tc.snapshot)

			res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
			if res.exit != 2 {
				t.Fatalf("record --only a over a malformed snapshot exit = %d, want 2\nstdout=%s\nstderr=%s",
					res.exit, res.stdout, res.stderr)
			}
			if got := readFile(t, snapPath); got != tc.snapshot {
				t.Errorf("malformed snapshot was overwritten: got=%s want unchanged=%s", got, tc.snapshot)
			}
		})
	}
}

// A snapshot metric whose dimension is no longer configured is dropped by
// --only exactly as a full record drops it — an orphan never survives.
func TestRecordOnlyDropsOrphanedMetric(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "gone", direction: "lower-is-better", command: `echo '{"value": 9}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 3}'`},
	))

	// "gone" is removed from config, leaving it an orphan in the snapshot.
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 2}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 3}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("record --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if _, ok := snap.Metrics["gone"]; ok {
		t.Errorf("orphaned metric \"gone\" must be dropped, snapshot = %+v", snap.Metrics)
	}
	if got := snap.Metrics["a"].Value; got != 2 {
		t.Errorf("a = %v, want 2 (re-measured)", got)
	}
	if got := snap.Metrics["c"].Value; got != 3 {
		t.Errorf("c = %v, want 3 (preserved)", got)
	}
}

// A configured dimension that is neither listed in --only nor present in the
// existing snapshot stays absent — --only never invents a value for it.
func TestRecordOnlyLeavesUnlistedNewDimensionAbsent(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))

	// "b" is a brand-new dimension, not in the existing snapshot and not
	// named in --only.
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 2}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 5}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("record --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if _, ok := snap.Metrics["b"]; ok {
		t.Errorf("unlisted new dimension \"b\" must stay absent, snapshot = %+v", snap.Metrics)
	}
	if got := snap.Metrics["a"].Value; got != 2 {
		t.Errorf("a = %v, want 2", got)
	}
}

// --only is valid only on record; on any other command it is a usage error,
// exit 2, never silently accepted or ignored.
func TestRecordOnlyRejectedOnOtherCommands(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})
	writeFile(t, dir, "pawl.yaml", config)
	base := gitCommitAll(t, dir, homeDir, "base")
	mustRecord(t, dir, config)

	cases := []struct {
		name string
		args []string
	}{
		{"check", []string{"check", "--only", "a"}},
		{"diff", []string{"diff", "--only", "a"}},
		{"baseline-guard", []string{"baseline-guard", "--only", "a", base}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runPawl(t, dir, gitEnv(homeDir), tc.args...)
			if res.exit != 2 {
				t.Fatalf("%s --only exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s",
					tc.name, res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// An empty --only list is a usage error, exit 2 — both the bare empty string
// and a value that splits into zero non-empty ids.
func TestRecordOnlyEmptyListExitsTwo(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	))

	for _, val := range []string{"", ","} {
		t.Run("value="+val, func(t *testing.T) {
			res := runPawl(t, dir, baseEnv(), "record", "--only", val)
			if res.exit != 2 {
				t.Fatalf("record --only %q exit = %d, want 2\nstdout=%s\nstderr=%s", val, res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// --format json on record --only emits the same record verdict object shape
// as a full record, with the merged current values: freshly measured for the
// listed dimension, carried over for the preserved ones.
func TestRecordOnlyFormatJSONEmitsMergedVerdict(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 3}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 6}'`},
	))

	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 99}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("record --only a --format json exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.Command != "record" {
		t.Errorf("command = %q, want record", r.Command)
	}
	if r.SchemaVersion == 0 {
		t.Errorf("schema_version = 0, want a set schema version")
	}
	ma, ok := metricByID(r, "a")
	if !ok {
		t.Fatalf("metrics missing \"a\": %+v", r.Metrics)
	}
	if ma.Current != 1 {
		t.Errorf("a.current = %v, want 1 (freshly measured)", ma.Current)
	}
	mb, ok := metricByID(r, "b")
	if !ok {
		t.Fatalf("metrics missing \"b\": %+v", r.Metrics)
	}
	if mb.Current != 6 {
		t.Errorf("b.current = %v, want 6 (preserved, not the new 99)", mb.Current)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if snap.Metrics["b"].Value != 6 {
		t.Errorf("snapshot b = %v, want 6 (preserved)", snap.Metrics["b"].Value)
	}
}

// The text-format footer for a partial record names the re-recorded id(s)
// and the count of preserved metrics, replacing the plain full-record
// "snapshot written" line with something that discloses what was and was
// not re-blessed.
func TestRecordOnlyTextFooterNamesRecordedIDsAndPreservedCount(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 10}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 20}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 40}'`},
		dimDef{id: "d", direction: "lower-is-better", command: `echo '{"value": 80}'`},
	))

	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 11}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 20}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 40}'`},
		dimDef{id: "d", direction: "lower-is-better", command: `echo '{"value": 80}'`},
	))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("record --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	idRe := regexp.MustCompile(`\ba\b`)
	if !idRe.MatchString(res.stdout) {
		t.Errorf("footer does not name the re-recorded id \"a\": stdout=%s", res.stdout)
	}
	// b, c, d (3 dims) are preserved.
	countRe := regexp.MustCompile(`\b3\b`)
	if !countRe.MatchString(res.stdout) {
		t.Errorf("footer does not name the preserved-metric count (3): stdout=%s", res.stdout)
	}
}
