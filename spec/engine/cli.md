Part of the pawl engine contract. See [spec/README.md](../README.md).

## CLI

```
pawl [command] [-c <config>] [--format <text|json>] [--since <ref>] [--only <ids>] [--current <path|->] [--write <target>] [--dry-run] [--accept-worse] [-q|--quiet] [-h|--help]

  init                 scaffold a starter pawl.yaml (never overwrites)
  agent                install (or print) the operating loop a coding agent
                       needs to use this gate
  measure              measure every dimension and print the measurement
                       document; no baseline read, no verdict rendered
  record               measure every dimension and (over)write the snapshot
  check                measure + compare; exit 1 on any regression — the CI gate
  baseline-guard <ref> compare the working tree's snapshot against the version
                       committed at <ref> — the anti-tamper gate
  trend [<id>]         print each metric's value across the committed snapshot's
                       git history — a fully local trend, no cloud
  rank                 rank included files by line or byte size (including
                       under-threshold files)
  version              print `pawl <version>` and exit 0
  help [<command>]     print global or command help and exit 0
```

- Run with no command, pawl defaults to `check` (so a bare `pawl` in CI is the
  gate, not a usage error). "No command" means zero positional arguments — an
  empty-string argument is an unknown command (exit 2), so a wrapper passing an
  unset variable fails loud instead of silently running the default gate.
- `-c <path>` selects the config file; default `./pawl.yaml`.
- `-h` / `--help`, `pawl help`, and `pawl help <command>` print help without
  reading config. An unknown command/topic remains a usage error (exit 2).
- `--limit <n>` caps how many recent snapshots `trend` prints (default 20, `0`
  for all); on any command other than `trend` it is a usage error (exit 2).
- `--only <id>[,<id>…]` limits which dimensions this invocation measures.
  On `record` it re-records those dimensions and preserves the rest of the
  committed snapshot, specified in [§ Partial record](../commands/record.md#partial-record---only).
  On `check` it measures and compares only those dimensions (the inner loop: a
  broken or regressed unlisted adapter does not block). The
  orphan check still uses the full config. `--format json` reports the narrowed
  set as the top-level `only` array on every command, so a partial verdict never
  reads as a full one — see [§ Machine-readable output](verdict.md#machine-readable-output).
  On `measure` it scopes the emitted document to those dimensions. On any other
  command `--only` is a usage error (exit 2).
- `--dry-run` previews what `record` (with or without `--only`) would write —
  same table, nothing written — and `--accept-worse` explicitly authorizes
  writing a dimension worse than the committed baseline. Both are valid only on
  `record`; specified in [§ Accepted debt](../commands/record.md#accepted-debt---dry-run---accept-worse). On any other
  command either is a usage error (exit 2).
- `-q` / `--quiet` silences pawl's own progress lines, artifact notes, and the
  `--format json` hint, and buffers a `text` run's stdout so it is released only
  when the exit code is non-zero. Exit 0 says every dimension held; exit 1 and
  exit 2 carry a "which one, and by how much" the code alone cannot. An
  adapter's own stderr is never suppressed — a quiet run must lose the noise,
  not the diagnosis of the tool that is failing — and `--format json` always
  emits its verdict, since a caller parsing one must always receive one. Valid
  on `measure`, `record` and `check`; on any other command a usage error
  (exit 2).
- `--current <path|->` supplies `check` or `record` with a measurement document
  — the output of `pawl measure`, from a file or (with `-`) stdin — instead of
  running the dimensions, specified in
  [§ Measure](../commands/measure.md#measure). On any other command it is a
  usage error (exit 2).
- `--format <text|json>` selects the output format of `record`/`check`;
  default `text`. `json` is specified in
  [§ Machine-readable output](verdict.md#machine-readable-output).
  `baseline-guard` ignores `--format` (its output is not tabular). `trend` and
  `rank` honor `text` (default) and `json`. `agent` emits Markdown and
  `measure` emits the measurement document by definition, so any `--format` on
  either is a usage error (exit 2).
- `--write <target>` installs `agent`'s block into an instruction file:
  `agent` → `./AGENTS.md` (Codex, Antigravity, Cursor), `claude` →
  `./CLAUDE.md` (Claude Code), specified in
  [§ agent](../commands/agent.md#agent). An unknown target, and `--write` on
  any other command, are usage errors (exit 2).
- `--since <ref>` scopes `check` (only) to lines changed in the **working
  tree** since `<ref>`, specified in [§ Diff-scoped checking](../commands/since.md#diff-scoped-checking).
  `--since` on any command other than `check` is a usage error (exit 2).
- Unknown command → stderr message naming valid commands, exit 2.
- Extra positional operands are usage errors (exit 2): `trend` takes at most
  one operand (the metric id) and `baseline-guard` one (the ref); every other
  command takes none. This keeps a mistyped invocation (`pawl record only x` —
  the dashes of `--only` forgotten) from silently running a different,
  state-writing command.
- `pawl version` and `pawl --version` print exactly `pawl <version>\n` to
  stdout and exit 0 **without reading any config file** — they must work in a
  directory with no `pawl.yaml`. A `--version` riding on a **valid,
  validly-flagged** command (`pawl check --version`) also prints the version;
  any usage error in the invocation — an unknown command, a mis-scoped flag, a
  disallowed format (`agent --format json`) — outranks the version
  print and exits 2. Unknown-command outranks mis-scoped-flag in diagnostics. The version string defaults to `dev` and is
  overridden at build time via
  `-ldflags "-X github.com/tiangong-dev/pawl/internal/pawl.Version=<x.y.z>"`.
- `pawl init` writes a commented starter config to the config path (honoring
  `-c`) **without reading any existing config** — it is the zero-friction
  on-ramp, specified in [§ init](../commands/init.md#init). If a file already exists at that path
  it refuses (exit 2) rather than overwrite.

### Exit codes

| code | meaning |
|------|---------|
| 0 | pass (including legitimate baseline-guard skips) |
| 1 | `check`: at least one dimension regressed; `baseline-guard`: snapshot regressed vs `<ref>` and not covered by an accepted-debt trailer; `record`: refused a worse value without `--accept-worse` (including under `--dry-run`, which mirrors what a real record would do) |
| 2 | anything that prevents an honest verdict: unknown command, missing/invalid config, no dimensions, missing snapshot for `check`, malformed snapshot shape, orphaned metric, measurement failure, unresolvable git ref |

The 1-vs-2 split is load-bearing: 1 means "measured fine, code got worse";
2 means "could not measure/compare honestly" and must never read as a pass.

