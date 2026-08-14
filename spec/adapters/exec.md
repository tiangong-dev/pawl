Part of the pawl engine contract. See [spec/README.md](../README.md).

## Exec adapter contract

- The command runs via `sh -c <command>`, with cwd = the config file's directory.
- Environment: the parent environment plus `PAWL_ROOT=<absolute config dir>`.
- **stdout must be exactly one JSON object** (surrounding whitespace allowed):

  ```json
  { "value": 42, "unit": "things", "breakdown": { "pkg/a.go:17": 2 } }
  ```

  `value` is required and must be a finite number. `unit` defaults to `"count"`.
  `breakdown` is optional (`null` and omitted are equivalent).
- stderr is passed through to pawl's stderr (diagnostics for humans).
- **Exit code semantics — the core of the contract**: exit 0 + valid JSON = a
  measurement. Non-zero exit, timeout, or unparseable/invalid stdout = a
  *measurement failure*: pawl reports the dimension id and aborts the whole run
  with exit 2. "Could not measure" and "measured zero" are different things and
  must never be conflated.
- All dimensions are measured concurrently. A progress line `  measuring <id>…`
  is written to stderr as each measurement starts.

