package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// exitCodeExtractConfig renders an extract: exit-code dimension whose body
// carries verbatim extra lines — `valid_exit_codes: [0, 1, 2]`, a timeout.
func exitCodeExtractConfig(command string, extra ...string) string {
	return extractConfig("s", "lower-is-better", "", command,
		append([]string{"extract: exit-code"}, extra...)...)
}

// extract: exit-code measures a pass/fail script by its own exit code: 0 is a
// pass (value 0), 1 is a failing check (value 1), and a recorded pass that
// starts failing is a regression — the case a `&& printf '0\n'` wrapper turned
// into "could not measure". The script's output is ignored.
func TestExtractExitCodeMeasuresPassFailScript(t *testing.T) {
	dir := t.TempDir()
	// The verdict lives outside the script so the same command can pass and
	// then fail; the script is noisy on both streams to prove exit-code reads
	// neither.
	writeFile(t, dir, "check.sh", "#!/bin/sh\ncat verdict.txt >&2\necho \"checked $(wc -l < verdict.txt) file(s)\"\nexit \"$(cat verdict.txt)\"\n")
	writeFile(t, dir, "verdict.txt", "0\n")
	writeFile(t, dir, "pawl.yaml", exitCodeExtractConfig("sh check.sh"))

	rec := runPawl(t, dir, baseEnv(), "record")
	if rec.exit != 0 {
		t.Fatalf("record (pass) exit = %d, want 0\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	m := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["s"]
	if m.Value != 0 {
		t.Errorf("value after a passing check = %v, want 0", m.Value)
	}
	if m.Unit != "failures" {
		t.Errorf("unit = %q, want %q", m.Unit, "failures")
	}

	writeFile(t, dir, "verdict.txt", "1\n")
	failed := runPawl(t, dir, baseEnv(), "check")
	if failed.exit != 1 {
		t.Fatalf("check (failing script) exit = %d, want 1\nstdout=%s\nstderr=%s", failed.exit, failed.stdout, failed.stderr)
	}
	if !strings.Contains(failed.stdout, "total 0 → 1") {
		t.Errorf("stdout missing the 0 → 1 regression detail: %s", failed.stdout)
	}
}

// The default contract for exit-code is [0, 1], not the [0] every other form
// inherits: a failing check must be measurable without declaring anything. The
// widening stops there — exit 2 is a broken check, so it is a measurement
// failure (exit 2) that names the accepted set, never a failing check.
func TestExtractExitCodeDefaultsToZeroOrOne(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeExtractConfig("exit 2"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{"exited with 2", "[0, 1]"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr should name %q, got:\n%s", want, res.stderr)
		}
	}
}

// Declaring the wider set is what turns a third exit code into a value: an
// explicit valid_exit_codes replaces the form's default rather than extending
// it.
func TestExtractExitCodeHonorsDeclaredValidExitCodes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeExtractConfig("exit 2", "valid_exit_codes: [0, 1, 2]"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["s"].Value; got != 1 {
		t.Errorf("value for an accepted exit 2 = %v, want 1", got)
	}
}

// A check that never produced a verdict is not a failing check: a timeout and
// a command that does not exist are both measurement failures.
func TestExtractExitCodeTimeoutAndMissingCommandAreMeasurementFailures(t *testing.T) {
	cases := []struct {
		name    string
		command string
		extra   []string
	}{
		{"timeout", "sleep 5", []string{`timeout: "50ms"`}},
		{"missing command", "definitely-not-a-real-command-zz", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", exitCodeExtractConfig(tc.command, tc.extra...))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// extract: exit-code declares how the number is derived, so adding it to an
// existing dimension redefines the measurement. The recorded number must not
// be compared across that line — check refuses it instead of reporting the
// difference as a regression.
func TestExtractExitCodeIsPartOfTheMeasurementDefinition(t *testing.T) {
	dir := t.TempDir()
	// One command that satisfies both contracts: valid exec JSON and exit 0.
	command := `printf '{"value": 0}\n'`
	writeFile(t, dir, "pawl.yaml", extractConfig("s", "lower-is-better", "", command))
	if rec := runPawl(t, dir, baseEnv(), "record"); rec.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	before := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["s"].Definition

	writeFile(t, dir, "pawl.yaml", exitCodeExtractConfig(command))
	measure := runPawl(t, dir, baseEnv(), "measure")
	if measure.exit != 0 {
		t.Fatalf("measure exit = %d, want 0\nstdout=%s\nstderr=%s", measure.exit, measure.stdout, measure.stderr)
	}
	writeFile(t, dir, "current.json", measure.stdout)
	after := readSnapshot(t, filepath.Join(dir, "current.json")).Metrics["s"].Definition
	if after == before {
		t.Errorf("adding extract: exit-code left the definition unchanged: %q", before)
	}

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check across the redefinition exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "measurement definition") {
		t.Errorf("diagnostic does not name the definition change: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// A builtin dimension runs no command of the config's choosing, so the
// exit-code form — which reads an exit code nobody can observe — is rejected
// at config load like every other extract form.
func TestExtractExitCodeRejectedOnABuiltinDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["**/*.go"]
    extract: exit-code
`)

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "extract") {
		t.Errorf("stderr should name the rejected option, got:\n%s", res.stderr)
	}
}
