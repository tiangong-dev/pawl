package main

import (
	"os"
	"strings"
	"testing"
)

// renameFixture records a snapshot for a.go while it still carries two
// pre-existing NOLINT offenders, then commits it as the --since baseline.
// Moving the file afterwards makes it look like a brand-new file to a per-file
// comparison, so the rename handling under test is the only thing deciding
// whether those two offenders are charged as new debt.
func renameFixture(t *testing.T) (dir, homeDir, base string) {
	t.Helper()
	dir = t.TempDir()
	homeDir = initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT one\n// NOLINT two\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base = gitCommitAll(t, dir, homeDir, "base")
	return dir, homeDir, base
}

// liveAndSuppressed splits a dimension's regressions for assertions.
func liveAndSuppressed(m jsonMetric) (live, suppressed []jsonRegression) {
	for _, r := range m.Regressions {
		if r.Suppressed {
			suppressed = append(suppressed, r)
		} else {
			live = append(live, r)
		}
	}
	return live, suppressed
}

// A staged `git mv` is a rename, not a deletion plus a new file: the moved
// file's existing offenders are pre-existing debt, so --since stays green. That
// must hold however the developer's git is configured to detect renames — with
// detection off, git prints the add/delete pair and every line of the moved file
// would count as added.
func TestSinceGitMvKeepsMovedOffendersOutOfNewDebt(t *testing.T) {
	for _, renames := range []string{"default", "false"} {
		t.Run("diff.renames="+renames, func(t *testing.T) {
			dir, homeDir, base := renameFixture(t)
			if renames != "default" {
				runGit(t, dir, homeDir, "config", "diff.renames", renames)
			}
			runGit(t, dir, homeDir, "mv", "a.go", "moved.go")
			gitCommitAll(t, dir, homeDir, "rename")

			res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
			if res.exit != 0 {
				t.Fatalf("exit = %d, want 0 (a rename must not turn existing offenders into new debt)\nstdout=%s\nstderr=%s",
					res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "count as all-new") {
				t.Errorf("a staged rename is detectable, so it must not draw the untracked-file hint:\n%s", res.stderr)
			}
		})
	}
}

// After a `git mv`, only lines genuinely added to the moved file are new debt:
// the appended offender fails the gate while the two moved-in offenders stay
// exempt. A pure rename would contribute no added lines at all.
func TestSinceRenamedFileChargesOnlyAddedLines(t *testing.T) {
	dir, homeDir, base := renameFixture(t)
	runGit(t, dir, homeDir, "mv", "a.go", "moved.go")
	writeFile(t, dir, "moved.go", "package a\n// NOLINT one\n// NOLINT two\n// NOLINT added\n")
	gitCommitAll(t, dir, homeDir, "rename and append")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (the appended offender is new debt)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	m, ok := metricByID(parseReport(t, res.stdout), "nolint")
	if !ok {
		t.Fatalf("verdict has no nolint metric:\n%s", res.stdout)
	}
	live, suppressed := liveAndSuppressed(m)
	if len(live) != 1 || live[0].Path == nil || *live[0].Path != "moved.go" ||
		live[0].Line == nil || *live[0].Line != 4 {
		t.Errorf("live regressions = %+v, want exactly moved.go:4", live)
	}
	// Only the two moved-in offenders are exempt; the moved file's scalar grew
	// to 3 and is listed as a suppressed total alongside them.
	movedIn := 0
	for _, r := range suppressed {
		if r.Kind == "per-file-count" {
			movedIn++
		}
	}
	if movedIn != 2 {
		t.Errorf("suppressed regressions = %+v, want the two moved-in offenders", suppressed)
	}
	if strings.Contains(res.stderr, "count as all-new") {
		t.Errorf("a staged rename must not draw the untracked-file hint:\n%s", res.stderr)
	}
}

// A plain (unstaged) `mv` leaves the new path untracked, so git cannot pair it
// with the deleted old path: every line of the moved file counts as added and
// the gate fails on offenders the move did not create. pawl cannot write the
// index, so it names the cause and the fixes on stderr — progress output, which
// `-q` silences. The JSON verdict on stdout is unaffected either way.
func TestSincePlainMvWarnsUntrackedCountsAsAllNew(t *testing.T) {
	dir, homeDir, base := renameFixture(t)
	if err := os.Rename(dirJoin(dir, "a.go"), dirJoin(dir, "moved.go")); err != nil {
		t.Fatalf("plain mv: %v", err)
	}

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (an untracked moved file counts as all-new)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "untracked file(s) count as all-new") ||
		!strings.Contains(res.stderr, "git mv") {
		t.Errorf("stderr must name the untracked-as-new cause and how to fix it:\n%s", res.stderr)
	}
	m, ok := metricByID(parseReport(t, res.stdout), "nolint")
	if !ok {
		t.Fatalf("verdict has no nolint metric:\n%s", res.stdout)
	}
	if live, _ := liveAndSuppressed(m); len(live) != 2 {
		t.Errorf("live regressions = %+v, want both moved-in offenders (whole file counts as new)", live)
	}

	quiet := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "-q")
	if quiet.exit != 1 {
		t.Fatalf("quiet exit = %d, want 1\nstdout=%s\nstderr=%s", quiet.exit, quiet.stdout, quiet.stderr)
	}
	if strings.Contains(quiet.stderr, "count as all-new") {
		t.Errorf("-q must silence the advisory:\n%s", quiet.stderr)
	}
}
