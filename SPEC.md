# pawl — engine contract (frozen)

This file is still the authoritative contract **index**. The contract body now
lives under [`spec/`](spec/README.md), split so a change can be read without
swallowing a 66KB file. The Go implementation and its tests are written against
that tree.

Start at [spec/README.md](spec/README.md). Pick the file that matches the
change; do not read every file.

## Former `##` sections

| Section | File |
|---|---|
| Scope boundary (design decision) | [spec/engine/scope.md](spec/engine/scope.md) |
| CLI | [spec/engine/cli.md](spec/engine/cli.md) |
| init | [spec/commands/init.md](spec/commands/init.md) |
| Config — `pawl.yaml` | [spec/commands/config.md](spec/commands/config.md) |
| Exec adapter contract | [spec/adapters/exec.md](spec/adapters/exec.md) |
| Declarative extract layer | [spec/adapters/extract.md](spec/adapters/extract.md) |
| Built-in adapters | [spec/adapters/builtins.md](spec/adapters/builtins.md) |
| Report-format ingest builtins | [spec/adapters/ingest.md](spec/adapters/ingest.md) |
| Partial record (`--only`) | [spec/commands/record.md](spec/commands/record.md#partial-record---only) |
| Accepted debt (`--dry-run`, `--accept-worse`) | [spec/commands/record.md](spec/commands/record.md#accepted-debt---dry-run---accept-worse) |
| Snapshot — `pawl.snapshot.json` | [spec/engine/snapshot.md](spec/engine/snapshot.md) |
| Comparison semantics | [spec/engine/comparison.md](spec/engine/comparison.md) |
| baseline-guard | [spec/commands/guard.md](spec/commands/guard.md) |
| Output | [spec/engine/verdict.md](spec/engine/verdict.md#output) |
| Machine-readable output | [spec/engine/verdict.md](spec/engine/verdict.md#machine-readable-output) |
| Diff-scoped checking | [spec/commands/since.md](spec/commands/since.md#diff-scoped-checking) |
| Trend | [spec/commands/trend.md](spec/commands/trend.md) |
| Public Go API (package `pawl`) | [spec/api.md](spec/api.md) |

## README link map

Former fragment links on this path now resolve as:

- `SPEC.md#gate-modes` → [spec/engine/comparison.md#gate-modes](spec/engine/comparison.md#gate-modes)
- `SPEC.md#diff-scoped-checking` → [spec/commands/since.md#diff-scoped-checking](spec/commands/since.md#diff-scoped-checking)
- Scope boundary → [spec/engine/scope.md](spec/engine/scope.md)
- Built-in adapters → [spec/adapters/builtins.md](spec/adapters/builtins.md)
- Report-format ingest → [spec/adapters/ingest.md](spec/adapters/ingest.md)
- Declarative extract → [spec/adapters/extract.md](spec/adapters/extract.md)
- Machine-readable output → [spec/engine/verdict.md](spec/engine/verdict.md#machine-readable-output)
