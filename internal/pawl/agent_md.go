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
	fmt.Fprint(stdout, agentBlock)
	warnIfBlockAlreadyInstalled(stderr)
	return 0
}

// warnIfBlockAlreadyInstalled says so when ./AGENTS.md already carries a pawl
// block, because the common next move is a redirect that would append a second
// one. It is advisory on stderr: stdout stays exactly the block, so the
// redirect still works if that is what the user meant. Any problem reading the
// file is silence — this is a courtesy, not a measurement, and it must never
// turn printing a fixed string into a failure.
func warnIfBlockAlreadyInstalled(stderr io.Writer) {
	abs, err := filepath.Abs(agentMDFile)
	if err != nil {
		return
	}
	existing, err := os.ReadFile(abs)
	if err != nil || !strings.Contains(string(existing), agentBlockMarker) {
		return
	}
	fmt.Fprintf(stderr, "note: %s already contains a pawl block — appending this output would make a second, diverging copy.\n", displayPath(abs))
}
