# Changelog

## Unreleased — 0.7.0

### Breaking

- **Removed `pawl diff`.** It was `check` with the exit code forced to zero.
  Use `pawl check || true`; `exit_code` is a field in `--format json`, so a step
  that must not fail CI needs no second command.
- **Removed `--format codeclimate`.** A third serialization of what
  `--format json` already carries, and a usage error in combination with
  `--only`. Use `--format json`. GitLab Code Quality integration goes with it;
  GitHub PR comments and annotations are unaffected.
- **Removed `pawl status` and `pawl constraints`.** They were second doors to
  reading `pawl.snapshot.json` and `pawl.yaml`; read the files, or use
  `pawl measure` for the current numbers. Neither command was ever in a
  release.
- **Removed `agent-md --write`.** `pawl agent-md >> AGENTS.md` does the same
  thing and lets you pick the file. When `./AGENTS.md` already carries a pawl
  block, a `note:` on stderr says so. `agent-md` was never in a release.

### Added

- **`pawl measure`** runs every dimension and prints the measurement document —
  no baseline read, no verdict. The document is the snapshot format byte for
  byte, so `pawl measure > pawl.snapshot.json` means what it looks like.
- **`check --current <path|->` and `record --current <path|->`** judge or record
  a measurement document instead of running the dimensions, from a file or
  stdin. One `measure` can now drive the check and the record that follows it,
  which previously were two separate passes over a tree that may have moved —
  and, when a dimension reads a report off disk, could read two different
  builds. A document missing a dimension in scope is a measurement failure
  naming it, never a quietly narrower run.
- **`valid_exit_codes` on any `command` dimension.** Previously a named SARIF
  analyzer's option only. It declares which exit codes count as a successful
  run, for tools that report findings through a non-zero exit. Prefer it to
  `|| true`, which accepts *every* exit code and turns a crashed tool into a
  clean zero.
- **`builtin: lines` named analyzer.** One regex with optional named groups
  (`path`, `line`, `rule`, `level`) turns any tool's line output into findings,
  so a tool pawl has no built-in support for still gets one scan feeding several
  rule- or level-scoped dimensions. It refuses `min_files` and `verify` rather
  than approximating them — line output reveals the files that had findings, not
  the files scanned, and carries no rule catalog.
- **`-q` / `--quiet`** on `measure`, `record` and `check`: silences progress and
  advisory output, and releases a text verdict only when the exit code is
  non-zero. An adapter's own stderr and the `--format json` verdict are never
  suppressed.
- **`check --only` reports what it skipped.** The JSON verdict carries an
  `excluded` array naming every configured dimension the scope left out, and the
  text output prints the same list — so a scoped exit 0 cannot be mistaken for a
  green full gate.
- **`pawl agent-md`** prints the operating loop a coding agent needs to use the
  gate correctly. Install it with `pawl agent-md >> AGENTS.md`.

### Changed

- `check`'s text output prints a one-line hint about `--format json` to stderr
  when stdout is not a terminal.
