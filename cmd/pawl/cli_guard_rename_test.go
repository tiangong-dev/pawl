package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// markerGoFile is a Go file carrying n MARKER comment lines, the offender the
// guardPerFileConfig dimension counts.
func markerGoFile(n int) string {
	var b strings.Builder
	b.WriteString("package a\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "// MARKER %d\n", i)
	}
	return b.String()
}

// recordAcceptWorse snapshots the tree, allowing the per-file movement a rename
// produces: the committed baseline still names the old path, so a plain record
// refuses until the change is recorded explicitly.
func recordAcceptWorse(t *testing.T, dir string, env []string) {
	t.Helper()
	res := runPawl(t, dir, env, "record", "--accept-worse")
	if res.exit != 0 {
		t.Fatalf("record --accept-worse: exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// guardRenamedBase commits a snapshot of the offenders in from, renames it to
// to, snapshots again and commits that, returning the base ref. The rename must
// be committed: guard reads renames from merge-base(ref, HEAD)..HEAD.
func guardRenamedBase(t *testing.T, dir, homeDir, from, to string, offenders int) string {
	t.Helper()
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, from, markerGoFile(offenders))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	base := gitCommitAll(t, dir, homeDir, "baseline: offenders in the old name")

	runGit(t, dir, homeDir, "mv", from, to)
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "pure rename")
	return base
}

// A pure rename moves existing offenders to a new path; guard maps the base
// count onto git's destination so the move itself is not new debt.
func TestGuardIgnoresPureRename(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	base := guardRenamedBase(t, dir, homeDir, "a.go", "b.go", 3)

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 for a pure rename\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// The mapping must not become a blanket excuse: a rename that also adds an
// offender is still a per-file regression, and the line says where the base
// count came from.
func TestGuardCatchesOffenderAddedByRenamedFile(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	base := guardRenamedBase(t, dir, homeDir, "a.go", "b.go", 3)

	writeFile(t, dir, "b.go", markerGoFile(4))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "add an offender in the renamed file")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "3 → 4 (renamed from a.go)") {
		t.Errorf("stdout does not name b.go's mapped base count: %s", res.stdout)
	}
}

// A copy is new code that duplicates existing code, not a move of it: the
// original is still there and the copy's offenders are new, so git's rename
// filter does not report it and guard must not excuse it.
func TestGuardDoesNotMapCopies(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "a.go", markerGoFile(3))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	base := gitCommitAll(t, dir, homeDir, "baseline: three offenders in a.go")

	writeFile(t, dir, "b.go", markerGoFile(3))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "copy a.go to b.go")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 for a copied file\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "b.go 0 → 3") || strings.Contains(res.stdout, "renamed from") {
		t.Errorf("stdout should report b.go as new, not as a rename: %s", res.stdout)
	}
}

// Splitting one file into two is whatever git reports: at most one of the
// halves can be the rename, the other is a file that did not exist — so it
// regresses from 0 and guard fails.
func TestGuardOnSplitFileFollowsGitDetection(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "a.go", markerGoFile(8))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	base := gitCommitAll(t, dir, homeDir, "baseline: eight offenders in a.go")

	runGit(t, dir, homeDir, "rm", "-q", "a.go")
	writeFile(t, dir, "b.go", markerGoFile(4))
	writeFile(t, dir, "c.go", markerGoFile(4))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "split a.go")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (one half is a new file)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "0 → 4") {
		t.Errorf("stdout does not report the newly created half: %s", res.stdout)
	}
}

// Rename paths are repo-relative while breakdown keys are relative to the
// config dir, which may be a repository subdirectory whose own name and file
// names carry spaces and non-ASCII characters.
func TestGuardMapsRenameInsideNonASCIIConfigSubdir(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	sub := filepath.Join(dir, "svc 服务")
	writeFile(t, dir, "svc 服务/pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "svc 服务/a b.go", markerGoFile(3))
	recordAcceptWorse(t, sub, gitEnv(homeDir))
	base := gitCommitAll(t, dir, homeDir, "baseline in the config subdir")

	runGit(t, dir, homeDir, "mv", "svc 服务/a b.go", "svc 服务/文件 c.go")
	recordAcceptWorse(t, sub, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "rename with spaces and non-ASCII names")

	res := runPawl(t, sub, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Rename detection is pinned to --find-renames: a repository that turns
// diff.renames off must not change guard's verdict.
func TestGuardRenameDetectionIgnoresDiffRenamesConfig(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	runGit(t, dir, homeDir, "config", "diff.renames", "false")
	base := guardRenamedBase(t, dir, homeDir, "a.go", "b.go", 3)

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 with diff.renames=false\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// When git cannot answer — here the ref shares no history with HEAD — guard
// compares without mapping, which is stricter, and says so instead of leaving
// the author to guess why a pure rename failed.
func TestGuardRenameDetectionUnavailableFallsBackToPerPath(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "a.go", markerGoFile(3))
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "baseline: three offenders in a.go")

	runGit(t, dir, homeDir, "checkout", "-q", "--orphan", "unrelated")
	gitCommitAll(t, dir, homeDir, "root commit holding the same snapshot")
	runGit(t, dir, homeDir, "checkout", "-q", "main")

	runGit(t, dir, homeDir, "mv", "a.go", "b.go")
	recordAcceptWorse(t, dir, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "pure rename")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", "unrelated")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (an unmapped rename reads as a new file)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "rename detection") || !strings.Contains(res.stdout, "unavailable") {
		t.Errorf("stdout does not report that rename detection was unavailable: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, "b.go 0 → 3") {
		t.Errorf("stdout should show the unmapped per-path comparison: %s", res.stdout)
	}
}
