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
