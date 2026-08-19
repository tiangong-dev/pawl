package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runPawlStdin is runPawl with something on the process's stdin — the only way
// to exercise `--current -`.
func runPawlStdin(t *testing.T, dir string, env []string, stdin string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(pawlBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exit: cmd.ProcessState.ExitCode()}
}

// A two-dimension config whose values come from files the test can rewrite
// between runs, so "measured then" and "measures now" can be made to differ.
func measureConfig(t *testing.T, dir string, a, b int) {
	t.Helper()
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", a))
	writeFile(t, dir, "b.txt", strings.Repeat("x\n", b))
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `printf '{"value": %s}' "$(wc -l < a.txt | tr -d ' ')"`},
		dimDef{id: "b", direction: "lower-is-better", command: `printf '{"value": %s}' "$(wc -l < b.txt | tr -d ' ')"`},
	))
}

// measure runs the dimensions and prints the numbers, nothing else: no verdict,
// no baseline read. It is the "what are the numbers right now" answer that
// otherwise requires asking for a judgement you did not want.
func TestMeasureEmitsTheNumbersWithoutAVerdict(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)

	res := runPawl(t, dir, baseEnv(), "measure")
	if res.exit != 0 {
		t.Fatalf("measure exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	var doc struct {
		Metrics map[string]struct {
			Value float64 `json:"value"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("stdout is not a measurement document: %v\nstdout=%s", err, res.stdout)
	}
	if doc.Metrics["a"].Value != 3 || doc.Metrics["b"].Value != 5 {
		t.Fatalf("metrics = %+v, want a=3 b=5", doc.Metrics)
	}
	if strings.Contains(res.stdout, "status") || strings.Contains(res.stdout, "same") {
		t.Fatalf("measure must not render a verdict, got:\n%s", res.stdout)
	}
	// No snapshot exists, and measure never needed one.
	if _, err := os.Stat(filepath.Join(dir, "pawl.snapshot.json")); err == nil {
		t.Fatal("measure wrote a snapshot; it must only print")
	}
}

// The document measure prints and the snapshot record writes are one format.
// That is what makes `pawl measure > pawl.snapshot.json` mean what it looks
// like, and what lets check read either one.
func TestMeasureDocumentIsTheSnapshotFormat(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)

	measured := runPawl(t, dir, baseEnv(), "measure")
	if measured.exit != 0 {
		t.Fatalf("measure exit = %d\nstderr=%s", measured.exit, measured.stderr)
	}
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	if got := readFile(t, filepath.Join(dir, "pawl.snapshot.json")); got != measured.stdout {
		t.Fatalf("measure document and snapshot differ:\nmeasure=%q\nsnapshot=%q", measured.stdout, got)
	}
}

// check --current judges numbers it did not produce. The dimensions must not
// run again: re-measuring for the verdict is how a gate ends up judging a
// different state than the one it reported.
func TestCheckCurrentJudgesTheSuppliedNumbersWithoutRemeasuring(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	measured := runPawl(t, dir, baseEnv(), "measure")
	// The files now say something worse than the document does. A run that
	// re-measured would fail; a run that honours --current must not.
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 99))
	writeFile(t, dir, "measurement.json", measured.stdout)

	res := runPawl(t, dir, baseEnv(), "check", "--current", "measurement.json")
	if res.exit != 0 {
		t.Fatalf("check --current exit = %d, want 0 — it re-measured instead of using the document\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "measuring a…") {
		t.Fatalf("check --current ran the dimensions:\n%s", res.stderr)
	}
}

// A regression in the supplied document is still a regression: --current
// changes where the numbers come from, never what the gate does with them.
func TestCheckCurrentStillFailsOnARegressionInTheDocument(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 9))
	measured := runPawl(t, dir, baseEnv(), "measure")
	writeFile(t, dir, "measurement.json", measured.stdout)

	res := runPawl(t, dir, baseEnv(), "check", "--current", "measurement.json")
	if res.exit != 1 {
		t.Fatalf("check --current exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// `pawl measure | pawl check --current -` is the whole point: pawl as a stage
// in a pipeline, not only its source.
func TestCheckCurrentReadsStdin(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	measured := runPawl(t, dir, baseEnv(), "measure")

	res := runPawlStdin(t, dir, baseEnv(), measured.stdout, "check", "--current", "-")
	if res.exit != 0 {
		t.Fatalf("check --current - exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "same") {
		t.Fatalf("expected a clean verdict, got:\n%s", res.stdout)
	}
}

// A document missing a configured dimension is a measurement failure naming it.
// Silently judging the dimensions that happen to be present would let a gate
// shrink without anyone saying so.
func TestCheckCurrentRefusesADocumentMissingADimension(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "partial.json", `{"metrics": {"a": {"direction": "lower-is-better", "value": 3, "unit": "count"}}}`)

	res := runPawl(t, dir, baseEnv(), "check", "--current", "partial.json", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	var verdict struct {
		FailureClass  string   `json:"failure_class"`
		FailedMetrics []string `json:"failed_metrics"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &verdict); err != nil {
		t.Fatalf("stdout is not a verdict: %v\n%s", err, res.stdout)
	}
	if verdict.FailureClass != "could-not-measure" {
		t.Fatalf("failure_class = %q, want could-not-measure", verdict.FailureClass)
	}
	if len(verdict.FailedMetrics) != 1 || verdict.FailedMetrics[0] != "b" {
		t.Fatalf("failed_metrics = %v, want [b]", verdict.FailedMetrics)
	}
}

// record --only --current locks in exactly the numbers the check reported, so
// the recorded baseline is the state that was verified rather than a second,
// later measurement of a moving tree.
func TestRecordOnlyCurrentWritesTheSuppliedNumbers(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 9, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 2)) // improved
	measured := runPawl(t, dir, baseEnv(), "measure")
	writeFile(t, dir, "measurement.json", measured.stdout)
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 7)) // moved again after measuring

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--current", "measurement.json")
	if res.exit != 0 {
		t.Fatalf("record --only --current exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["a"].Value; got != 2 {
		t.Fatalf("recorded a = %v, want 2 — the value that was measured, not a re-measurement", got)
	}
	if got := snap.Metrics["b"].Value; got != 5 {
		t.Fatalf("recorded b = %v, want the preserved 5", got)
	}
}

// A worse value in the supplied document meets the same refusal a measured one
// does — --current must not become a way around accepted-debt bookkeeping.
func TestRecordCurrentStillRefusesWorseWithoutAcceptWorse(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 9))
	measured := runPawl(t, dir, baseEnv(), "measure")
	writeFile(t, dir, "measurement.json", measured.stdout)

	res := runPawl(t, dir, baseEnv(), "record", "--current", "measurement.json")
	if res.exit != 1 {
		t.Fatalf("record --current exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["a"].Value; got != 3 {
		t.Fatalf("snapshot changed to %v despite the refusal", got)
	}
}

// A malformed document fails before anything runs, so a typo in the path does
// not cost a full measurement pass before reporting itself.
func TestCurrentRejectsAMalformedDocument(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	writeFile(t, dir, "junk.json", "not json at all")

	res := runPawl(t, dir, baseEnv(), "check", "--current", "junk.json")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "junk.json") {
		t.Fatalf("stderr should name the document, got:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "measuring a…") {
		t.Fatalf("the dimensions ran before the document was rejected:\n%s", res.stderr)
	}
}

func TestCurrentRejectedOnOtherCommands(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	for _, command := range []string{"measure", "rank", "trend"} {
		res := runPawl(t, dir, baseEnv(), command, "--current", "x.json")
		if res.exit != 2 {
			t.Fatalf("%s --current exit = %d, want 2\nstderr=%s", command, res.exit, res.stderr)
		}
		if !strings.Contains(res.stderr, "--current is only valid") {
			t.Fatalf("%s: stderr should reject the flag, got:\n%s", command, res.stderr)
		}
	}
}

// measure emits one document; a format flag would imply there is a choice.
func TestMeasureRejectsFormatFlag(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	res := runPawl(t, dir, baseEnv(), "measure", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("measure --format json exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "--format is not valid on `measure`") {
		t.Fatalf("stderr should reject --format, got:\n%s", res.stderr)
	}
}

// measure honours --only, so a scoped measurement can drive a scoped check.
func TestMeasureOnlyScopesTheDocument(t *testing.T) {
	dir := t.TempDir()
	measureConfig(t, dir, 3, 5)
	res := runPawl(t, dir, baseEnv(), "measure", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("measure --only exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, `"a"`) || strings.Contains(res.stdout, `"b"`) {
		t.Fatalf("document should carry only a, got:\n%s", res.stdout)
	}
}
