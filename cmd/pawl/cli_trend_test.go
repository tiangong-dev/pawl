package main

// Integration tests for `pawl trend`: it reconstructs each metric's value
// over time from the committed snapshot file's own git history — read-only,
// never measures. See SPEC.md § Trend. Shared fixtures/helpers live in
// cli_trend_helpers_test.go.

import (
	"fmt"
	"strings"
	"testing"
)

// A metric's series, oldest first, prints as a header plus one row per
// commit with a Δ from the previous point ("—" for the oldest point).
func TestTrendTextShowsMetricSeriesOldestToNewest(t *testing.T) {
	dir, homeDir := newTrendRepo(t)

	sha1 := commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5},
	}, "v1", "2026-01-01 12:00:00 +0000")
	sha2 := commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 3},
	}, "v2", "2026-01-02 12:00:00 +0000")
	sha3 := commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 4},
	}, "v3", "2026-01-03 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend")
	if res.exit != 0 {
		t.Fatalf("trend exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "file-length  (lower-is-better, files > 500 lines)") {
		t.Errorf("stdout missing metric header: %s", res.stdout)
	}
	if strings.Contains(res.stdout, "showing") {
		t.Errorf("a 3-commit history under the default --limit 20 must not print a truncation line: %s", res.stdout)
	}

	short1 := gitShortSHA(t, dir, homeDir, sha1)
	short2 := gitShortSHA(t, dir, homeDir, sha2)
	short3 := gitShortSHA(t, dir, homeDir, sha3)

	idx1, idx2, idx3 := strings.Index(res.stdout, short1), strings.Index(res.stdout, short2), strings.Index(res.stdout, short3)
	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatalf("stdout missing one of the commit short shas: %s", res.stdout)
	}
	if !(idx1 < idx2 && idx2 < idx3) {
		t.Errorf("rows are not ordered oldest to newest: stdout=%s", res.stdout)
	}

	wantRows := map[string][]string{
		short1: {short1, "2026-01-01", "5", "—"},
		short2: {short2, "2026-01-02", "3", "-2"},
		short3: {short3, "2026-01-03", "4", "+1"},
	}
	for sha, want := range wantRows {
		got := trendRowFields(t, res.stdout, sha)
		if len(got) < 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
			t.Errorf("row for %s = %v, want %v", sha, got, want)
		}
	}
}

// `--format json` emits one point per kept commit, oldest first, each with a
// non-empty commit and date.
func TestTrendJSONShowsPointsOldestToNewest(t *testing.T) {
	dir, homeDir := newTrendRepo(t)

	value := func(v float64) map[string]trendMetricValue {
		return map[string]trendMetricValue{"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: v}}
	}
	commitSnapshotAt(t, dir, homeDir, value(5), "v1", "2026-01-01 12:00:00 +0000")
	commitSnapshotAt(t, dir, homeDir, value(3), "v2", "2026-01-02 12:00:00 +0000")
	commitSnapshotAt(t, dir, homeDir, value(4), "v3", "2026-01-03 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("trend --format json exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	report := parseTrendReport(t, res.stdout)
	if report.Command != "trend" {
		t.Errorf("command = %q, want trend", report.Command)
	}
	if report.Snapshot != "pawl.snapshot.json" {
		t.Errorf("snapshot = %q, want pawl.snapshot.json", report.Snapshot)
	}
	if len(report.Metrics) != 1 {
		t.Fatalf("metrics count = %d, want 1: %+v", len(report.Metrics), report.Metrics)
	}
	m := report.Metrics[0]
	if m.ID != "file-length" {
		t.Errorf("metric id = %q, want file-length", m.ID)
	}
	if len(m.Points) != 3 {
		t.Fatalf("points count = %d, want 3: %+v", len(m.Points), m.Points)
	}
	wantValues := []float64{5, 3, 4}
	for i, p := range m.Points {
		if p.Value != wantValues[i] {
			t.Errorf("point[%d].value = %v, want %v", i, p.Value, wantValues[i])
		}
		if p.Commit == "" {
			t.Errorf("point[%d] has an empty commit", i)
		}
		if p.Date == "" {
			t.Errorf("point[%d] has an empty date", i)
		}
	}
}

// Metrics are sorted by id; a metric introduced only in a later commit shows
// a gap (fewer points) instead of crashing.
func TestTrendMultipleMetricsSortedByIDWithGapForLateMetric(t *testing.T) {
	dir, homeDir := newTrendRepo(t)

	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"a": {direction: "lower-is-better", unit: "count", value: 10},
	}, "only a", "2026-01-01 12:00:00 +0000")
	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"a": {direction: "lower-is-better", unit: "count", value: 8},
		"b": {direction: "higher-is-better", unit: "widgets", value: 1},
	}, "add b", "2026-01-02 12:00:00 +0000")
	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"a": {direction: "lower-is-better", unit: "count", value: 6},
		"b": {direction: "higher-is-better", unit: "widgets", value: 2},
	}, "grow both", "2026-01-03 12:00:00 +0000")

	jsonRes := runPawl(t, dir, gitEnv(homeDir), "trend", "--format", "json")
	if jsonRes.exit != 0 {
		t.Fatalf("trend --format json exit = %d, want 0\nstdout=%s\nstderr=%s", jsonRes.exit, jsonRes.stdout, jsonRes.stderr)
	}
	report := parseTrendReport(t, jsonRes.stdout)
	if len(report.Metrics) != 2 {
		t.Fatalf("metrics count = %d, want 2: %+v", len(report.Metrics), report.Metrics)
	}
	if report.Metrics[0].ID != "a" || report.Metrics[1].ID != "b" {
		t.Fatalf("metrics not sorted by id: [%s, %s]", report.Metrics[0].ID, report.Metrics[1].ID)
	}
	if len(report.Metrics[0].Points) != 3 {
		t.Errorf("metric a present in every commit should have 3 points, got %d", len(report.Metrics[0].Points))
	}
	if len(report.Metrics[1].Points) != 2 {
		t.Errorf("metric b introduced in the second commit should have a gap (2 points, not 3), got %d", len(report.Metrics[1].Points))
	}

	textRes := runPawl(t, dir, gitEnv(homeDir), "trend")
	if textRes.exit != 0 {
		t.Fatalf("trend exit = %d, want 0\nstdout=%s\nstderr=%s", textRes.exit, textRes.stdout, textRes.stderr)
	}
	idxA := strings.Index(textRes.stdout, "a  (")
	idxB := strings.Index(textRes.stdout, "b  (")
	if idxA == -1 || idxB == -1 {
		t.Fatalf("text output missing a header for one of the metrics: %s", textRes.stdout)
	}
	if idxA > idxB {
		t.Errorf("text output metrics are not in sorted-id order: %s", textRes.stdout)
	}
}

// `<id>` restricts output to that one metric.
func TestTrendFiltersToSingleMetricByID(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"a": {direction: "lower-is-better", unit: "count", value: 10},
		"b": {direction: "lower-is-better", unit: "count", value: 20},
	}, "both", "2026-01-01 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "a")
	if res.exit != 0 {
		t.Fatalf("trend a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "a  (") {
		t.Errorf("stdout missing filtered metric a: %s", res.stdout)
	}
	if strings.Contains(res.stdout, "b  (") {
		t.Errorf("filtering to id a must not print metric b: %s", res.stdout)
	}
}

// An `<id>` absent from every historical snapshot cannot be trended.
func TestTrendUnknownMetricIDExitsTwo(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"a": {direction: "lower-is-better", unit: "count", value: 10},
	}, "a only", "2026-01-01 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "does-not-exist")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (unknown metric id)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, `no metric "does-not-exist"`) {
		t.Errorf("output missing the unknown-metric message: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// `--limit <n>` keeps only the n most recent commits and prints a loud
// truncation line — never a silent cap.
func TestTrendLimitCapsToMostRecentWithLoudTruncationLine(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	dates := []string{
		"2026-01-01 12:00:00 +0000", "2026-01-02 12:00:00 +0000", "2026-01-03 12:00:00 +0000",
		"2026-01-04 12:00:00 +0000", "2026-01-05 12:00:00 +0000",
	}
	shas := make([]string, len(dates))
	for i, d := range dates {
		shas[i] = commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
			"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: float64(i + 1)},
		}, fmt.Sprintf("v%d", i+1), d)
	}

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "--limit", "2")
	if res.exit != 0 {
		t.Fatalf("trend --limit 2 exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "showing 2 of 5 snapshots (--limit 0 for all)") {
		t.Errorf("stdout missing the loud truncation line: %s", res.stdout)
	}

	short1 := gitShortSHA(t, dir, homeDir, shas[0])
	short4 := gitShortSHA(t, dir, homeDir, shas[3])
	short5 := gitShortSHA(t, dir, homeDir, shas[4])
	if !strings.Contains(res.stdout, short4) || !strings.Contains(res.stdout, short5) {
		t.Errorf("stdout missing the two most recent commits: %s", res.stdout)
	}
	if strings.Contains(res.stdout, short1) {
		t.Errorf("stdout should not include the oldest commit when limited to the 2 most recent: %s", res.stdout)
	}
}

// `--limit 0` means all snapshots, with no truncation line.
func TestTrendLimitZeroShowsAllSnapshotsWithoutTruncationLine(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	for i := 1; i <= 5; i++ {
		commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
			"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: float64(i)},
		}, fmt.Sprintf("v%d", i), fmt.Sprintf("2026-01-0%d 12:00:00 +0000", i))
	}

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "--limit", "0")
	if res.exit != 0 {
		t.Fatalf("trend --limit 0 exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, "showing") {
		t.Errorf("--limit 0 must not print a truncation line: %s", res.stdout)
	}
	if count := strings.Count(res.stdout, "2026-01-0"); count != 5 {
		t.Errorf("expected all 5 dated rows with --limit 0, found %d: %s", count, res.stdout)
	}
}

// A commit whose snapshot is unparseable JSON is skipped with a warning, not
// aborted — one corrupt historical commit must not kill the whole trend.
func TestTrendSkipsUnparseableHistoricalSnapshotWithWarning(t *testing.T) {
	dir, homeDir := newTrendRepo(t)

	sha1 := commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5},
	}, "good1", "2026-01-01 12:00:00 +0000")

	writeFile(t, dir, "pawl.snapshot.json", "not json")
	badSha := gitCommitAllDated(t, dir, homeDir, "bad snapshot", "2026-01-02 12:00:00 +0000")

	sha3 := commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 4},
	}, "good2", "2026-01-03 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend")
	if res.exit != 0 {
		t.Fatalf("trend exit = %d, want 0 (a corrupt historical commit must not abort the whole trend)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "⚠️") {
		t.Errorf("stdout missing the off-CI warning marker for the unparseable commit: %s", res.stdout)
	}

	short1 := gitShortSHA(t, dir, homeDir, sha1)
	short3 := gitShortSHA(t, dir, homeDir, sha3)
	if !strings.Contains(res.stdout, short1) || !strings.Contains(res.stdout, short3) {
		t.Errorf("stdout missing points from the two good commits: %s", res.stdout)
	}

	ciEnv := append(gitEnv(homeDir), "GITHUB_ACTIONS=true")
	ciRes := runPawl(t, dir, ciEnv, "trend")
	if ciRes.exit != 0 {
		t.Fatalf("CI trend exit = %d, want 0\nstdout=%s\nstderr=%s", ciRes.exit, ciRes.stdout, ciRes.stderr)
	}
	if !strings.Contains(ciRes.stdout, "::warning::") {
		t.Errorf("CI stdout missing ::warning:: annotation for the unparseable commit at %s: %s", badSha, ciRes.stdout)
	}
}

// Outside a git repo there is no history to reconstruct.
func TestTrendNotAGitRepoExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", trendConfig)
	writeFile(t, dir, "pawl.snapshot.json", trendSnapshotJSON(map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5},
	}))

	res := runPawl(t, dir, baseEnv(), "trend")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (not a git repo)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A snapshot path that was never committed has no history to walk.
func TestTrendNeverCommittedSnapshotExitsTwo(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	gitCommitAllDated(t, dir, homeDir, "config only, no snapshot yet", "2026-01-01 12:00:00 +0000")

	// The snapshot exists in the working tree but was never committed.
	writeFile(t, dir, "pawl.snapshot.json", trendSnapshotJSON(map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5},
	}))

	res := runPawl(t, dir, gitEnv(homeDir), "trend")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (snapshot path never committed)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "no committed history") {
		t.Errorf("output missing the no-committed-history message: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// `--format codeclimate` has no tabular meaning for `trend`.
func TestTrendFormatCodeclimateIsUsageError(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir, map[string]trendMetricValue{
		"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5},
	}, "v1", "2026-01-01 12:00:00 +0000")

	res := runPawl(t, dir, gitEnv(homeDir), "trend", "--format", "codeclimate")
	if res.exit != 2 {
		t.Fatalf("trend --format codeclimate exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// `--limit` only means something to `trend`; on every other command it is a
// usage error, not a silently ignored flag. Each fixture is built so the
// command would otherwise exit 0, isolating the rejection to --limit itself.
func TestTrendLimitOnNonTrendCommandIsUsageError(t *testing.T) {
	t.Run("check", func(t *testing.T) {
		dir := t.TempDir()
		mustRecord(t, dir, trendConfig)
		res := runPawl(t, dir, baseEnv(), "check", "--limit", "3")
		if res.exit != 2 {
			t.Fatalf("check --limit exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("diff", func(t *testing.T) {
		dir := t.TempDir()
		mustRecord(t, dir, trendConfig)
		res := runPawl(t, dir, baseEnv(), "diff", "--limit", "3")
		if res.exit != 2 {
			t.Fatalf("diff --limit exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("record", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pawl.yaml", trendConfig)
		res := runPawl(t, dir, baseEnv(), "record", "--limit", "3")
		if res.exit != 2 {
			t.Fatalf("record --limit exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("baseline-guard", func(t *testing.T) {
		dir := t.TempDir()
		homeDir := initGitRepo(t, dir)
		writeFile(t, dir, "pawl.yaml", guardConfig)
		writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
		base := gitCommitAll(t, dir, homeDir, "committed baseline")

		res := runPawl(t, dir, gitEnv(homeDir), "baseline-guard", base, "--limit", "3")
		if res.exit != 2 {
			t.Fatalf("baseline-guard --limit exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
}
