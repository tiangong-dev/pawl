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

// agentMDFile is fixed rather than a flag: the point is one command that lands
// the block where a coding agent already looks, and AGENTS.md is that place.
// Anything else is a redirect away (`pawl agent-md >> CLAUDE.md`).
const agentMDFile = "AGENTS.md"

// agentBlockMarker opens the block. --write uses it to recognize a copy it
// already installed, so re-running is a loud no-op instead of a second,
// diverging copy of the same instructions.
const agentBlockMarker = "<!-- pawl:begin -->"

// runAgentMD prints the agent operating block, or installs it into AGENTS.md
// with --write. See spec/commands/agent-md.md.
func runAgentMD(write bool, stdout, stderr io.Writer) int {
	if !write {
		fmt.Fprint(stdout, agentBlock)
		return 0
	}
	abs, err := filepath.Abs(agentMDFile)
	if err != nil {
		fmt.Fprintf(stderr, "agent-md: resolving %s: %v\n", agentMDFile, err)
		return 2
	}
	existing, err := os.ReadFile(abs)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "agent-md: reading %s: %v\n", displayPath(abs), err)
		return 2
	}
	if strings.Contains(string(existing), agentBlockMarker) {
		fmt.Fprintf(stderr, "agent-md: %s already contains a pawl block — edit it, or remove the block first.\n", displayPath(abs))
		return 2
	}
	// Append, never overwrite: AGENTS.md is the adopter's file and usually
	// already carries instructions that have nothing to do with pawl. A
	// scaffolder that clobbered them would be worse than useless — the same
	// reasoning that makes `pawl init` refuse an existing config.
	out := agentBlock
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n\n") {
		if strings.HasSuffix(string(existing), "\n") {
			out = "\n" + out
		} else {
			out = "\n\n" + out
		}
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "agent-md: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	if _, err := f.WriteString(out); err != nil {
		f.Close()
		fmt.Fprintf(stderr, "agent-md: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(stderr, "agent-md: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	verb := "appended pawl's agent loop to"
	if len(existing) == 0 {
		verb = "wrote"
	}
	fmt.Fprintf(stdout, "✅ %s %s\n", verb, displayPath(abs))
	return 0
}
