package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardRenameWithRelativeGitDiff(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	runGit(t, dir, homeDir, "config", "diff.relative", "true")
	sub := filepath.Join(dir, "src")
	writeFile(t, dir, "src/pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "src/a.go", markerGoFile(3))
	recordAcceptWorse(t, sub, gitEnv(homeDir))
	base := gitCommitAll(t, dir, homeDir, "baseline")
	runGit(t, dir, homeDir, "mv", "src/a.go", "src/b.go")
	recordAcceptWorse(t, sub, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "rename")

	res := runPawl(t, sub, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("pure rename: exit=%d stdout=%s stderr=%s", res.exit, res.stdout, res.stderr)
	}

	writeFile(t, dir, "src/b.go", markerGoFile(4))
	recordAcceptWorse(t, sub, gitEnv(homeDir))
	gitCommitAll(t, dir, homeDir, "add offender")
	res = runPawl(t, sub, gitEnv(homeDir), "guard", base)
	if res.exit != 1 || !strings.Contains(res.stdout, "3 → 4 (renamed from a.go)") {
		t.Fatalf("added offender: exit=%d stdout=%s stderr=%s", res.exit, res.stdout, res.stderr)
	}
}
