package main

import (
	"strings"
	"testing"
)

// Across real agent evaluations against demo/fixture (see demo/README.md),
// --format json was almost never self-initiated — `pawl --help` listing the
// flag was not enough, and even this hint firing didn't change the outcome
// in every run tried since. runPawl's stdout is always a pipe (never a real
// terminal), matching how a script or agent actually invokes pawl, so this
// is the realistic non-interactive case the hint targets.

func onlyDimConfig() string {
	return buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})
}

func TestCheckTextHintsJSONWhenPiped(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, onlyDimConfig())

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "pawl check --format json") {
		t.Errorf("stderr missing the --format json hint: %s", res.stderr)
	}
	if strings.Contains(res.stdout, "--format json") {
		t.Errorf("hint leaked into stdout, must stay on stderr so it never perturbs a script parsing stdout: %s", res.stdout)
	}
}

// The hint is redundant noise once the caller is already using --format
// json — it must not appear there.
func TestCheckJSONFormatOmitsHint(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, onlyDimConfig())

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check --format json exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "--format json") {
		t.Errorf("hint printed even though caller already used --format json: %s", res.stderr)
	}
}

// The hint is specific to `check`, the command an automated loop actually
// gates on — `diff` and `record` are exploratory/state-writing, not the
// inner-loop decision point the hint is aimed at.
func TestDiffAndRecordDoNotHintJSON(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, onlyDimConfig())

	diffRes := runPawl(t, dir, baseEnv(), "diff")
	if strings.Contains(diffRes.stderr, "--format json") {
		t.Errorf("diff printed the check-only hint: %s", diffRes.stderr)
	}

	recordRes := runPawl(t, dir, baseEnv(), "record")
	if strings.Contains(recordRes.stderr, "--format json") {
		t.Errorf("record printed the check-only hint: %s", recordRes.stderr)
	}
}
