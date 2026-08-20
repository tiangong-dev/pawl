Part of the pawl engine contract. See [spec/README.md](../README.md).

## agent

`pawl agent` emits the operating loop a coding agent needs to use this gate:
run `check` before calling a task done, branch on `failure_class` rather than
the exit code alone, scope a `record` with `--only`, read a scoped verdict as
scoped, never hand-edit the snapshot.

It exists because that knowledge was reachable only from pawl's own repository.
`pawl init` scaffolds a `pawl.yaml` and nothing else, so an adopter's agent met
the gate cold — and evaluation runs against `demo/fixture` show what that costs:
agents that never invoke the gate at all before declaring a task done. A CLI
cannot make an agent call it; the repository's own instruction file is where an
agent already looks before it starts.

**Which instruction file is not one answer.** Claude Code loads `CLAUDE.md`,
`.claude/CLAUDE.md` and `CLAUDE.local.md`, and does not read `AGENTS.md` at
all; Codex, Antigravity and Cursor read `AGENTS.md`. So an adopter who installs
the block into `AGENTS.md` and then works in Claude Code has installed nothing,
with no error to tell them — the failure is silent, which is why the command
asks rather than assumes.

- Reads **no config**. The block is identical in every repository, and the
  moment someone reaches for it is often the moment `pawl.yaml` is mid-edit or
  broken, so requiring a loadable config would withhold it exactly then. This
  holds whether the block is printed or installed.
- `--write <target>` installs it. `agent` writes `./AGENTS.md`, `claude` writes
  `./CLAUDE.md`; any other value is a usage error (exit 2), because a target
  that quietly resolved to a default would write a file nobody chose. `--write`
  on any other command is a usage error (exit 2).
- With **no `--write`**, the destination is chosen interactively — but only when
  stdin, stdout, and the stderr prompt are all attached to terminals. Otherwise the block goes to stdout
  unchanged, so `pawl agent >> AGENTS.md`, `pawl agent | pbcopy`, and an agent
  shelling out to read the loop all keep working instead of blocking on a prompt
  nobody can answer. The prompt itself goes to **stderr**, so choosing "print"
  leaves stdout carrying nothing but the block. EOF is not an answer: it fails
  (exit 2) rather than pick a target.
- The block is delimited by `<!-- pawl:begin -->` / `<!-- pawl:end -->`. An
  install **replaces what sits between those markers** and leaves the rest of
  the file — prose before it and after it — untouched. Appending unconditionally
  would leave two blocks disagreeing about how to use the gate after a pawl
  upgrade, which is worse for an agent than having none.
- A file whose markers are **damaged or duplicated** — an opening marker with no
  closing one, a closing one with no opening, the two in the wrong order, or
  more than one of either — is an error (exit 2) and the file is left exactly as
  found. pawl is editing a file the adopter owns; every guess it could make
  there silently destroys prose it did not write.
- When printing, `./AGENTS.md` and `./CLAUDE.md` are checked for an existing
  block and a `note:` line per hit goes to **stderr**, because the usual next
  move is a redirect that would append a second, diverging copy. stdout is
  unchanged and the exit code stays 0: it is advice, not a verdict. Any error
  reading either file is silent — printing a fixed string must not become a
  failure because of an unrelated file.
- **Those files are read before the block is printed, never after.** Under a
  redirect install, stdout *is* the instruction file: checking afterwards finds
  the block this very run just wrote, so a first install warns about itself. The
  ordering is the contract, not an implementation detail.
- `--format` is not valid here: the output is Markdown by definition. It is a
  usage error, exit 2.
- `pawl init`'s next-steps output names `agent`, since a command an adopter
  never hears about is a command nobody runs.
