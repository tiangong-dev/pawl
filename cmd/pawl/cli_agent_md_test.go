package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readAgentsMD(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	return string(b)
}

// agent-md is a pure print: an adopter redirecting it somewhere (AGENTS.md,
// CLAUDE.md, a docs page) must not silently get a file written too.
const agentBlockOpenMarker = "<!-- pawl:begin -->"

// runPawlShell runs a shell command line with the built pawl binary on PATH,
// so a test can exercise the redirect an adopter actually types.
func runPawlShell(t *testing.T, dir, line string) cliResult {
	t.Helper()
	cmd := exec.Command("sh", "-c", line)
	cmd.Dir = dir
	cmd.Env = append(baseEnv(), "PATH="+filepath.Dir(pawlBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exit: cmd.ProcessState.ExitCode()}
}

func TestAgentMDPrintsWithoutWriting(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "agent-md")
	if res.exit != 0 {
		t.Fatalf("agent-md exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "pawl check") {
		t.Errorf("agent-md stdout does not look like the operating loop: %s", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("agent-md wrote AGENTS.md (stat err = %v)", err)
	}
}

// The block is the only thing an adopter's agent ever sees about this gate —
// pawl's own AGENTS.md is not in their repo. If a trim drops one of these,
// the agent loses a distinct failure mode, so each is asserted by name.
func TestAgentMDBlockCarriesTheLoadBearingRules(t *testing.T) {
	dir := t.TempDir()
	// Collapsed to single spaces: these are assertions about what the block
	// says, and a reflow of the Markdown must not read as a lost rule.
	out := strings.Join(strings.Fields(runPawl(t, dir, baseEnv(), "agent-md").stdout), " ")

	for _, want := range []string{
		"pawl check --format json", // the inner loop itself
		"failure_class",            // branch on why, not just on non-zero
		"could-not-measure",        // exit 2 is an environment problem
		"pawl record --only",       // never re-bless untouched dimensions
		"excluded",                 // a scoped verdict is not a green gate
		"watch",                    // headroom is advisory, judging it is the agent's job
		"pawl rank --format json",  // measure headroom before the edit, not after
		"artifact",                 // a stale report is not the current tree
	} {
		if !strings.Contains(out, want) {
			t.Errorf("agent block never mentions %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "before you call a task done") {
		t.Errorf("agent block does not open with the run-it-yourself rule:\n%s", out)
	}
}

// The one thing a flag could do that a redirect cannot: recognize a copy
// already installed. It survives as advice — stderr note, stdout untouched, exit 0 —
// because two diverging copies of the loop leave an agent worse off than one.
func TestAgentMDWarnsWhenAgentsFileAlreadyCarriesTheBlock(t *testing.T) {
	dir := t.TempDir()
	first := runPawl(t, dir, baseEnv(), "agent-md")
	writeFile(t, dir, "AGENTS.md", "# House rules\n\n"+first.stdout)

	res := runPawl(t, dir, baseEnv(), "agent-md")
	if res.exit != 0 {
		t.Fatalf("agent-md exit = %d, want 0 — the note is advice, not a verdict\nstderr=%s", res.exit, res.stderr)
	}
	if res.stdout != first.stdout {
		t.Errorf("stdout changed because of AGENTS.md; the block must be the same bytes every time")
	}
	if !strings.Contains(res.stderr, "already contains a pawl block") {
		t.Errorf("stderr should warn about the existing block, got:\n%s", res.stderr)
	}
}

// An unrelated AGENTS.md must not produce the note, or it becomes noise nobody
// reads.
func TestAgentMDIsQuietWhenAgentsFileHasNoBlock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "# House rules\n\nRun the linter.\n")

	res := runPawl(t, dir, baseEnv(), "agent-md")
	if res.exit != 0 {
		t.Fatalf("agent-md exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "already contains") {
		t.Errorf("warned about an AGENTS.md that carries no pawl block:\n%s", res.stderr)
	}
}

// Installing the block is a redirect, so pawl must never touch AGENTS.md
// itself — not even the file it warns about.
func TestAgentMDNeverWritesAgentsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "# House rules\n")

	if res := runPawl(t, dir, baseEnv(), "agent-md"); res.exit != 0 {
		t.Fatalf("agent-md exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if got := readAgentsMD(t, dir); got != "# House rules\n" {
		t.Errorf("AGENTS.md changed: %q", got)
	}
}

// The removed --write flag is unknown everywhere now, including on agent-md,
// so a script still passing it fails loudly instead of being ignored.
func TestWriteFlagIsGone(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, onlyDimConfig())

	for _, command := range []string{"agent-md", "check", "record", "init", "rank"} {
		res := runPawl(t, dir, baseEnv(), command, "--write")
		if res.exit != 2 {
			t.Errorf("%s --write exit = %d, want 2 (unknown flag)\nstdout=%s", command, res.exit, res.stdout)
		}
		if !strings.Contains(res.stderr, "--write") {
			t.Errorf("%s --write error does not name the flag: %s", command, res.stderr)
		}
	}
}

// The loop is identical in every repo, so it must not depend on a config —
// a broken or half-written pawl.yaml is exactly when someone reaches for it.
func TestAgentMDNeedsNoConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", "dimensions: [ this is not valid yaml")

	res := runPawl(t, dir, baseEnv(), "agent-md")
	if res.exit != 0 {
		t.Fatalf("agent-md exit = %d with a broken config, want 0\nstderr=%s", res.exit, res.stderr)
	}
}

func TestAgentMDRejectsFormatFlag(t *testing.T) {
	dir := t.TempDir()
	for _, format := range []string{"json", "text-ish"} {
		res := runPawl(t, dir, baseEnv(), "agent-md", "--format", format)
		if res.exit != 2 {
			t.Errorf("agent-md --format %s exit = %d, want 2", format, res.exit)
		}
	}
}

// init is where an adopter meets pawl; if it doesn't name agent-md, the
// command exists but nobody runs it.
func TestInitPointsAtAgentMD(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "agent-md") {
		t.Errorf("init output never mentions agent-md: %s", res.stdout)
	}
}

// The documented install is `pawl agent-md >> AGENTS.md`, which makes stdout
// and AGENTS.md the same file. Checking for an existing block after printing
// therefore finds the block this very run just wrote, and a first install
// warns about itself — so the check has to happen before the write.
func TestAgentMDDoesNotWarnOnAFirstRedirectInstall(t *testing.T) {
	dir := t.TempDir()

	res := runPawlShell(t, dir, "pawl agent-md >> AGENTS.md")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "already contains") {
		t.Fatalf("a first install warned about itself:\n%s", res.stderr)
	}
	if !strings.Contains(readAgentsMD(t, dir), agentBlockOpenMarker) {
		t.Fatal("AGENTS.md did not receive the block")
	}

	// The second one is the case the note exists for.
	again := runPawlShell(t, dir, "pawl agent-md >> AGENTS.md")
	if !strings.Contains(again.stderr, "already contains") {
		t.Fatalf("a second install did not warn:\n%s", again.stderr)
	}
}
