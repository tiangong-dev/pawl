package main

import "testing"

// --since may only ever narrow what it exempts. Both cases here are a way the
// added-line set silently loses an entry, which flips a brand-new offender into
// "pre-existing debt" and takes the gate green on debt it was supposed to stop.

// git marks a file with no trailing newline by emitting `\ No newline at end of
// file` inside the hunk — between the `-` line and the `+` line at --unified=0.
// Counting that marker as a context line advanced the new-side counter, so the
// `+` line was recorded one line too far down and the offender on the line that
// actually changed was never in the added set.
func TestSinceScopesFileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// keep\n// tail")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// Same last line, now an offender, still with no trailing newline.
	writeFile(t, dir, "a.go", "package a\n// keep\n// NOLINT added")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (a no-newline-at-EOF marker must not shift the added line)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// The untracked list arrives NUL-separated from `git ls-files -z`. Trimming the
// whole payload as if it were one string ate the leading blank of the first
// path, so that file failed to stat and dropped out of the added set entirely —
// its offenders then read as pre-existing.
func TestSinceScopesUntrackedFileWithLeadingBlankInName(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// A leading space sorts this first in ls-files output, where the trim hit.
	writeFile(t, dir, " lead.go", "package lead\n// NOLINT new file\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (an untracked offender must count whatever its name starts with)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// The same trim ate the path out of watch's touched-file set, so a file this
// invocation actually edited never reported its headroom.
func TestWatchListsTouchedFileWithLeadingBlankInName(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, " lead.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, " lead.txt", nLines(10))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 1 || r.Watch[0].Path != " lead.txt" {
		t.Fatalf("watch = %+v, want one entry for %q\nstderr=%s", r.Watch, " lead.txt", res.stderr)
	}
}
