# pawl engine contract

`pawl` is a language-agnostic anti-regression quality gate. Each **dimension**
measures one number (plus an optional per-file breakdown). `record` snapshots the
numbers; `check` re-measures and fails when any dimension regresses against the
snapshot. The measuring tool is an implementation detail of each dimension —
swapping tools means rewriting one adapter command while the baseline and the CI
gate stay put.

This tree is the authoritative behavioral contract. The Go implementation and
its tests are both written against it. Root [SPEC.md](../SPEC.md) is the short
index of the same contract (kept so existing `SPEC.md` links still resolve).

## How to read

Do not read every file. Open the slice that matches the change. Heading text is
unchanged from the former monolithic SPEC, so GitHub-style `#slug` fragments
still work on the file that now owns that heading.

## Files

### Engine

- [engine/scope.md](engine/scope.md) — Scope boundary + Integration acceptance criteria
- [engine/cli.md](engine/cli.md) — CLI usage, flag scope, arity, exit codes
- [engine/snapshot.md](engine/snapshot.md) — Snapshot — `pawl.snapshot.json`
- [engine/comparison.md](engine/comparison.md) — Comparison semantics (Worse/Better, [gate modes](engine/comparison.md#gate-modes))
- [engine/verdict.md](engine/verdict.md) — Output (text), machine-readable JSON, Code Quality

### Adapters

- [adapters/exec.md](adapters/exec.md) — Exec adapter contract
- [adapters/extract.md](adapters/extract.md) — Declarative extract layer
- [adapters/builtins.md](adapters/builtins.md) — Built-in adapters (`file-length`, `file-bytes`, `pattern-count`, `eslint`, …)
- [adapters/ingest.md](adapters/ingest.md) — Report-format ingest builtins (`sarif`, `junit`, `coverage`)

### Commands

- [commands/init.md](commands/init.md) — `init`
- [commands/agent.md](commands/agent.md) — `agent`
- [commands/measure.md](commands/measure.md) — `measure` + `--current`
- [commands/config.md](commands/config.md) — Config — `pawl.yaml`
- [commands/record.md](commands/record.md) — Partial record (`--only`) + Accepted debt (`--dry-run`, `--accept-worse`)
- [commands/since.md](commands/since.md) — [Diff-scoped checking](commands/since.md#diff-scoped-checking)
- [commands/guard.md](commands/guard.md) — `guard`
- [commands/trend.md](commands/trend.md) — Trend
- [commands/rank.md](commands/rank.md) — `rank`

### API

- [api.md](api.md) — Public Go API (package `pawl`)
