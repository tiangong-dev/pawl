# Changelog

Release notes and upgrade instructions. Review the breaking changes before updating an existing installation.

## Unreleased

### Added

- File-backed report dimensions can set `artifact_max_age` to fail closed when
  evidence is older than the configured duration.
- The GitHub Action accepts `guard-ref` to run `pawl guard` as part of the same
  standard check step.
- `--format codeclimate` now explains its removal and points consumers to the
  stable JSON verdict instead of emitting a generic format error.

## 0.8.0

### Breaking

- **`pawl agent-md` was renamed to `pawl agent`, with no alias.** Update any scripts that call the old command; it now exits 2 as an unknown command.
- **`pawl baseline-guard` was renamed to `pawl guard`, with no alias.** Update CI to call `pawl guard <ref>`.

### Changed

- **`pawl agent` can install its instruction block.** It asks for the target file, or accepts `--write agent` for `AGENTS.md` and `--write claude` for `CLAUDE.md`. This replaces the previous redirect-only workflow.
- Re-running the install replaces the block between `<!-- pawl:begin -->` and `<!-- pawl:end -->` without changing the surrounding file. Damaged or duplicate markers are rejected with exit 2.
- Without an interactive terminal, `pawl agent` prints the block and does not prompt or write a file. It warns on stderr if an instruction file already contains the block.
- Snapshots now include a versioned definition fingerprint for each metric. `check` refuses incompatible comparisons after measurement semantics change; `record`, `record --only`, `guard`, and `trend` handle the definition boundary explicitly. Legacy snapshots remain readable and are upgraded when recorded.
- Release CI now validates the local package with `npm pack --dry-run`. This avoids npm 11 registry version checks during pull-request validation.

### Fixed

- Character devices such as `/dev/null` and `/dev/zero` are no longer treated as terminals. Redirected commands now remain non-interactive.

## 0.7.1

### Changed

- Changed the Marketplace badge from green to black to match the wordmark. The binary is unchanged from 0.7.0.

## 0.7.0

### Breaking

- **Removed `pawl diff`.** Use `pawl check || true` for a non-blocking step; `--format json` includes the real `exit_code`.
- **Removed `--format codeclimate`.** Use `--format json`. GitLab Code Quality integration is no longer included; GitHub comments and annotations are unaffected.
- **Removed `pawl status` and `pawl constraints`.** Read `pawl.snapshot.json` and `pawl.yaml` directly, or use `pawl measure` for current values. Neither command appeared in a release.
- **Removed `agent-md --write`.** Use output redirection to choose the target file. This command did not appear in a release.

### Added

- **`pawl measure`** runs every dimension and prints a measurement document without reading a baseline or giving a verdict. Its output uses the snapshot format.
- **`check --current <path|->` and `record --current <path|->`** use a supplied measurement document instead of measuring again. A missing in-scope dimension is a measurement failure.
- **`valid_exit_codes` on command dimensions** declares which process exits produced a valid measurement. This is safer than `|| true`, which also hides crashes and invocation errors.
- **The `lines` named analyzer** turns line-oriented tool output into findings with a regular expression. Several rule- or level-filtered dimensions can share the same run.
- **`-q` / `--quiet`** on `measure`, `record`, and `check` suppresses progress and advisory output. Adapter stderr and JSON verdicts are not suppressed.
- **`check --only` reports excluded dimensions** in both text and JSON, so a scoped pass is distinguishable from a full pass.
- **`pawl agent-md`** prints the operating instructions for a coding agent. See 0.8.0 for its replacement.

### Changed

- When stdout is not a terminal, `check` prints a short `--format json` hint to stderr.
