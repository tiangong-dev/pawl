package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnv is a fully explicit git environment isolated from the host's real
// git identity and global config — GIT_CONFIG_NOSYSTEM plus a throwaway HOME
// means these fixtures never touch the developer's actual gitconfig.
func gitEnv(homeDir string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"GIT_CONFIG_NOSYSTEM=1",
	}
}

func runGit(t *testing.T, dir, homeDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// initGitRepo creates a throwaway repo with a committed identity, isolated
// from the host's global git config, and returns its HOME dir for reuse.
func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	homeDir := t.TempDir()
	runGit(t, dir, homeDir, "init", "-q", "-b", "main")
	runGit(t, dir, homeDir, "config", "user.email", "pawl-test@example.com")
	runGit(t, dir, homeDir, "config", "user.name", "pawl test")
	runGit(t, dir, homeDir, "config", "commit.gpgsign", "false")
	return homeDir
}

func gitCommitAll(t *testing.T, dir, homeDir, message string) string {
	t.Helper()
	runGit(t, dir, homeDir, "add", "-A")
	runGit(t, dir, homeDir, "commit", "-q", "-m", message)
	return strings.TrimSpace(runGit(t, dir, homeDir, "rev-parse", "HEAD"))
}

const guardConfig = `dimensions:
  - id: "a"
    title: "A"
    direction: "lower-is-better"
    command: "echo '{\"value\": 1}'"
`

// A missing <ref> argument cannot produce an honest comparison.
func TestGuardMissingRefExitsTwo(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	gitCommitAll(t, dir, homeDir, "initial")

	res := runPawl(t, dir, gitEnv(homeDir), "guard")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// An unresolvable ref is a loud error (exit 2, not a silent skip) naming the
// bad ref — a typo must not disable the anti-tamper gate.
func TestGuardUnresolvableRefExitsTwo(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	gitCommitAll(t, dir, homeDir, "initial")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", "totally-bogus-ref-xyz")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "totally-bogus-ref-xyz") {
		t.Errorf("output does not mention the unresolvable ref: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// A ref that resolves fine but predates the snapshot is a legitimate skip
// (exit 0), distinct from an unresolvable ref (exit 2).
func TestGuardRefWithoutSnapshotSkips(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	base := gitCommitAll(t, dir, homeDir, "initial, no snapshot yet")

	res := runPawl(t, dir, gitEnv(homeDir), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res2.exit != 0 {
		t.Fatalf("guard exit = %d, want 0 (ref predates the snapshot)\nstdout=%s\nstderr=%s",
			res2.exit, res2.stdout, res2.stderr)
	}
}

// A working-tree snapshot that regressed against the version committed at
// <ref> fails with a violation line; equal or better passes with a
// consistency message.
func TestGuardViolationAndConsistency(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	t.Run("worse working tree fails with a violation line", func(t *testing.T) {
		writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null}}}`+"\n")
		res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
		if res.exit != 1 {
			t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stdout, "a: 5 → 8") {
			t.Errorf("stdout missing violation line: %s", res.stdout)
		}
	})

	t.Run("equal working tree passes", func(t *testing.T) {
		writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
		res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("better working tree passes", func(t *testing.T) {
		writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":3,"unit":"count","breakdown":null}}}`+"\n")
		res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
}

// A metric removed in the working tree is a warning, not a failure — the
// orphan check (a check/diff concern) covers that dishonesty, not
// guard.
func TestGuardRemovedMetricWarnsNotFails(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null},`+
			`"b":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline with two metrics")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "⚠️") {
		t.Errorf("stdout missing off-CI warning marker: %s", res.stdout)
	}

	ciEnv := append(gitEnv(homeDir), "GITHUB_ACTIONS=true")
	ciRes := runPawl(t, dir, ciEnv, "guard", base)
	if ciRes.exit != 0 {
		t.Fatalf("CI exit = %d, want 0\nstdout=%s\nstderr=%s", ciRes.exit, ciRes.stdout, ciRes.stderr)
	}
	if !strings.Contains(ciRes.stdout, "::warning::") {
		t.Errorf("CI stdout missing ::warning:: annotation: %s", ciRes.stdout)
	}
}

// The recorded tolerance from the COMMITTED baseline is honored, since
// guard has no config dimensions to fall back on.
func TestGuardHonorsRecordedTolerance(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null,"tolerance":2}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline with tolerance")

	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":{"direction":"lower-is-better","value":6,"unit":"count","breakdown":null,"tolerance":2}}}`+"\n")
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("within recorded tolerance: exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null,"tolerance":2}}}`+"\n")
	res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res2.exit != 1 {
		t.Fatalf("beyond recorded tolerance: exit = %d, want 1\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
}

// A missing working-tree snapshot cannot be compared honestly, even when a
// baseline exists at <ref>.
func TestGuardMissingWorkingTreeSnapshotExitsTwo(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	if err := os.Remove(filepath.Join(dir, "pawl.snapshot.json")); err != nil {
		t.Fatalf("removing working tree snapshot: %v", err)
	}

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A commit between <ref> and HEAD carrying a `Pawl-Accept: <id> <value>`
// trailer that covers the regressed value downgrades the violation to an
// accepted notice: exit 0.
func TestGuardHonorsAcceptTrailer(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null}}}`+"\n")
	gitCommitAll(t, dir, homeDir, "accept the regression\n\nPawl-Accept: a 8\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "accepted regression") || !strings.Contains(res.stdout, "a: 5 → 8") {
		t.Errorf("stdout missing the accepted-regression notice: %s", res.stdout)
	}

	ciEnv := append(gitEnv(homeDir), "GITHUB_ACTIONS=true")
	ciRes := runPawl(t, dir, ciEnv, "guard", base)
	if ciRes.exit != 0 {
		t.Fatalf("CI exit = %d, want 0\nstdout=%s\nstderr=%s", ciRes.exit, ciRes.stdout, ciRes.stderr)
	}
	if !strings.Contains(ciRes.stdout, "::notice::") {
		t.Errorf("CI stdout missing ::notice:: annotation: %s", ciRes.stdout)
	}
}

// A trailer that declares a smaller accepted value than the actual regression
// does not cover it — the working tree moved past what was accepted.
func TestGuardTrailerMustCoverActualValue(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null}}}`+"\n")
	gitCommitAll(t, dir, homeDir, "accept a smaller regression than what actually happened\n\nPawl-Accept: a 6\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (trailer declared 6, actual is 8)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "a: 5 → 8") {
		t.Errorf("stdout missing the violation line: %s", res.stdout)
	}
}

// A trailer with a non-numeric value is ignored, not treated as acceptance —
// a malformed trailer must never silently disable the gate.
func TestGuardMalformedTrailerIsIgnored(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null}}}`+"\n")
	gitCommitAll(t, dir, homeDir, "malformed trailer\n\nPawl-Accept: a not-a-number\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (malformed trailer must not be honored)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Multiple Pawl-Accept trailers for the same id across separate commits in
// ref..HEAD are combined, taking the most-permissive (worst) declared value —
// NOT just the most recent one. The earlier trailer here declares the more
// permissive value (9); a buggy "only the last trailer counts" implementation
// would see only the later, stricter one (6), which does not cover the
// actual regressed value (8), and wrongly reject.
func TestGuardMultipleTrailersTakeTheWorstDeclaredValue(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "committed baseline")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":8,"unit":"count","breakdown":null}}}`+"\n")
	gitCommitAll(t, dir, homeDir, "an earlier, more permissive accept\n\nPawl-Accept: a 9\n")
	writeFile(t, dir, "note.txt", "unrelated change so the second commit is non-empty\n")
	gitCommitAll(t, dir, homeDir, "a later, stricter accept that alone would not cover the regression\n\nPawl-Accept: a 6\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (the earlier trailer's more permissive value must still count)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// baseline-guard was renamed, not aliased. A CI step still calling the old name
// must fail loudly: silently accepting it — or silently ignoring it — would drop
// the anti-tamper gate while the job stayed green, which is the exact outcome
// this command exists to prevent.
func TestBaselineGuardIsGone(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	gitCommitAll(t, dir, homeDir, "initial")

	res := runPawl(t, dir, gitEnv(homeDir), "baseline-guard", "HEAD")
	if res.exit != 2 {
		t.Fatalf("baseline-guard exit = %d, want 2 (unknown command)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "guard") {
		t.Errorf("the unknown-command error does not name the replacement: %s", res.stderr)
	}
}
