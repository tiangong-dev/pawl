Part of the pawl engine contract. See [spec/README.md](../README.md).

## agent-md

`pawl agent-md` emits the operating loop a coding agent needs to use this gate:
run `check` before calling a task done, branch on `failure_class` rather than
the exit code alone, scope a `record` with `--only`, read a scoped verdict as
scoped, never hand-edit the snapshot.

It exists because that knowledge was reachable only from pawl's own repository.
`pawl init` scaffolds a `pawl.yaml` and nothing else, so an adopter's agent met
the gate cold — and evaluation runs against `demo/fixture` show what that costs:
agents that never invoke the gate at all before declaring a task done. A CLI
cannot make an agent call it; the repository's own instruction file is where an
agent already looks before it starts.

- Reads **no config**. The block is identical in every repository, and the
  moment someone reaches for it is often the moment `pawl.yaml` is mid-edit or
  broken, so requiring a loadable config would withhold it exactly then.
- Default is a **pure print** to stdout, exit 0 — nothing is written, so it
  composes (`pawl agent-md >> CLAUDE.md`).
- `--write` **appends** the block to `./AGENTS.md`, creating the file if absent,
  exit 0. Pre-existing content is preserved: `AGENTS.md` is the adopter's file
  and usually already carries unrelated instructions.
- The block is delimited by `<!-- pawl:begin -->` / `<!-- pawl:end -->`. If
  `AGENTS.md` already contains the opening marker, `--write` changes nothing,
  prints a message naming the path, and exits 2 — two diverging copies of the
  same loop would leave an agent worse off than one. Any other filesystem error
  is likewise exit 2.
- `--write` is valid on no other command, and `--format` is not valid here: the
  output is Markdown by definition. Both are usage errors, exit 2.
- `pawl init`'s next-steps output names `agent-md`, since a command an adopter
  never hears about is a command nobody runs.
