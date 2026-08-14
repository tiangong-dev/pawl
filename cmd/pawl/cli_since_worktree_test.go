package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// check --since includes uncommitted working-tree edits: a new offender on a
// line that has not been committed is a live regression, not pre-existing debt.
func TestSinceUncommittedOffenderOnAddedLineFails(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (uncommitted offender on an added line)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// check --since HEAD with only uncommitted work used to see an empty added-line
// set (merge-base..HEAD) and suppress the new offender. The working-tree form
// must fail.
func TestSinceHEADIncludesUncommittedWorkingTree(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", "HEAD")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (--since HEAD must see uncommitted added lines)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// An untracked file is not in git diff; --since must still treat every line
// of it as added so a new offender there fails the gate.
func TestSinceUntrackedFileOffenderFails(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "b.go", "package b\n// NOLINT new file\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (untracked file offender)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "b.go") {
		t.Errorf("stdout should name the untracked file: %s", res.stdout)
	}
}

// An untracked entry that is not a readable regular file — a dangling symlink,
// a socket, a file whose permissions deny reading — must not decide whether the
// gate can produce a verdict. Measurement skips non-regular files too, so an
// offender can never hide behind one.
func TestSinceSkipsUnreadableUntrackedEntry(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	if err := os.Symlink(filepath.Join(dir, "no-such-target"), filepath.Join(dir, "dangling.link")); err != nil {
		t.Skipf("cannot create a symlink here (%v) — this platform has no way to stage the case", err)
	}

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (a dangling untracked symlink is not a measurement failure)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}

	// …and skipping it must not cost the real untracked offender next to it.
	writeFile(t, dir, "b.go", "package b\n// NOLINT new file\n")
	res = runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (untracked offender beside the dangling symlink)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}
