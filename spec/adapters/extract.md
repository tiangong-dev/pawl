Part of the pawl engine contract. See [spec/README.md](../README.md).

## Declarative extract layer

The raw exec contract demands the command emit `{value,unit,breakdown}` JSON —
so measuring anything trivial (a line count, a grep tally, one number in a JSON
report) forces a wrapper script whose only job is to reformat. The optional
`extract` field removes that wrapper: the command emits its tool's **raw
output**, and pawl derives the measurement from it declaratively.

- `extract` is valid **only** on a `command` dimension (never `builtin`). A
  `command` dimension with no `extract` keeps the raw JSON contract above,
  unchanged.
- The command runs under the same execution environment as an exec adapter
  (`sh -c`, config-dir cwd, `PAWL_ROOT`, stderr passthrough, timeout).
- **The command must exit 0.** A non-zero exit, a timeout, or output that
  cannot be extracted per the declared form is a *measurement failure* (exit 2,
  naming the dimension) — never a silent zero. (Note: tools like `grep` exit 1
  when they find nothing; wrap such a command so it exits 0, e.g.
  `grep -c foo || true`, or the empty result reads as a failure.)
- The extracted `unit` defaults to `"count"`. The object forms
  (`regex`/`json_path`) accept an optional `unit` string.

Four forms — the scalar forms are a bare YAML string, the object forms a map:

### `extract: number`

The command's trimmed stdout must be exactly one finite number → `value`.
No breakdown. Stdout that is empty, non-numeric, or more than one token is a
measurement failure.

```yaml
- id: todo-count
  command: "grep -rc TODO src | awk -F: '{s+=$2} END{print s+0}'"
  direction: "lower-is-better"
  extract: number
```

### `extract: lines`

`value` = the number of **non-empty** lines on stdout (a line is empty if it is
blank after trimming trailing `\r`/whitespace). No breakdown. Intended for
"count the matches this command printed".

```yaml
- id: nolint
  command: "grep -rn nolint src || true"
  direction: "lower-is-better"
  extract: lines
```

### `extract: { regex: "<Go regexp>", unit?: "<unit>" }`

The regexp is applied to each **non-empty** stdout line.
- **Every non-empty line must match**, or the run is a measurement failure —
  this is the honesty guard: a mistyped regexp that matches nothing would
  otherwise report `value = 0` and lie. Filter summary/noise lines out in the
  command so only findings reach pawl.
- `value` = the number of matching lines.
- If the regexp declares a named capture group `path`, a breakdown is built:
  key = `"<path>:<line>"` where `<line>` is the `line` named group if present
  (else `0`), and each matching line contributes `+1` to its key. With no
  `path` group, `breakdown` is null (scalar-only).
- v1 does **not** sum a numeric capture group into `value` (no second numeric
  semantics) and does **not** offer an "ignore unmatched lines" escape hatch.

```yaml
- id: legacy-lint
  command: "my-linter --format concise"
  direction: "lower-is-better"
  gate: "per-file-count"
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+:'
```

### `extract: { json_path: "<dotted path>", unit?: "<unit>" }`

The command's stdout is parsed as JSON; the dotted `json_path` (e.g.
`total.lines.pct`) is navigated to a finite number → `value`. No breakdown.
A malformed document, a missing key, a non-object midway, or a non-numeric leaf
is a measurement failure — never a silent zero. This covers the "read one
number from a tool's stdout JSON" case; it does **not** replace the `json-value`
builtin's `file`/stale-artifact-protection semantics.

```yaml
- id: coverage
  command: "go test -coverprofile=c.out ./... >/dev/null && gocov ..."
  direction: "higher-is-better"
  extract:
    json_path: "total.lines.pct"
    unit: "%"
```

Extract is a strictly additive convenience over the exec contract: the same
concurrency, `PAWL_ROOT`, timeout, and fail-loud rules apply, and the six
built-in adapters are **not** deprecated — they carry tool-specific exit-code
handling, stale-report protection, path relativization, and threshold semantics
that a raw regexp cannot.

