package main

import (
	"strings"
	"testing"
)

// patternCountSinceConfig is a per-file-count dimension over the pattern-count
// builtin (breakdown keys "path:line") — the deterministic offender source for
// --since scoping tests: adding a marker line is a new offender on an added
// line, so the only unimplemented thing under test is --since itself.
func patternCountSinceConfig() string {
	return buildConfig("", dimDef{
		id: "nolint", title: "NOLINT suppressions",
		direction: "lower-is-better", gate: "per-file-count",
		builtin:     "pattern-count",
		optionLines: []string{`pattern = "NOLINT"`, `include = ["**/*.go"]`},
	})
}

// --since is valid only on check; on any other command it is a usage error
// (exit 2), never silently accepted.
func TestSinceOnlyValidOnCheck(t *testing.T) {
	for _, cmd := range []string{"diff", "record"} {
		t.Run(cmd, func(t *testing.T) {
			dir := t.TempDir()
			homeDir := initGitRepo(t, dir)
			writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
			writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
			base := gitCommitAll(t, dir, homeDir, "base")

			res := runPawl(t, dir, gitEnv(homeDir), cmd, "--since", base)
			if res.exit != 2 {
				t.Fatalf("%s --since exit = %d, want 2 (usage error)\nstdout=%s\nstderr=%s",
					cmd, res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// check --since still requires a snapshot — it is the gate narrowed to new
// code, not a standalone new-code scanner. Missing snapshot → exit 2.
func TestSinceRequiresSnapshot(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	base := gitCommitAll(t, dir, homeDir, "base")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (no snapshot)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// check --since in a directory that is not a git repo cannot compute a
// changed-line set → exit 2, never a silent "nothing changed".
func TestSinceNotAGitRepoExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	mustRecord(t, dir, patternCountSinceConfig())

	res := runPawl(t, dir, baseEnv(), "check", "--since", "HEAD")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (not a git repo)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// An unresolvable --since ref is a loud failure (exit 2), not a silent pass —
// a typo must not disable the diff scoping.
func TestSinceUnresolvableRefExitsTwo(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", "no-such-ref")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (unresolvable ref)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A new offender whose "path:line" lies on a line ADDED since <ref> is a live
// regression → exit 1. This is the core "new code must not regress" gate.
func TestSinceNewOffenderOnAddedLineFails(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	// Baseline: one pre-existing offender on line 2.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// Add a new marker on a brand-new line 3.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")
	gitCommitAll(t, dir, homeDir, "add offender on new line")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (new offender on an added line)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// A new offender (vs the snapshot) whose line was NOT added since <ref> is
// pre-existing debt: exit 0, and text output reports it as suppressed/exempted.
// The snapshot lags reality by one marker that already existed at <ref>.
func TestSinceNewOffenderOnUnchangedLineIsExempted(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	// Record the snapshot with a single offender (line 2)…
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	// …then add a second marker (line 3) WITHOUT re-recording, and commit both
	// the file and the now-stale snapshot as the baseline ref.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT preexisting\n")
	base := gitCommitAll(t, dir, homeDir, "base with pre-existing debt")

	// An unrelated added line (line 4) — the offender on line 3 is unchanged.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT preexisting\nplain tail\n")
	gitCommitAll(t, dir, homeDir, "unrelated change")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (offender on an unchanged line is pre-existing)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stdout), "suppress") &&
		!strings.Contains(strings.ToLower(res.stdout), "exempt") {
		t.Errorf("stdout does not report the exempted pre-existing regression: %s", res.stdout)
	}
}
