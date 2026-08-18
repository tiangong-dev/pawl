package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// exitCodeConfig renders a one-dimension pawl.yaml whose body carries verbatim
// extra lines — `valid_exit_codes: [0, 1]`, and optionally an extract form.
func exitCodeConfig(command string, extra ...string) string {
	var b strings.Builder
	b.WriteString("dimensions:\n")
	b.WriteString("  - id: \"m\"\n")
	b.WriteString("    title: \"m\"\n")
	b.WriteString("    direction: \"lower-is-better\"\n")
	fmt.Fprintf(&b, "    command: %q\n", command)
	for _, line := range extra {
		fmt.Fprintf(&b, "    %s\n", line)
	}
	return b.String()
}

// The default contract is unchanged: a command dimension that says nothing
// about exit codes still treats any non-zero exit as a measurement failure.
// This is the compatibility guarantee — valid_exit_codes only ever widens, and
// only where a config asks for it.
func TestCommandDimensionStillFailsOnNonZeroExitByDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeConfig(`echo '{"value": 3}'; exit 1`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "exit status 1") {
		t.Fatalf("stderr should name the exit code, got:\n%s", res.stderr)
	}
}

// A tool that reports findings through exit 1 is measurable once the config
// says so — the case that previously forced either a tool-specific builtin or
// an `|| true` suffix.
func TestValidExitCodesAcceptsAToolThatSignalsFindingsWithExitOne(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeConfig(`echo '{"value": 7, "unit": "issues"}'; exit 1`,
		"valid_exit_codes: [0, 1]"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["m"].Value; got != 7 {
		t.Fatalf("value = %v, want 7", got)
	}
}

// The whole point of declaring the set instead of appending `|| true`: an exit
// code outside it is still a measurement failure. `|| true` would have turned
// this crash into a clean measurement of whatever partial stdout existed.
func TestValidExitCodesStillRejectsAnUnlistedExitCode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeConfig(`echo '{"value": 0}'; exit 2`,
		"valid_exit_codes: [0, 1]"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "exited with 2") || !strings.Contains(res.stderr, "[0, 1]") {
		t.Fatalf("stderr should name the offending code and the accepted set, got:\n%s", res.stderr)
	}
}

// The contract belongs to the command, not to the JSON adapter shape, so it
// applies identically to a declarative extract dimension — where the `|| true`
// workaround was most common, since line-oriented tools exit non-zero on match.
func TestValidExitCodesAppliesToExtractDimensions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeConfig(`printf 'a.go:1: x\nb.go:2: y\n'; exit 1`,
		"valid_exit_codes: [0, 1]",
		"gate: \"per-file-count\"",
		"extract:",
		"  regex: '^(?P<path>[^:]+):(?P<line>\\d+):'"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	m := snap.Metrics["m"]
	if m.Value != 2 {
		t.Fatalf("value = %v, want 2", m.Value)
	}
	if m.Breakdown["a.go:1"] != 1 || m.Breakdown["b.go:2"] != 1 {
		t.Fatalf("breakdown = %v, want one finding per file", m.Breakdown)
	}
}

// A builtin dimension runs no command of the config's choosing, so accepting
// the option there would describe an exit code nobody can observe.
func TestValidExitCodesRejectedOnABuiltinDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", strings.Join([]string{
		"dimensions:",
		"  - id: \"len\"",
		"    title: \"len\"",
		"    direction: \"lower-is-better\"",
		"    builtin: \"file-length\"",
		"    valid_exit_codes: [0, 1]",
		"    options:",
		"      threshold: 100",
		"      include: [\"**/*.go\"]",
		"",
	}, "\n"))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "valid_exit_codes") {
		t.Fatalf("stderr should name the rejected option, got:\n%s", res.stderr)
	}
}

// A malformed set aborts at config load rather than silently degrading to the
// default contract, which would quietly re-narrow a gate the config widened.
func TestValidExitCodesRejectsANonIntegerEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", exitCodeConfig(`echo '{"value": 1}'`,
		"valid_exit_codes: [0, \"one\"]"))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "valid_exit_codes") {
		t.Fatalf("stderr should name the rejected option, got:\n%s", res.stderr)
	}
}
