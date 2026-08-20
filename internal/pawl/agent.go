package pawl

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// agentBlock is the operating loop pawl hands an adopter's coding agent. It lives as an embedded Markdown asset rather than a Go string literal for the same reason starter.yaml does: its own prose would otherwise be scanned by pawl's `**/*.go` dimensions.
//
// It exists because the knowledge an agent needs to use this gate — that exit 2 is an environment problem, that `record` without `--only` re-blesses everything, that a scoped verdict is not a green gate — was reachable only from pawl's own repo. An adopter ran `pawl init`, got a pawl.yaml, and their agent got nothing. Real evaluation runs (see demo/README.md) show what that costs: agents that never invoke the gate at all before declaring a task done, and verify with `wc -l` or a prose prediction instead.
//
//go:embed agent_block.md
var agentBlock string

// The block is delimited so a later run can replace it in place instead of appending a second, diverging copy.
const (
	agentBlockBeginMarker = "<!-- pawl:begin -->"
	agentBlockEndMarker   = "<!-- pawl:end -->"
)

// agentTarget names one instruction file and the tools that actually load it.
// Which file an agent reads is the tool's decision, not pawl's, and the two entries here are not interchangeable: Claude Code loads CLAUDE.md and does not read AGENTS.md at all, while Codex, Antigravity and Cursor read AGENTS.md.
// That asymmetry is the whole reason this command has a choice to offer — an adopter who installs the block into AGENTS.md and works in Claude Code has installed nothing.
type agentTarget struct {
	name  string
	file  string
	tools string
}

var agentTargets = []agentTarget{
	{name: "agent", file: "AGENTS.md", tools: "Codex, Antigravity, Cursor"},
	{name: "claude", file: "CLAUDE.md", tools: "Claude Code"},
}

func agentTargetNames() []string {
	names := make([]string, 0, len(agentTargets))
	for _, t := range agentTargets {
		names = append(names, t.name)
	}
	return names
}

func lookupAgentTarget(name string) (agentTarget, bool) {
	for _, t := range agentTargets {
		if t.name == name {
			return t, true
		}
	}
	return agentTarget{}, false
}

// runAgent installs the operating block, or prints it. See spec/commands/agent.md.
//
// An explicit --write is obeyed as given. Without one the choice is interactive, but only when there is a human on both ends of the pipe: a prompt written into a pipeline would hang a script, and `pawl agent >> AGENTS.md` has to keep working.
func runAgent(target string, stdin io.Reader, stdout, stderr io.Writer) int {
	if target == "" {
		if !isTerminalWriter(stdout) || !isTerminalReader(stdin) {
			return printAgentBlock(stdout, stderr)
		}
		chosen, ok := chooseAgentTarget(stdin, stderr)
		if !ok {
			return 2
		}
		if chosen == "" {
			return printAgentBlock(stdout, stderr)
		}
		target = chosen
	}
	t, ok := lookupAgentTarget(target)
	if !ok {
		fmt.Fprintf(stderr, "agent: unknown target %q — use one of: %s\n", target, strings.Join(agentTargetNames(), ", "))
		return 2
	}
	return writeAgentBlock(t, stdout, stderr)
}

// printAgentBlock writes the block to stdout unchanged.
//
// Every instruction file is read before the block is printed, never after. Under a redirect install stdout *is* the instruction file: checking afterwards finds the block this very run just wrote, so a first install would warn about itself. The ordering is the contract, not an implementation detail.
func printAgentBlock(stdout, stderr io.Writer) int {
	installed := installedAgentBlockPaths()
	fmt.Fprint(stdout, agentBlock)
	for _, path := range installed {
		fmt.Fprintf(stderr, "note: %s already contains a pawl block — appending this output would make a second, diverging copy. `pawl agent --write <target>` replaces it in place.\n", path)
	}
	return 0
}

// installedAgentBlockPaths lists the instruction files already carrying a block.
// Any problem reading one is a "no": this is a courtesy, not a measurement, and it must never turn printing a fixed string into a failure.
func installedAgentBlockPaths() []string {
	var paths []string
	for _, t := range agentTargets {
		abs, err := filepath.Abs(t.file)
		if err != nil {
			continue
		}
		existing, err := os.ReadFile(abs)
		if err != nil || !strings.Contains(string(existing), agentBlockBeginMarker) {
			continue
		}
		paths = append(paths, displayPath(abs))
	}
	return paths
}

// chooseAgentTarget prompts for a destination and returns the chosen target name, "" for print-only, and false if the choice could not be made.
// The prompt goes to stderr so that choosing "print" still leaves stdout carrying nothing but the block.
func chooseAgentTarget(stdin io.Reader, stderr io.Writer) (string, bool) {
	scanner := bufio.NewScanner(stdin)
	// Bounded, because a stream that looks like a terminal and is not one (a character device that is neither a tty nor /dev/null) would otherwise keep answering unusably forever.
	for attempt := 0; attempt < 5; attempt++ {
		fmt.Fprintln(stderr, "Where should the pawl block go?")
		for i, t := range agentTargets {
			fmt.Fprintf(stderr, "  %d) %s — %s\n", i+1, t.file, t.tools)
		}
		fmt.Fprintf(stderr, "  %d) print it here, write nothing\n", len(agentTargets)+1)
		fmt.Fprintf(stderr, "Choose [1-%d]: ", len(agentTargets)+1)
		if !scanner.Scan() {
			// EOF or a read error on a stream that looked like a terminal. Answering for the user would write a file they never chose.
			fmt.Fprintln(stderr, "\nagent: no choice read — pass `--write <target>` instead.")
			return "", false
		}
		switch answer := strings.TrimSpace(scanner.Text()); answer {
		case fmt.Sprint(len(agentTargets) + 1):
			return "", true
		case "":
			fmt.Fprintln(stderr, "")
		default:
			for i, t := range agentTargets {
				if answer == fmt.Sprint(i+1) || answer == t.name || answer == t.file {
					return t.name, true
				}
			}
			fmt.Fprintf(stderr, "\n%q is not one of the choices.\n", answer)
		}
	}
	fmt.Fprintln(stderr, "agent: no usable choice — pass `--write <target>` instead.")
	return "", false
}

// writeAgentBlock installs the block into one instruction file, replacing an existing copy in place.
// Appending unconditionally would leave two blocks disagreeing about how to use the gate after a pawl upgrade, which is worse for an agent than having none.
func writeAgentBlock(t agentTarget, stdout, stderr io.Writer) int {
	abs, err := filepath.Abs(t.file)
	if err != nil {
		fmt.Fprintf(stderr, "agent: resolving %s: %v\n", t.file, err)
		return 2
	}
	existing, err := os.ReadFile(abs)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "agent: reading %s: %v\n", displayPath(abs), err)
		return 2
	}
	next, verb, err := mergeAgentBlock(string(existing))
	if err != nil {
		fmt.Fprintf(stderr, "agent: %s %v\n", displayPath(abs), err)
		return 2
	}
	if err := os.WriteFile(abs, []byte(next), 0o644); err != nil {
		fmt.Fprintf(stderr, "agent: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	fmt.Fprintf(stdout, "✅ %s %s — read by %s.\n", verb, displayPath(abs), t.tools)
	return 0
}

// mergeAgentBlock returns the new contents of an instruction file and the verb describing what happened to it.
// A file whose markers are damaged or duplicated is an error rather than a guess: pawl is editing a file the adopter owns, and every wrong guess silently destroys prose it did not write.
func mergeAgentBlock(existing string) (string, string, error) {
	begins := strings.Count(existing, agentBlockBeginMarker)
	ends := strings.Count(existing, agentBlockEndMarker)
	switch {
	case begins > 1 || ends > 1:
		return "", "", fmt.Errorf("carries %d `%s` and %d `%s` markers — leave exactly one block, or none, and run this again", begins, agentBlockBeginMarker, ends, agentBlockEndMarker)
	case begins == 1 && ends == 0:
		return "", "", fmt.Errorf("has a `%s` with no `%s` — repair or remove that block first", agentBlockBeginMarker, agentBlockEndMarker)
	case begins == 0 && ends == 1:
		return "", "", fmt.Errorf("has a `%s` with no `%s` — repair or remove that block first", agentBlockEndMarker, agentBlockBeginMarker)
	}
	if begins == 1 {
		start := strings.Index(existing, agentBlockBeginMarker)
		end := strings.Index(existing, agentBlockEndMarker)
		if end < start {
			return "", "", fmt.Errorf("has `%s` before `%s` — repair or remove that block first", agentBlockEndMarker, agentBlockBeginMarker)
		}
		end += len(agentBlockEndMarker)
		// The block is embedded with a trailing newline; the tail after the old end marker already carries whatever separation the file had.
		return existing[:start] + strings.TrimSuffix(agentBlock, "\n") + existing[end:], "updated", nil
	}
	if strings.TrimSpace(existing) == "" {
		return agentBlock, "wrote", nil
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + agentBlock, "appended to", nil
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && isTerminalFile(f)
}

// isTerminalFile reports whether a stream is plausibly a human's terminal.
// `os.ModeCharDevice` alone says "character device", which /dev/null also is — and `> /dev/null` or `< /dev/null`, the usual way a CI step detaches a command, would then read as interactive and stop to ask a question nobody is there to answer.
// The redirect target is identified by inode rather than by name: an os.File's name is whatever it was opened as, so a redirected os.Stdout still calls itself /dev/stdout.
func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
		return false
	}
	return true
}
