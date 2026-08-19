Part of the pawl engine contract. See [spec/README.md](../README.md).

## rank

`pawl rank` never writes a snapshot. `--only` is a usage error (it is not a
gate), and so is an extra operand (exit 2).

### Output

For every `file-length` and `file-bytes` dimension, lists **all** matching files
(not just offenders), sorted by size descending. A file is `over` when its size
exceeds `threshold`, `near` when it is strictly above `0.9 * threshold` and not
over, otherwise `ok`. No such dimension in the config → exit 2. This is the
human full list; the agent-facing subset is `watch` on
`check --format json` (touched `near`/`over` files only) — see
[§ Machine-readable output](../engine/verdict.md#machine-readable-output).
`--format json`:

```json
{ "schema_version": 2, "command": "rank",
  "dimensions": [{ "id": "file-length", "kind": "lines", "threshold": 500,
                   "files": [{ "path": "a.go", "value": 612, "status": "over" }] }] }
```

