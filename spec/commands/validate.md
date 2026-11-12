Part of the pawl engine contract. See [spec/README.md](../README.md).

## validate

`pawl validate` answers one question — is this config usable? — and nothing
else. It runs the same [config validation](../commands/config.md) every
measuring command runs, and **measures nothing**: no adapter command is
executed, the snapshot is neither read nor written, no git ref is resolved, and
no file is created. That makes it cheap enough for an editor integration or a
pre-commit hook to run on every save, and for CI to run before the tools a
dimension needs are installed.

The config path resolves exactly as it does everywhere else (`-c <path>`,
default `./pawl.yaml`); nothing here searches parent directories. The `snapshot`
path is not checked, so a config whose snapshot does not exist yet is valid.

This is also the authority the editor
[schema](../commands/config.md#editor-schema) only assists: whatever a schema
makes a tooltip say, a config is usable when `validate` says so.

### Unknown keys

Unlike every other command, a key the schema does not define is a **problem**
here rather than an advisory warning: `validate` is the one caller that must not
guess what the author meant. Each stray key is reported at its document path
(`dimensions[0].gat`) with the same "did you mean" suggestion
[§ Unknown keys](../commands/config.md#unknown-keys) prints, and the `x-`
extension prefix and YAML merge keys are exempt exactly as they are there. For
every command that measures, an unknown key stays a warning that does not change
the exit code.

### Exit codes

| code | meaning |
|------|---------|
| 0 | the config loads, and defines no unknown key |
| 2 | the config could not be loaded, or a key the schema does not define was found |

Exit 2 is the "could not measure / config problem" code, never 1: `validate`
reports no regression because it measures nothing. A `-c` path that does not
exist is exit 2, like any other unreadable config.

### Output (text)

Success is one line naming the config and what it declares:

```
ok: <path> (<n> dimension(s), <m> analyzer(s))
```

Failure prints one line per problem: the config error in the wording the
measuring commands use, followed by one line per unknown key.

```
dimension d: unknown builtin "nope" (available: file-length, …)
unknown key "dimensions[0].gat" — did you mean "gate"?
```

The report goes to stdout in both cases — it is the command's output, not a
diagnostic beside a verdict — and the exit code carries the machine signal.
Nothing is written to stderr.

### Output (`--format json`)

One JSON object on stdout, always carrying every field:

```json
{
  "schema_version": 1,
  "command": "validate",
  "ok": false,
  "config": "pawl.yaml",
  "dimensions": 0,
  "analyzers": 0,
  "errors": [{ "message": "dimension d: unknown builtin \"nope\" (available: …)" }],
  "unknown_keys": [{ "path": "dimensions[0].gat", "suggestion": "gate" }]
}
```

- `ok` is the verdict the exit code carries: no `errors` and no `unknown_keys`.
- `config` is the config path as it was given on the command line (default
  `pawl.yaml`), the same string the text line prints.
- `dimensions` and `analyzers` are the counts pawl accepted, and `0` when the
  config could not be loaded at all.
- `errors` holds the config errors, in the wording the measuring commands print.
  `validate` reports what loading the config raises first, so it names the same
  problem `pawl check` would refuse to run on.
- `unknown_keys` holds every key the schema does not define, each with the
  near-miss suggestion when a defined key is close by, `""` when none is.
- `errors` and `unknown_keys` are always present, `[]` when empty. One file can
  carry both, and both are reported: a stray key is worth naming even when the
  rest of the config already failed to load.

`--format json` never falls back to a bare error line: an unreadable config is
reported in this same document with `"ok": false`.

### Flags

- `-c <path>` selects the config file. `--format text|json` selects the report
  format; the default is `text`.
- `-q` / `--quiet` is accepted and changes nothing: `validate` emits no progress,
  and its report is its result.
- Every flag that describes measuring (`--only`, `--since`, `--current`,
  `--limit`, `--dry-run`, `--accept-worse`, `--write`) and an extra positional
  operand are usage errors (exit 2), so a wrapper cannot make `validate` look
  like it scoped or measured anything.
