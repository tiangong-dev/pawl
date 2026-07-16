package main

// Integration tests for command-scoped flags on the version path, per
// SPEC.md: --since is valid only on `check`, --limit only on `trend`, and
// --only only on `record`. Attaching a scoped flag to `version` (subcommand
// or bare --version) is a usage error: exit 2, a stderr message naming the
// offending flag, and no version text on stdout — the mis-scoped flag must
// never be silently accepted and swallowed.

import (
	"strings"
	"testing"
)

func TestVersionCommandRejectsSinceFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "version", "--since", "HEAD~1")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--since") {
		t.Errorf("stderr = %q, want a mention of --since", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

func TestVersionCommandRejectsLimitFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "version", "--limit", "1")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--limit") {
		t.Errorf("stderr = %q, want a mention of --limit", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

func TestVersionCommandRejectsOnlyFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "version", "--only", "cognitive-complexity")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--only") {
		t.Errorf("stderr = %q, want a mention of --only", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

func TestVersionFlagRejectsLimitFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "--version", "--limit", "1")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--limit") {
		t.Errorf("stderr = %q, want a mention of --limit", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

func TestVersionFlagRejectsSinceFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "--version", "--since", "HEAD~1")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--since") {
		t.Errorf("stderr = %q, want a mention of --since", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

func TestVersionFlagRejectsOnlyFlag(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — a usage error must not require config
	res := runPawl(t, dir, baseEnv(), "--version", "--only", "cognitive-complexity")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsFlagMention(res.stderr, "--only") {
		t.Errorf("stderr = %q, want a mention of --only", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string on a usage error", res.stdout)
	}
}

// TestBareVersionCommandStillWorksInEmptyDir is the control: without any
// mis-scoped flag, `version` still prints the version and exits 0 in a
// directory with no pawl.yaml, proving the scope guard above rejects only
// the offending flag combination, not the version path itself.
func TestBareVersionCommandStillWorksInEmptyDir(t *testing.T) {
	dir := t.TempDir()
	res := runPawl(t, dir, baseEnv(), "version")

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "pawl dev\n" {
		t.Errorf("stdout = %q, want %q", res.stdout, "pawl dev\n")
	}
}

// TestBareVersionFlagStillWorksInEmptyDir is the --version counterpart of
// the control above.
func TestBareVersionFlagStillWorksInEmptyDir(t *testing.T) {
	dir := t.TempDir()
	res := runPawl(t, dir, baseEnv(), "--version")

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "pawl dev\n" {
		t.Errorf("stdout = %q, want %q", res.stdout, "pawl dev\n")
	}
}

func containsFlagMention(stderr, flag string) bool {
	return strings.Contains(stderr, flag)
}

func containsVersionString(stdout string) bool {
	return strings.Contains(stdout, "pawl dev")
}

// TestUnknownCommandWithVersionFlagIsUsageError guards that an unknown
// positional command is never laundered into a successful version print just
// because --version rides along: the unknown command wins, so this is a
// usage error (exit 2, "unknown command" on stderr), not a version dump.
func TestUnknownCommandWithVersionFlagIsUsageError(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — an unknown command is rejected before config is read
	res := runPawl(t, dir, baseEnv(), "frobnicate", "--version")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsUnknownCommandMention(res.stderr) {
		t.Errorf("stderr = %q, want a mention of \"unknown command\"", res.stderr)
	}
	if containsVersionString(res.stdout) {
		t.Errorf("stdout = %q, must not contain the version string when the command is unknown", res.stdout)
	}
}

// TestUnknownCommandOutranksMisScopedFlagDiagnostic guards the diagnostic
// priority order: when both the positional command is unknown AND a flag is
// scoped to a different command, the unknown-command error is reported —
// not a complaint about the mis-scoped flag.
func TestUnknownCommandOutranksMisScopedFlagDiagnostic(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — an unknown command is rejected before config is read
	res := runPawl(t, dir, baseEnv(), "frobnicate", "--limit", "1")

	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if !containsUnknownCommandMention(res.stderr) {
		t.Errorf("stderr = %q, want a mention of \"unknown command\" (not a --limit complaint)", res.stderr)
	}
}

// TestValidCommandVersionFlagStillWins is the control for the two tests
// above: attaching --version to a VALID command still prints the version and
// exits 0, proving the unknown-command guard rejects only unrecognized
// positionals, not the existing "--version wins" behavior for real commands.
func TestValidCommandVersionFlagStillWins(t *testing.T) {
	dir := t.TempDir() // no pawl.yaml present — --version must not require config even on a valid command
	res := runPawl(t, dir, baseEnv(), "check", "--version")

	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q stderr=%q)", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "pawl dev\n" {
		t.Errorf("stdout = %q, want %q", res.stdout, "pawl dev\n")
	}
}

func containsUnknownCommandMention(stderr string) bool {
	return strings.Contains(stderr, "unknown command")
}
