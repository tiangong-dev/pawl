Part of the pawl engine contract. See [spec/README.md](../README.md).

## Built-in adapters

### `file-length`

Counts files whose line count exceeds `threshold` (default 500). Options: `threshold` (int), `include` (glob list, required, `**` supported), `exclude` (glob list, optional). Globs are matched against paths relative to the config dir. The `.git` directory is never traversed, and a directory matching an exclude glob (or its `/**`-less prefix, e.g. `**/node_modules/**` at the `node_modules` directory) is pruned without descending — excluding a huge tree costs zero traversal. Both built-ins share these traversal semantics.
- Line count: empty file = 0 lines; a trailing newline does not add a line (`"a\nb\n"` = 2 lines, `"a\nb"` = 2 lines).
- Result: `value` = number of files over the threshold, `unit` = `"files > <threshold> lines"`, `breakdown` = `{ "<relative path>": <line count> }` for each offending file.
- Intended gate: `total` — growing an already-long file must not fail CI; only a new file crossing the limit (which moves the total) should. Pair a second dimension on the same builtin with `gate: per-key-value` to also refuse growth of files already in the breakdown. See RECIPES.md.

### `file-bytes`

Counts files whose byte size exceeds `threshold` (default 32768). Same options and traversal semantics as `file-length`. A closer proxy for token cost than line count, still a language-agnostic filesystem scan — not a tokenizer.
- Result: `value` = number of files over the threshold, `unit` = `"files > <threshold> bytes"`, `breakdown` = `{ "<relative path>": <byte count> }` for each offending file.
- Intended gate: same pairing as `file-length` (`total` plus optional `per-key-value`).

### `pattern-count`

Counts regexp matches across files — the generic "suppression / escape-hatch counter" (`//nolint`, `@Suppress`, `!!`, `as!`, …). Options: `pattern` (Go regexp, required), `include` (glob list, required), `exclude` (glob list, optional).
- Matching is per line; every non-overlapping match counts.
- Result: `value` = total match count, `unit` = `"matches"`, `breakdown` = `{ "<relative path>:<1-based line>": <matches on that line> }`.
- The `path:line` breakdown key shape is what makes `per-file-count` gating work.

### `eslint`

Runs an ESLint invocation the project supplies and parses its `--format json` output — pawl owns the format knowledge, the project owns the tool setup. Options: `command` (string, required — must produce ESLint JSON on stdout, e.g. `npx eslint packages --format json --no-inline-config`), `rules` (list of rule ids, optional — count only messages whose `ruleId` is in the list; empty or omitted counts every message).
- The command runs under the same execution environment as an exec adapter (sh -c, config-dir cwd, `PAWL_ROOT`, stderr passthrough, timeout).
- **Exit-code semantics are ESLint's, not the raw exec contract's**: exit 0 (clean) and exit 1 (problems found) are both valid measurements; exit 2+ (fatal: config error, crash) is a measurement failure. This is the point of shipping the adapter — a raw exec command would need `|| true` and thereby lose real-failure detection.
- stdout must parse as the ESLint JSON array; anything else is a measurement failure.
- Result: `value` = total counted messages, `unit` = `"issues"`, `breakdown` = `{ "<path relative to config dir>:<line>": <count> }` (absolute `filePath`s from ESLint are relativized; a message with no line uses line 0).
- Intended gate: `per-file-count`.

### `oxlint`

Runs the Oxc project's Oxlint linter and parses its native `--format json` output. It is available both as a direct dimension builtin and as a named analyzer; use the named form when several dimensions should share one scan. Options: `command` (string, required), `rules` (native diagnostic code list, optional), `levels` (optional list containing `error`, `warning`, and/or `advice`), `min_files` (optional non-negative integer), and `verify` (direct builtin only: optional list of `--print-config` commands; named analyzers put the same list in the analyzer's top-level `verify` field).

- The command must emit Oxlint native JSON, for example `npx oxlint src --format json`. The report must contain a `diagnostics` array and a non-negative integer `number_of_files`; missing fields, malformed JSON, or unsupported diagnostic severities are measurement failures. A legitimate empty scan is `{"diagnostics":[],"number_of_files":n}`, never `{}`.
- Oxlint uses exit 0 when no error-level diagnostics are present and exit 1 when errors are found. Some fatal CLI/config failures also use exit 1, so Pawl accepts 0/1 only if stdout passes the native-report validation above. Every other exit is a measurement failure.
- `rules` entries match the diagnostic `code` exactly, such as `eslint(no-debugger)`, `typescript(no-explicit-any)`, or `unicorn(no-invalid-fetch-options)`. Parser diagnostics legitimately have no code: they count in an unfiltered dimension but do not match a rule selector.
- Each `verify` command must produce Oxlint `--print-config` JSON. Core config keys map from `no-debugger` to `eslint(no-debugger)`; plugin keys map from `unicorn/no-invalid-fetch-options` to `unicorn(no-invalid-fetch-options)`. Every selected rule must be enabled in at least one representative config or measurement fails before the scan.
- `value` is the number of diagnostics that match the selectors, `unit` is `"issues"`, and `breakdown` accumulates `{ "<filename relative to config dir>:<first labelled line>": <count> }`. Diagnostics without a filename remain in the scalar value but have no breakdown entry; a filename without a labelled line uses line 0.
- Intended gate: `per-file-count`.

### `jscpd`

Runs a jscpd invocation the project supplies and reads the JSON report it writes. Options: `command` (string, required — must run jscpd with `--reporters json`, e.g. `npx jscpd packages --min-tokens 50 --reporters json --output .pawl/jscpd --silent`), `report` (string, required — path to the produced `jscpd-report.json`, relative to the config dir).
- Same execution environment as an exec adapter; jscpd's exit 0 is required (pawl does not use jscpd's own `--threshold` gating).
- The report file must exist after the command and parse as jscpd JSON; `value` = `statistics.total.duplicatedLines`, `unit` = `"duplicated lines"`, `breakdown` = null. A missing or malformed report is a measurement failure — never a zero. pawl deletes any pre-existing report before running the command, so a stale report from an earlier run can never satisfy the measurement.
- Intended gate: `total` (clone boundaries legitimately move in refactors).

### `json-value`

Reads one number out of a tool's JSON — the generic reader behind coverage percentages, passing-test counts, `type-coverage`, and jscpd's duplicated-lines. It is commonly used for `higher-is-better` dimensions (coverage should not drop). Options: `path` (string, required — a dotted key path into the JSON, e.g. `total.lines.pct`), one JSON source, and `unit` (optional, default `"count"`). The source is one of:
- `command` alone — its stdout is the JSON (e.g. `type-coverage --json`).
- `file` alone — a JSON file (path relative to the config dir) that already exists (e.g. a `coverage-summary.json` a prior test step wrote).
- `command` + `file` — the command is run to *produce* the file, which is then read. As with `jscpd`, any pre-existing `file` is deleted before the command runs, so a stale artifact can never satisfy the measurement.

`command` must exit 0. A missing/malformed source, a `path` that does not resolve to a finite number (missing key, non-object midway, non-numeric leaf), or a missing `path`/source option is a measurement failure — never a silent zero. Result: `value` = the number, `unit` = the configured unit, `breakdown` = null. Config validation (exit 2 at load) requires `path` and at least one of `command`/`file`.

### `swift-complexity`

Runs a [swift-complexity](https://github.com/fummicc1/swift-complexity) (MIT) invocation the project supplies and parses its `--format json` output — the open-source way to gate Swift **cognitive** complexity, which SwiftLint does not measure. Options: `command` (string, required — must run `SwiftComplexityCLI … --format json`, e.g. `swift-complexity Sources --recursive --format json`), `threshold` (number, required — a function counts as an offender when its selected metric is **≥ threshold**), `metric` (optional — `"cognitive"` (default) or `"cyclomatic"`, selects which per-function value to gate).
- Same execution environment as an exec adapter (sh -c, config-dir cwd, `PAWL_ROOT`, stderr passthrough, timeout).
- **The command must not pass swift-complexity's own `--threshold`**: that tool returns exit 1 for *both* "functions exceeded threshold" and "bad path / crash", so pawl cannot tell a finding from a failure once thresholding is delegated. pawl therefore requires **exit 0** (like `jscpd`) and does the thresholding itself from the full function list, keeping the gate definition single-sourced in `pawl.yaml`.
- stdout must parse as swift-complexity JSON: `{ "files": [ { "filePath": "…", "functions": [ { "cognitiveComplexity": n, "cyclomaticComplexity": n, "location": { "line": n } } ] } ] }`. stdout that parses as JSON but lacks the top-level `files` key is the wrong shape and is a measurement failure (an empty run is `{"files":[]}`, not `{}`). A function missing the selected metric field is likewise a measurement failure, never a silent zero.
- Result: `value` = number of offender functions, `unit` = `"functions"`, `breakdown` = `{ "<path relative to config dir>:<line>": <count> }` (absolute `filePath`s are relativized; a function with no line uses line 0).
- Intended gate: `per-file-count` — a function crossing the threshold in one file fails even if the total is unchanged.
