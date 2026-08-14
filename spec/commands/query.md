Part of the pawl engine contract. See [spec/README.md](../README.md).

## Query commands

These commands never write a snapshot. `--format codeclimate` is a usage error
(exit 2). `--only` is a usage error (they are not a gate). Extra operands are
usage errors (exit 2).

### `status`

Prints the committed snapshot **without measuring**. Missing or malformed
snapshot → exit 2. Text is a table of id / value / unit. `--format json`:

```json
{ "schema_version": 2, "command": "status", "snapshot": "pawl.snapshot.json",
  "metrics": [{ "id": "file-length", "title": "…", "direction": "lower-is-better",
                "value": 3, "unit": "files > 500 lines" }] }
```

Metrics are sorted by id. `title` is filled from the config when the id matches
a dimension, otherwise omitted.

### `constraints`

Prints the configured dimensions (thresholds, include globs, patterns) without
measuring. `--format json` is an object `{ "schema_version": 2, "command":
"constraints", "dimensions": [ { "id", "title", "direction", "gate", "builtin",
"command", "source", "options" } ] }` in config order. Text lists each id with
its direction, gate, source, and the `threshold` / `pattern` / `include` options
when present.

### `rank`

For every `file-length` and `file-bytes` dimension, lists **all** matching files
(not just offenders), sorted by size descending. A file is `over` when its size
exceeds `threshold`, `near` when it is strictly above `0.9 * threshold` and not
over, otherwise `ok`. No such dimension in the config → exit 2. This is the
human full list; the agent-facing subset is `watch` on
`check`/`diff --format json` (touched `near`/`over` files only) — see
[§ Machine-readable output](../engine/verdict.md#machine-readable-output).
`--format json`:

```json
{ "schema_version": 2, "command": "rank",
  "dimensions": [{ "id": "file-length", "kind": "lines", "threshold": 500,
                   "files": [{ "path": "a.go", "value": 612, "status": "over" }] }] }
```

