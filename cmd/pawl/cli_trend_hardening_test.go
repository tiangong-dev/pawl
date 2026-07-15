package main

import (
	"os"
	"strings"
	"testing"
)

// A historical snapshot that parses as JSON but has an invalid shape (a metric
// with no numeric value) must be skipped with a warning — never turned into a
// measured 0. Guards pawl's honesty contract on the trend path.
func TestTrendSkipsInvalidShapeSnapshotWithoutFabricatingZero(t *testing.T) {
	dir, home := newTrendRepo(t)
	m := map[string]trendMetricValue{"a": {direction: "lower-is-better", unit: "count", value: 5}}
	commitSnapshotAt(t, dir, home, m, "v1", "2026-01-01T00:00:00")

	// A shape-invalid snapshot: metric "a" has no value.
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","unit":"count","breakdown":null}}}`+"\n")
	gitCommitAllDated(t, dir, home, "bad-shape", "2026-01-02T00:00:00")

	m["a"] = trendMetricValue{direction: "lower-is-better", unit: "count", value: 3}
	commitSnapshotAt(t, dir, home, m, "v2", "2026-01-03T00:00:00")

	res := runPawl(t, dir, baseEnv(), "trend", "a", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("trend exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	rep := parseTrendReport(t, res.stdout)
	if len(rep.Metrics) != 1 || len(rep.Metrics[0].Points) != 2 {
		t.Fatalf("want 2 points (the two valid snapshots), got %+v", rep.Metrics)
	}
	for _, p := range rep.Metrics[0].Points {
		if p.Value != 5 && p.Value != 3 {
			t.Errorf("point value %v is neither valid snapshot (5, 3) — a shape-invalid commit became a fake point", p.Value)
		}
	}
	if !strings.Contains(res.stderr, "invalid shape") {
		t.Errorf("expected a warning about the invalid-shape commit on stderr, got: %s", res.stderr)
	}
}

// A commit where the snapshot cannot be read (it deleted the file) is skipped
// loudly, not silently — and the surrounding valid commits still plot.
func TestTrendWarnsOnUnreadableCommitInsteadOfSilentSkip(t *testing.T) {
	dir, home := newTrendRepo(t)
	m := map[string]trendMetricValue{"a": {direction: "lower-is-better", unit: "count", value: 5}}
	commitSnapshotAt(t, dir, home, m, "v1", "2026-01-01T00:00:00")

	if err := os.Remove(dirJoin(dir, "pawl.snapshot.json")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	gitCommitAllDated(t, dir, home, "delete-snapshot", "2026-01-02T00:00:00")

	m["a"] = trendMetricValue{direction: "lower-is-better", unit: "count", value: 3}
	commitSnapshotAt(t, dir, home, m, "v2", "2026-01-03T00:00:00")

	res := runPawl(t, dir, baseEnv(), "trend", "a")
	if res.exit != 0 {
		t.Fatalf("trend exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "cannot read") && !strings.Contains(res.stderr, "cannot read") {
		t.Errorf("expected a loud skip note for the delete commit, got stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// trend reads config only for the snapshot path, so a measurement config that
// full-load rejects (here: zero dimensions) must not block viewing history.
func TestTrendWorksWithInvalidMeasurementConfig(t *testing.T) {
	dir, home := newTrendRepo(t)
	m := map[string]trendMetricValue{"a": {direction: "lower-is-better", unit: "count", value: 5}}
	commitSnapshotAt(t, dir, home, m, "v1", "2026-01-01T00:00:00")

	// A config full LoadConfig rejects ("declares no dimensions") but whose
	// snapshot path is still resolvable.
	writeFile(t, dir, "pawl.yaml", "dimensions: []\n")

	res := runPawl(t, dir, baseEnv(), "trend", "a")
	if res.exit != 0 {
		t.Fatalf("trend exit = %d, want 0 (trend must not validate the measurement config)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "5") {
		t.Errorf("trend did not show the committed value 5:\n%s", res.stdout)
	}
}
