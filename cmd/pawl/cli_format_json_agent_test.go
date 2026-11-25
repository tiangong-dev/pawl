package main

import (
	"os"
	"path/filepath"
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

func TestCheckJSONImprovementWithStrictRegressionOmitsNextAction(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", gate: "per-key-value-strict",
		builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
	})
	writeFile(t, dir, "old-a.txt", nLines(11))
	writeFile(t, dir, "old-b.txt", nLines(11))
	mustRecord(t, dir, config)
	if err := os.Remove(filepath.Join(dir, "old-a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "old-b.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "new.txt", nLines(11))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want strict new-key regression\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	m, ok := metricByID(r, "length")
	if !ok {
		t.Fatal("metric length absent")
	}
	if !m.Improved || m.Status != "worse" || m.NextAction != "" {
		t.Errorf("metric improved/status/next_action = %v/%q/%q, want true/worse/omitted", m.Improved, m.Status, m.NextAction)
	}
	if !strings.Contains(res.stdout, `"new_key": true`) || strings.Contains(res.stdout, `"next_action"`) {
		t.Errorf("JSON must mark the strict regression and omit the refused record action: %s", res.stdout)
	}
	textRes := runPawl(t, dir, baseEnv(), "check")
	if textRes.exit != 1 || strings.Contains(textRes.stdout, "pawl record --only length") {
		t.Errorf("text output must report the regression without a record hint (exit=%d): %s", textRes.exit, textRes.stdout)
	}
	ciRes := runPawl(t, dir, baseEnv("GITHUB_ACTIONS=true"), "check")
	if ciRes.exit != 1 || strings.Contains(ciRes.stdout, "::notice::pawl improved:") {
		t.Errorf("CI output must not announce a record action for a regressed metric (exit=%d): %s", ciRes.exit, ciRes.stdout)
	}
}

func TestCheckJSONMultiMetricImprovementHintsOnlyCleanMetric(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "clean", direction: "lower-is-better", command: `echo '{"value":5}'`,
	}, dimDef{
		id: "regressed", direction: "lower-is-better", gate: "per-key-value-strict",
		command: `echo '{"value":10,"breakdown":{"old":10}}'`,
	})
	mustRecord(t, dir, config)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "clean", direction: "lower-is-better", command: `echo '{"value":3}'`,
	}, dimDef{
		id: "regressed", direction: "lower-is-better", gate: "per-key-value-strict",
		command: `echo '{"value":5,"breakdown":{"old":0,"new":5}}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("check exit=%d, want strict regression\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	report := parseReport(t, res.stdout)
	clean, cleanOK := metricByID(report, "clean")
	regressed, regressedOK := metricByID(report, "regressed")
	if !cleanOK || clean.NextAction != "pawl record --only clean" || !regressedOK || !regressed.Improved || regressed.NextAction != "" {
		t.Fatalf("clean/regressed metrics = %+v / %+v", clean, regressed)
	}
	textRes := runPawl(t, dir, baseEnv(), "check")
	if !strings.Contains(textRes.stdout, "pawl record --only clean") || strings.Contains(textRes.stdout, "pawl record --only regressed") {
		t.Errorf("text hints should include only the clean metric: %s", textRes.stdout)
	}
	ciRes := runPawl(t, dir, baseEnv("GITHUB_ACTIONS=true"), "check")
	if !strings.Contains(ciRes.stdout, "::notice::pawl improved: clean") || strings.Contains(ciRes.stdout, "improved: clean, regressed") {
		t.Errorf("CI notice should include only the clean metric: %s", ciRes.stdout)
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

// An exit-2 object must describe the invocation that produced it. A --since run
// that could not resolve its ref is still a since run: reporting mode "full"
// tells an agent branching on mode that the whole gate was measured.
func TestCheckJSONCouldNotMeasureKeepsSinceScope(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	gitCommitAll(t, dir, homeDir, "base")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", "no-such-ref", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.Mode != "since" {
		t.Errorf("mode = %q, want since", r.Mode)
	}
	if r.Since == nil || *r.Since != "no-such-ref" {
		t.Errorf("since = %v, want no-such-ref", r.Since)
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
