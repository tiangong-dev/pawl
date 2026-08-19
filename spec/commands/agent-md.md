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
- Writes **nothing**. It prints the block to stdout and exits 0, so installing
  it is a redirect — `pawl agent-md >> AGENTS.md` — and pawl neither decides
  which file the block belongs in nor edits a file the adopter owns.
- The block is delimited by `<!-- pawl:begin -->` / `<!-- pawl:end -->`. When
  `./AGENTS.md` already contains the opening marker, a `note:` line goes to
  **stderr** saying so, because the usual next move is a redirect that would
  append a second, diverging copy. stdout is unchanged and the exit code stays
  0: it is advice, not a verdict. Any error reading `AGENTS.md` is silent —
  printing a fixed string must not become a failure because of an unrelated
  file.
- **`AGENTS.md` is read before the block is printed, never after.** Under the
  documented install, stdout *is* `AGENTS.md`: checking afterwards finds the
  block this very run just wrote, so a first install warns about itself. The
  ordering is the contract, not an implementation detail.
- `--format` is not valid here: the output is Markdown by definition. It is a
  usage error, exit 2.
- `pawl init`'s next-steps output names `agent-md`, since a command an adopter
  never hears about is a command nobody runs.
