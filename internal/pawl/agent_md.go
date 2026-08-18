package pawl

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// agentBlock is the operating loop pawl hands an adopter's coding agent. It
// lives as an embedded Markdown asset rather than a Go string literal for the
// same reason starter.yaml does: its own prose would otherwise be scanned by
// pawl's `**/*.go` dimensions.
//
// It exists because the knowledge an agent needs to use this gate — that exit
// 2 is an environment problem, that `record` without `--only` re-blesses
// everything, that a scoped verdict is not a green gate — was reachable only
// from pawl's own repo. An adopter ran `pawl init`, got a pawl.yaml, and their
// agent got nothing. Real evaluation runs (see demo/README.md) show what that
// costs: agents that never invoke the gate at all before declaring a task
// done, and verify with `wc -l` or a prose prediction instead.
//
//go:embed agent_md.md
var agentBlock string

// agentMDFile is where a coding agent already looks, so it is the file the
// warning below checks. pawl does not write it: `pawl agent-md >> AGENTS.md`
// is the shell's job, and a --write flag was only ever a worse spelling of it
// — one that also decided, on the user's behalf, which file to append to.
const agentMDFile = "AGENTS.md"

// agentBlockMarker opens the block, which makes an already-installed copy
// recognizable. That check is the one thing --write did that a redirect cannot,
// so it survives as a warning: two diverging copies of the same instructions
// are worse than one.
const agentBlockMarker = "<!-- pawl:begin -->"

// runAgentMD prints the agent operating block on stdout. See
// spec/commands/agent-md.md.
func runAgentMD(stdout, stderr io.Writer) int {
	// Read before writing. The documented way to install this is
	// `pawl agent-md >> AGENTS.md`, which means stdout *is* AGENTS.md — check
	// it afterwards and the block this very run just printed is what gets
	// found, so a first install warns about itself.
	installed, path := blockAlreadyInstalled()
	fmt.Fprint(stdout, agentBlock)
	if installed {
		fmt.Fprintf(stderr, "note: %s already contains a pawl block — appending this output would make a second, diverging copy.\n", path)
	}
	return 0
}

// blockAlreadyInstalled reports whether ./AGENTS.md carries a pawl block
// already, and the path to name if it does. Any problem reading the file is a
// "no" — this is a courtesy, not a measurement, and it must never turn printing
// a fixed string into a failure.
func blockAlreadyInstalled() (bool, string) {
	abs, err := filepath.Abs(agentMDFile)
	if err != nil {
		return false, ""
	}
	existing, err := os.ReadFile(abs)
	if err != nil || !strings.Contains(string(existing), agentBlockMarker) {
		return false, ""
	}
	return true, displayPath(abs)
}
