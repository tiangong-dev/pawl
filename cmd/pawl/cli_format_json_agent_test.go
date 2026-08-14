package main

import (
	"strings"
	"testing"
)

// A check that fails sets failure_class to "regression". A passing check omits it.
func TestCheckJSONFailureClassOnRegression(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 8}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.FailureClass != "regression" {
		t.Errorf("failure_class = %q, want regression", r.FailureClass)
	}
}

func TestCheckJSONFailureClassOmittedOnPass(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.FailureClass != "" {
		t.Errorf("failure_class = %q, want omitted on a pass", r.FailureClass)
	}
}

// An improved check/diff metric carries the surgical record command; a worse
// or unchanged metric does not.
func TestCheckJSONNextActionOnImprovement(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 3}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	m, ok := metricByID(r, "m")
	if !ok {
		t.Fatalf("metric m absent")
	}
	if !m.Improved {
		t.Errorf("improved = false, want true")
	}
	if m.NextAction != "pawl record --only m" {
		t.Errorf("next_action = %q, want %q", m.NextAction, "pawl record --only m")
	}
}

func TestCheckJSONNextActionOmittedWhenNotImproved(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 8}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	m, _ := metricByID(parseReport(t, res.stdout), "m")
	if m.NextAction != "" {
		t.Errorf("next_action = %q, want omitted on a regression", m.NextAction)
	}
}

func TestRecordJSONOmitsNextActionEvenWhenBetter(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 3}'`}))

	res := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	m, _ := metricByID(parseReport(t, res.stdout), "m")
	if m.NextAction != "" {
		t.Errorf("record next_action = %q, want omitted (already recording)", m.NextAction)
	}
}

// A check that cannot measure still emits the verdict JSON when --format json
// is set: failure_class is could-not-measure, and the human diagnostic stays
// on stderr. Agents must not have to parse unstructured stderr to learn why
// the gate did not run.
func TestCheckJSONCouldNotMeasureMissingSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.ExitCode != 2 {
		t.Errorf("exit_code = %d, want 2", r.ExitCode)
	}
	if r.FailureClass != "could-not-measure" {
		t.Errorf("failure_class = %q, want could-not-measure", r.FailureClass)
	}
	if r.Error == "" {
		t.Errorf("error omitted, want the missing-snapshot diagnostic")
	}
	if !strings.Contains(res.stderr, "pawl record") {
		t.Errorf("stderr must still name the human fix: %s", res.stderr)
	}
}

func TestCheckJSONCouldNotMeasureNamesFailedDimension(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `sh -c 'echo broken >&2; exit 1'`,
	}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.FailureClass != "could-not-measure" {
		t.Errorf("failure_class = %q, want could-not-measure", r.FailureClass)
	}
	if len(r.FailedMetrics) != 1 || r.FailedMetrics[0] != "m" {
		t.Errorf("failed_metrics = %v, want [m]", r.FailedMetrics)
	}
}

// Usage errors happen before a gate is running: --format json must not mint a
// verdict object, or agents will treat a typo as a measurement failure.
func TestCheckJSONUsageErrorHasNoVerdict(t *testing.T) {
	dir := t.TempDir()
	res := runPawl(t, dir, baseEnv(), "frobnicate", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, `"failure_class"`) {
		t.Errorf("usage error minted a verdict JSON: %s", res.stdout)
	}
}
