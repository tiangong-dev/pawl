package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The two instruction files pawl can install into, and the tools that load them. Claude Code reads only CLAUDE.md, so installing into AGENTS.md alone installs nothing there.
const (
	agentsFile = "AGENTS.md"
	claudeFile = "CLAUDE.md"
)

const (
	agentBlockOpenMarker  = "<!-- pawl:begin -->"
	agentBlockCloseMarker = "<!-- pawl:end -->"
)

func readInstructionFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// runPawlShell runs a shell command line with the built pawl binary on PATH, so a test can exercise the redirect an adopter actually types.
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

// With no --write and no terminal on both ends, `agent` is a pure print. A prompt here would hang every pipeline and every agent-driven invocation.
func TestAgentPrintsWithoutWritingWhenNotInteractive(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "agent")
	if res.exit != 0 {
		t.Fatalf("agent exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "pawl check") {
		t.Errorf("agent stdout does not look like the operating loop: %s", res.stdout)
	}
	for _, name := range []string{agentsFile, claudeFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("agent wrote %s without --write (stat err = %v)", name, err)
		}
	}
}

// The block is the only thing an adopter's agent ever sees about this gate — pawl's own AGENTS.md is not in their repo. If a trim drops one of these, the agent loses a distinct failure mode, so each is asserted by name.
func TestAgentBlockCarriesTheLoadBearingRules(t *testing.T) {
	dir := t.TempDir()
	// Collapsed to single spaces: these are assertions about what the block says, and a reflow of the Markdown must not read as a lost rule.
	out := strings.Join(strings.Fields(runPawl(t, dir, baseEnv(), "agent").stdout), " ")

	for _, want := range []string{
		"pawl check --format json", // the inner loop itself
		"failure_class",            // branch on why, not just on non-zero
		"could-not-measure",        // exit 2 is an environment problem
		"pawl record --only",       // never re-bless untouched dimensions
		"excluded",                 // a scoped verdict is not a green gate
		"watch",                    // headroom is advisory, judging it is the agent's job
		"pawl rank --format json",  // measure headroom before the edit, not after
		"artifact",                 // a stale report is not the current tree
		"not `pawl measure`",       // the closest miss: numbers without a baseline
	} {
		if !strings.Contains(out, want) {
			t.Errorf("agent block never mentions %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "before you call a task done") {
		t.Errorf("agent block does not open with the run-it-yourself rule:\n%s", out)
	}
}

// Each target writes its own file and leaves the other alone: an adopter on Claude Code who installs into CLAUDE.md must not also get an AGENTS.md they never asked for.
func TestAgentWriteCreatesOnlyTheChosenFile(t *testing.T) {
	for _, tc := range []struct{ target, want, other string }{
		{"agent", agentsFile, claudeFile},
		{"claude", claudeFile, agentsFile},
	} {
		t.Run(tc.target, func(t *testing.T) {
			dir := t.TempDir()

			res := runPawl(t, dir, baseEnv(), "agent", "--write", tc.target)
			if res.exit != 0 {
				t.Fatalf("agent --write %s exit = %d, want 0\nstderr=%s", tc.target, res.exit, res.stderr)
			}
			if got := readInstructionFile(t, dir, tc.want); !strings.Contains(got, agentBlockOpenMarker) {
				t.Errorf("%s did not receive the block:\n%s", tc.want, got)
			}
			if !strings.Contains(res.stdout, tc.want) {
				t.Errorf("stdout does not name the file it wrote: %s", res.stdout)
			}
			if _, err := os.Stat(filepath.Join(dir, tc.other)); !os.IsNotExist(err) {
				t.Errorf("--write %s also touched %s (stat err = %v)", tc.target, tc.other, err)
			}
		})
	}
}

// The instruction file belongs to the adopter, so an install appends to whatever is already there instead of replacing the file.
func TestAgentWriteAppendsToExistingProse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, agentsFile, "# House rules\n\nRun the linter.\n")

	if res := runPawl(t, dir, baseEnv(), "agent", "--write", "agent"); res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	got := readInstructionFile(t, dir, agentsFile)
	if !strings.Contains(got, "Run the linter.") {
		t.Errorf("the adopter's own prose was lost:\n%s", got)
	}
	if !strings.Contains(got, agentBlockOpenMarker) {
		t.Errorf("the block was not appended:\n%s", got)
	}
}

// The reason --write exists at all: a second install replaces the block in place instead of leaving two copies that disagree about how to use the gate. Prose on both sides of the block survives, since pawl is editing a file it does not own.
func TestAgentWriteReplacesAnInstalledBlockInPlace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, agentsFile, "# House rules\n\n"+
		agentBlockOpenMarker+"\nsomething an older pawl printed\n"+agentBlockCloseMarker+"\n\n## Deploy\n\nssh and pray.\n")

	if res := runPawl(t, dir, baseEnv(), "agent", "--write", "agent"); res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	got := readInstructionFile(t, dir, agentsFile)
	if n := strings.Count(got, agentBlockOpenMarker); n != 1 {
		t.Errorf("file carries %d blocks, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "something an older pawl printed") {
		t.Errorf("the stale block survived the update:\n%s", got)
	}
	if !strings.Contains(got, "pawl check --format json") {
		t.Errorf("the current block did not replace it:\n%s", got)
	}
	for _, keep := range []string{"# House rules", "## Deploy", "ssh and pray."} {
		if !strings.Contains(got, keep) {
			t.Errorf("prose %q around the block was lost:\n%s", keep, got)
		}
	}
}

// Running the same install twice must converge, not accumulate.
func TestAgentWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	if res := runPawl(t, dir, baseEnv(), "agent", "--write", "claude"); res.exit != 0 {
		t.Fatalf("first write exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	first := readInstructionFile(t, dir, claudeFile)
	if res := runPawl(t, dir, baseEnv(), "agent", "--write", "claude"); res.exit != 0 {
		t.Fatalf("second write exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	if second := readInstructionFile(t, dir, claudeFile); second != first {
		t.Errorf("a second install changed the file:\nfirst=%q\nsecond=%q", first, second)
	}
}

// Damaged or duplicated markers are an error, not a guess. Every guess pawl could make here silently destroys prose it did not write, so the file is left exactly as found.
func TestAgentWriteRefusesDamagedMarkers(t *testing.T) {
	for name, content := range map[string]string{
		"begin without end": "# Rules\n\n" + agentBlockOpenMarker + "\nhalf a block\n",
		"end without begin": "# Rules\n\nhalf a block\n" + agentBlockCloseMarker + "\n",
		"end before begin":  agentBlockCloseMarker + "\nbackwards\n" + agentBlockOpenMarker + "\n",
		"two blocks": "# Rules\n\n" + agentBlockOpenMarker + "\none\n" + agentBlockCloseMarker + "\n\n" +
			agentBlockOpenMarker + "\ntwo\n" + agentBlockCloseMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, agentsFile, content)

			res := runPawl(t, dir, baseEnv(), "agent", "--write", "agent")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if got := readInstructionFile(t, dir, agentsFile); got != content {
				t.Errorf("the file was modified despite the refusal:\n%s", got)
			}
			if !strings.Contains(res.stderr, agentsFile) {
				t.Errorf("stderr does not name the file it refused: %s", res.stderr)
			}
		})
	}
}

// --write is the one flag that decides where pawl writes, so a typo must never resolve to a default target.
func TestAgentWriteRejectsUnknownAndMissingTargets(t *testing.T) {
	dir := t.TempDir()

	for _, args := range [][]string{
		{"agent", "--write", "cursor"},
		{"agent", "--write", ""},
		{"agent", "--write"},
	} {
		res := runPawl(t, dir, baseEnv(), args...)
		if res.exit != 2 {
			t.Errorf("pawl %v exit = %d, want 2\nstdout=%s", args, res.exit, res.stdout)
		}
		if !strings.Contains(res.stderr, "--write") {
			t.Errorf("pawl %v: stderr does not name the flag: %s", args, res.stderr)
		}
	}
	for _, name := range []string{agentsFile, claudeFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("a rejected --write still created %s", name)
		}
	}
}

// --write is scoped to `agent`; on any other command it is a usage error, so a script that mistypes the command does not have its intent silently ignored.
func TestWriteFlagIsScopedToAgent(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, onlyDimConfig())

	for _, command := range []string{"check", "record", "init", "rank", "version"} {
		res := runPawl(t, dir, baseEnv(), command, "--write", "claude")
		if res.exit != 2 {
			t.Errorf("%s --write exit = %d, want 2\nstdout=%s", command, res.exit, res.stdout)
		}
		if !strings.Contains(res.stderr, "--write is only valid on `agent`") {
			t.Errorf("%s --write error does not scope the flag: %s", command, res.stderr)
		}
	}
}

// The loop is identical in every repo, so it must not depend on a config — a broken or half-written pawl.yaml is exactly when someone reaches for it, whether they print it or install it.
func TestAgentNeedsNoConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", "dimensions: [ this is not valid yaml")

	for _, args := range [][]string{{"agent"}, {"agent", "--write", "claude"}} {
		if res := runPawl(t, dir, baseEnv(), args...); res.exit != 0 {
			t.Errorf("pawl %v exit = %d with a broken config, want 0\nstderr=%s", args, res.exit, res.stderr)
		}
	}
}

func TestAgentRejectsFormatFlag(t *testing.T) {
	dir := t.TempDir()
	for _, format := range []string{"json", "text-ish"} {
		res := runPawl(t, dir, baseEnv(), "agent", "--format", format)
		if res.exit != 2 {
			t.Errorf("agent --format %s exit = %d, want 2", format, res.exit)
		}
	}
}

// agent-md was renamed, not aliased: a script still calling it fails loud rather than appearing to work.
func TestAgentMDIsGone(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "agent-md")
	if res.exit != 2 {
		t.Fatalf("agent-md exit = %d, want 2 (unknown command)\nstdout=%s", res.exit, res.stdout)
	}
	if !strings.Contains(res.stderr, "agent") {
		t.Errorf("the unknown-command error does not name the replacement: %s", res.stderr)
	}
}

// init is where an adopter meets pawl; if it doesn't name the command, the command exists but nobody runs it.
func TestInitPointsAtAgent(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "pawl agent") {
		t.Errorf("init output never mentions `pawl agent`: %s", res.stdout)
	}
}

// A redirect is still a supported install, so the print path keeps warning about a copy that is already there — for either file, since an adopter may work in more than one tool.
func TestAgentPrintWarnsAboutAnAlreadyInstalledBlock(t *testing.T) {
	for _, name := range []string{agentsFile, claudeFile} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			first := runPawl(t, dir, baseEnv(), "agent")
			writeFile(t, dir, name, "# House rules\n\n"+first.stdout)

			res := runPawl(t, dir, baseEnv(), "agent")
			if res.exit != 0 {
				t.Fatalf("agent exit = %d, want 0 — the note is advice, not a verdict\nstderr=%s", res.exit, res.stderr)
			}
			if res.stdout != first.stdout {
				t.Errorf("stdout changed because of %s; the block must be the same bytes every time", name)
			}
			if !strings.Contains(res.stderr, "already contains a pawl block") {
				t.Errorf("stderr should warn about the existing block, got:\n%s", res.stderr)
			}
			if !strings.Contains(res.stderr, name) {
				t.Errorf("the note does not name %s: %s", name, res.stderr)
			}
		})
	}
}

// An unrelated instruction file must not produce the note, or it becomes noise nobody reads.
func TestAgentPrintIsQuietWhenNoBlockIsInstalled(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, agentsFile, "# House rules\n\nRun the linter.\n")

	res := runPawl(t, dir, baseEnv(), "agent")
	if res.exit != 0 {
		t.Fatalf("agent exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "already contains") {
		t.Errorf("warned about an instruction file that carries no pawl block:\n%s", res.stderr)
	}
	if got := readInstructionFile(t, dir, agentsFile); got != "# House rules\n\nRun the linter.\n" {
		t.Errorf("the print path modified %s: %q", agentsFile, got)
	}
}

// `pawl agent >> AGENTS.md` makes stdout and AGENTS.md the same file. Checking for an existing block after printing therefore finds the block this very run just wrote, and a first install warns about itself — so the check has to happen before the write.
func TestAgentDoesNotWarnOnAFirstRedirectInstall(t *testing.T) {
	dir := t.TempDir()

	res := runPawlShell(t, dir, "pawl agent >> AGENTS.md")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "already contains") {
		t.Fatalf("a first install warned about itself:\n%s", res.stderr)
	}
	if !strings.Contains(readInstructionFile(t, dir, agentsFile), agentBlockOpenMarker) {
		t.Fatal("AGENTS.md did not receive the block")
	}

	// The second one is the case the note exists for.
	again := runPawlShell(t, dir, "pawl agent >> AGENTS.md")
	if !strings.Contains(again.stderr, "already contains") {
		t.Fatalf("a second install did not warn:\n%s", again.stderr)
	}
}
