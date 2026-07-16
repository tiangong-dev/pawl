# pawl — engine contract (frozen)

`pawl` is a language-agnostic anti-regression quality gate. Each **dimension**
measures one number (plus an optional per-file breakdown). `record` snapshots the
numbers; `check` re-measures and fails when any dimension regresses against the
snapshot. The measuring tool is an implementation detail of each dimension —
swapping tools means rewriting one adapter command while the baseline and the CI
gate stay put.

This document is the authoritative behavioral contract. The Go implementation and
its tests are both written against it.

## Scope boundary (design decision)

pawl is a **quality gate + honesty guard**, not a code analyzer. It never parses a
language. The line is drawn by the two-tier built-in design:

- **Primitives** (`file-length`, `pattern-count`) are Go-native only because they
  are both trivial *and* genuinely language-agnostic — counting lines and regexp
  matches needs no grammar.
- Anything requiring real language semantics (complexity, type escapes, dead
  code) is delegated to that language's own best analyzer through a **tool
  adapter** — pawl parses the analyzer's machine output, never the source.

This is deliberate. Complexity — cognitive complexity especially — is strongly
language-specific, and a home-grown metric would disagree with the ecosystem
tool developers already see in their IDE, so the gate would lose trust.
Reimplementing a multi-language AST metric engine is exactly what qlty already
is; if cross-language *uniform* metrics are ever wanted, the move is to adopt
qlty as one more adapter (pinned version, telemetry off), not to rebuild it. So
pawl stays a small, verifiable binary over a clean adapter contract.

## CLI

```
pawl [command] [-c <config>] [--format <text|json|codeclimate>] [--since <ref>] [--only <ids>]

  init                 scaffold a starter pawl.yaml (never overwrites)
  record               measure every dimension and (over)write the snapshot
  check                measure + compare; exit 1 on any regression — the CI gate
  diff                 measure + compare, print the table, always exit 0
  baseline-guard <ref> compare the working tree's snapshot against the version
                       committed at <ref> — the anti-tamper gate
  trend [<id>]         print each metric's value across the committed snapshot's
                       git history — a fully local trend, no cloud
  version              print `pawl <version>` and exit 0
```

- Run with no command, pawl defaults to `check` (so a bare `pawl` in CI is the
  gate, not a usage error).
- `-c <path>` selects the config file; default `./pawl.yaml`.
- `--limit <n>` caps how many recent snapshots `trend` prints (default 20, `0`
  for all); on any command other than `trend` it is a usage error (exit 2).
- `--only <id>[,<id>…]` re-records only the named dimensions and preserves the
  rest of the committed snapshot; valid only on `record`, specified in
  [§ Partial record](#partial-record---only). On any other command it is a
  usage error (exit 2).
- `--format <text|json|codeclimate>` selects the output format of
  `record`/`check`/`diff`; default `text`. `json` is specified in
  [§ Machine-readable output](#machine-readable-output); `codeclimate` in
  [§ Code Quality output](#code-quality-output).
  `baseline-guard` ignores `--format` (its output is not tabular). `trend`
  honors `text` (default) and `json`; `--format codeclimate` on `trend` is a
  usage error (exit 2).
- `--since <ref>` scopes `check` (only) to lines changed since `<ref>`, specified
  in [§ Diff-scoped checking](#diff-scoped-checking). `--since` on any command
  other than `check` is a usage error (exit 2).
- Unknown command → stderr message naming valid commands, exit 2.
- `pawl version` and `pawl --version` print exactly `pawl <version>\n` to
  stdout and exit 0 **without reading any config file** — they must work in a
  directory with no `pawl.yaml`. A `--version` riding on a **valid** command
  (`pawl check --version`) also prints the version; on an **unknown** command
  it is the unknown-command usage error (exit 2), never a version print. The
  unknown-command error also outranks a mis-scoped-flag error in diagnostics. The version string defaults to `dev` and is
  overridden at build time via
  `-ldflags "-X github.com/tiangong-dev/pawl/internal/pawl.Version=<x.y.z>"`.
- `pawl init` writes a commented starter config to the config path (honoring
  `-c`) **without reading any existing config** — it is the zero-friction
  on-ramp, specified in [§ init](#init). If a file already exists at that path
  it refuses (exit 2) rather than overwrite.

### Exit codes

| code | meaning |
|------|---------|
| 0 | pass (including `diff` with regressions, and legitimate baseline-guard skips) |
| 1 | `check`: at least one dimension regressed; `baseline-guard`: snapshot regressed vs `<ref>` |
| 2 | anything that prevents an honest verdict: unknown command, missing/invalid config, no dimensions, missing snapshot for `check`/`diff`, malformed snapshot shape, orphaned metric, measurement failure, unresolvable git ref |

The 1-vs-2 split is load-bearing: 1 means "measured fine, code got worse";
2 means "could not measure/compare honestly" and must never read as a pass.

## init

`pawl init` scaffolds a working starter `pawl.yaml` so a new project can go from
nothing to a passing gate in two commands (`pawl init && pawl record`). It reads
no existing config.

- Writes to the config path (`-c <path>`, default `./pawl.yaml`).
- **Never overwrites**: if a file already exists at that path, it prints a
  message naming the path and exits 2 (a scaffolder that clobbered a hand-tuned
  config would be worse than useless). A different pre-existing filesystem error
  on the stat is likewise exit 2.
- The written config is **valid and non-empty**: it declares at least one
  dimension using only zero-dependency primitive builtins (`file-length`,
  `pattern-count`), so `pawl record` succeeds immediately with no external tool
  installed. Comments in the file point at the recipe cookbook for more.
- On success it writes the file and prints a next-steps line (naming the file
  and pointing at `pawl record`), exit 0.

## Config — `pawl.yaml`

```yaml
# Optional. Snapshot path, resolved relative to the config file's directory.
snapshot: "pawl.snapshot.json"

dimensions:
  - id: "nolint-count"          # required, unique across dimensions
    title: "nolint suppressions" # required, human-readable
    direction: "lower-is-better" # required: lower-is-better | higher-is-better
    gate: "per-file-count"       # optional: total (default) | per-file-count | per-key-value
    tolerance: 0.0               # optional, absolute slack in the worse direction, default 0
    timeout: "10m"               # optional Go duration, default "10m"
    command: "./scripts/count-nolint.sh"  # exec adapter (exactly one of command | builtin)

  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    builtin: "file-length"       # built-in adapter (exactly one of command | builtin)
    options:                     # builtin-specific options
      threshold: 500
      include: ["**/*.go"]
      exclude: ["vendor/**"]
```

Validation errors (all exit 2): missing/duplicate `id`, missing `title`, missing or
invalid `direction`, invalid `gate`, both or neither of `command`/`builtin`, unknown
`builtin` name, invalid builtin options (bad regexp, missing `include`, …),
`extract` set on a `builtin` dimension (extract is an exec-adapter feature),
unknown `extract` form, an `extract` object with neither/both of `regex`/`json_path`,
an uncompilable `extract.regex`, an empty `extract.json_path`,
zero dimensions, unparseable YAML, config file not found.

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
- id: golangci
  command: "golangci-lint run ./... | grep -E '^[^:]+:[0-9]+:' || true"
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

## Built-in adapters

### `file-length`

Counts files whose line count exceeds `threshold` (default 500).
Options: `threshold` (int), `include` (glob list, required, `**` supported),
`exclude` (glob list, optional). Globs are matched against paths relative to the
config dir. The `.git` directory is never traversed, and a directory matching an
exclude glob (or its `/**`-less prefix, e.g. `**/node_modules/**` at the
`node_modules` directory) is pruned without descending — excluding a huge tree
costs zero traversal. Both built-ins share these traversal semantics.
- Line count: empty file = 0 lines; a trailing newline does not add a line
  (`"a\nb\n"` = 2 lines, `"a\nb"` = 2 lines).
- Result: `value` = number of files over the threshold, `unit` = `"files > <threshold> lines"`,
  `breakdown` = `{ "<relative path>": <line count> }` for each offending file.
- Intended gate: `total` — growing an already-long file must not fail CI; only a
  new file crossing the limit (which moves the total) should.

### `pattern-count`

Counts regexp matches across files — the generic "suppression / escape-hatch
counter" (`//nolint`, `@Suppress`, `!!`, `as!`, …).
Options: `pattern` (Go regexp, required), `include` (glob list, required),
`exclude` (glob list, optional).
- Matching is per line; every non-overlapping match counts.
- Result: `value` = total match count, `unit` = `"matches"`,
  `breakdown` = `{ "<relative path>:<1-based line>": <matches on that line> }`.
- The `path:line` breakdown key shape is what makes `per-file-count` gating work.

### `eslint`

Runs an ESLint invocation the project supplies and parses its `--format json`
output — pawl owns the format knowledge, the project owns the tool setup.
Options: `command` (string, required — must produce ESLint JSON on stdout,
e.g. `npx eslint packages --format json --no-inline-config`), `rules` (list of
rule ids, optional — count only messages whose `ruleId` is in the list; empty
or omitted counts every message).
- The command runs under the same execution environment as an exec adapter
  (sh -c, config-dir cwd, `PAWL_ROOT`, stderr passthrough, timeout).
- **Exit-code semantics are ESLint's, not the raw exec contract's**: exit 0
  (clean) and exit 1 (problems found) are both valid measurements; exit 2+
  (fatal: config error, crash) is a measurement failure. This is the point of
  shipping the adapter — a raw exec command would need `|| true` and thereby
  lose real-failure detection.
- stdout must parse as the ESLint JSON array; anything else is a measurement
  failure.
- Result: `value` = total counted messages, `unit` = `"issues"`, `breakdown` =
  `{ "<path relative to config dir>:<line>": <count> }` (absolute `filePath`s
  from ESLint are relativized; a message with no line uses line 0).
- Intended gate: `per-file-count`.

### `jscpd`

Runs a jscpd invocation the project supplies and reads the JSON report it
writes. Options: `command` (string, required — must run jscpd with
`--reporters json`, e.g. `npx jscpd packages --min-tokens 50 --reporters json
--output .pawl/jscpd --silent`), `report` (string, required — path to the
produced `jscpd-report.json`, relative to the config dir).
- Same execution environment as an exec adapter; jscpd's exit 0 is required
  (pawl does not use jscpd's own `--threshold` gating).
- The report file must exist after the command and parse as jscpd JSON;
  `value` = `statistics.total.duplicatedLines`, `unit` = `"duplicated lines"`,
  `breakdown` = null. A missing or malformed report is a measurement failure —
  never a zero. pawl deletes any pre-existing report before running the
  command, so a stale report from an earlier run can never satisfy the
  measurement.
- Intended gate: `total` (clone boundaries legitimately move in refactors).

### `json-value`

Reads one number out of a tool's JSON — the generic reader behind coverage
percentages, passing-test counts, `type-coverage`, and jscpd's duplicated-lines.
It is the natural home for `higher-is-better` dimensions (coverage should not
drop).
Options: `path` (string, required — a dotted key path into the JSON, e.g.
`total.lines.pct`), one JSON source, and `unit` (optional, default `"count"`).
The source is one of:
- `command` alone — its stdout is the JSON (e.g. `type-coverage --json`).
- `file` alone — a JSON file (path relative to the config dir) that already
  exists (e.g. a `coverage-summary.json` a prior test step wrote).
- `command` + `file` — the command is run to *produce* the file, which is then
  read. As with `jscpd`, any pre-existing `file` is deleted before the command
  runs, so a stale artifact can never satisfy the measurement.

`command` must exit 0. A missing/malformed source, a `path` that does not
resolve to a finite number (missing key, non-object midway, non-numeric leaf),
or a missing `path`/source option is a measurement failure — never a silent
zero. Result: `value` = the number, `unit` = the configured unit, `breakdown` =
null. Config validation (exit 2 at load) requires `path` and at least one of
`command`/`file`.

### `swift-complexity`

Runs a [swift-complexity](https://github.com/fummicc1/swift-complexity) (MIT)
invocation the project supplies and parses its `--format json` output — the
open-source way to gate Swift **cognitive** complexity, which SwiftLint does not
measure.
Options: `command` (string, required — must run `SwiftComplexityCLI … --format
json`, e.g. `swift-complexity Sources --recursive --format json`), `threshold`
(number, required — a function counts as an offender when its selected metric is
**≥ threshold**), `metric` (optional — `"cognitive"` (default) or
`"cyclomatic"`, selects which per-function value to gate).
- Same execution environment as an exec adapter (sh -c, config-dir cwd,
  `PAWL_ROOT`, stderr passthrough, timeout).
- **The command must not pass swift-complexity's own `--threshold`**: that tool
  returns exit 1 for *both* "functions exceeded threshold" and "bad path / crash",
  so pawl cannot tell a finding from a failure once thresholding is delegated.
  pawl therefore requires **exit 0** (like `jscpd`) and does the thresholding
  itself from the full function list, keeping the gate definition single-sourced
  in `pawl.yaml`.
- stdout must parse as swift-complexity JSON:
  `{ "files": [ { "filePath": "…", "functions": [ { "cognitiveComplexity": n,
  "cyclomaticComplexity": n, "location": { "line": n } } ] } ] }`. stdout that
  parses as JSON but lacks the top-level `files` key is the wrong shape and is a
  measurement failure (an empty run is `{"files":[]}`, not `{}`). A function
  missing the selected metric field is likewise a measurement failure, never a
  silent zero.
- Result: `value` = number of offender functions, `unit` = `"functions"`,
  `breakdown` = `{ "<path relative to config dir>:<line>": <count> }` (absolute
  `filePath`s are relativized; a function with no line uses line 0).
- Intended gate: `per-file-count` — a function crossing the threshold in one file
  fails even if the total is unchanged.

## Report-format ingest builtins

Three builtins read the ecosystem's **standard machine report formats** so any
tool that emits one becomes a pawl dimension with no wrapper — pawl sits *on top
of* the tools it already trusts (a scanner's SARIF, a runner's JUnit XML, a
coverage report) rather than reimplementing them.

**Shared exit-code rule (the honesty guard for all three).** Unlike the raw exec
contract (exit 0 or bust) and the `eslint` adapter (0/1 ok, 2+ fatal), these
formats are produced by tools that conventionally exit **non-zero to signal
findings/failures** — a scanner with findings, a test run with a failing test, a
coverage run whose tests failed. Gating on exit code would force `|| true` and
lose the report. So pawl **does not gate on the command's exit code**; the
honesty guard is instead that a **well-formed report of the declared format must
be produced**. A tool that crashes emits no parseable report (or, in `file`
mode, does not write the file — a pre-existing `file` is deleted before the
command runs, so a stale one can never satisfy the measurement), which is a
measurement failure (exit 2), never a silent zero. The one dishonesty this
cannot catch is a tool that emits a *well-formed but empty* report after failing
to actually analyze/run — pin the tool and its invocation so that can't happen.

**Shared JSON/report source** (as `json-value`): exactly one of
- `command` alone — its stdout is the report;
- `file` alone — a report file (path relative to the config dir) that already
  exists;
- `command` + `file` — the command produces the file (stale-artifact guard: any
  pre-existing `file` is deleted first).

### `sarif`

Counts results in a [SARIF](https://sarifweb.azurewebsites.net/) log — the
standard output of CodeQL, Semgrep, and a growing set of linters/scanners.
Options: the shared source, plus `rules` (string list, optional — count only
results whose `ruleId` is in the list; empty/omitted counts all) and `levels`
(string list, optional — count only results whose `level` is in the list; each
entry must be one of `error`/`warning`/`note`/`none`, else a config error;
empty/omitted counts all). A result with no `level` is treated as `warning`
(the SARIF default).
- stdout/file must parse as a SARIF log object with a `runs` array; a document
  that parses as JSON but lacks a `runs` array is the wrong shape and a
  measurement failure (an empty run is `{"runs":[]}`, not `{}`).
- `value` = number of `results` across all `runs` matching the filters, `unit` =
  `"findings"`.
- `breakdown` = `{ "<path>:<line>": <count> }` from each result's
  `locations[0].physicalLocation`: `artifactLocation.uri` (a `file://` scheme
  prefix stripped, then relativized to the config dir) and `region.startLine`
  (0 when absent). A result with **no** `physicalLocation`/`uri` still counts
  toward `value` but is omitted from the breakdown (nothing to attribute it to).
- Intended gate: `per-file-count`.

### `junit`

Reads a [JUnit XML](https://github.com/testmoapp/junitxml) report — the
near-universal test-result format (`<testsuites>`/`<testsuite>`/`<testcase>`) —
and counts one quantity. Options: the shared source, plus `count` (string,
optional, default `"failures"`), one of:
- `"failures"` — testcases with a `<failure>` or `<error>` child (`lower-is-better`);
- `"tests"` — all testcases;
- `"skipped"` — testcases with a `<skipped>` child;
- `"passing"` — `tests − failures − skipped` (`higher-is-better`).

Counts are derived from the **`<testcase>` elements themselves** (not the
suite-level `tests=`/`failures=` attributes, which producers compute
inconsistently and which would be a second, divergent source of truth). Each
state is detected independently, so `skipped` really is "testcases with a
`<skipped>` child". A document that does not parse as XML, is **not rooted at
`<testsuites>`/`<testsuite>`** (some other XML that merely contains a
`<testcase>` is not a JUnit report), has no `<testcase>` at all, or contains a
**contradictory** testcase (both failed/errored and skipped) is a measurement
failure. `value` = the selected count, `unit` = the `count` name
(`"tests"`/`"failures"`/`"skipped"`/`"passing"`), `breakdown` = null. Intended
gate: `total` (a passing-count floor, or a failure-count ceiling).

### `coverage`

Reads a code-coverage report and computes a coverage **percentage** — the two
machine formats `json-value` cannot read (lcov's text, cobertura's XML). (A
`coverage-summary.json` is already a `json-value` `total.lines.pct` read.)
Options: `file` (string, required — the report, relative to the config dir),
`format` (string, required — `"lcov"` or `"cobertura"`), `command` (optional —
produces the file, with the stale-artifact guard), and `metric` (optional,
default `"lines"` — `"lines"`, `"branches"`, or `"functions"`; `functions` is
lcov-only, so `functions` + `cobertura` is a config error).
- **lcov**: sum the `LF`/`LH` (lines), `FNF`/`FNH` (functions), `BRF`/`BRH`
  (branches) records across the file; `value` = `hit / found × 100`. Counters
  must be **non-negative finite** numbers and `hit ≤ found`; a negative, `NaN`,
  `Inf`, or hit-exceeds-found counter is a measurement failure (else e.g.
  `LF:-1 LH:-1` would read as 100%).
- **cobertura**: the root `<coverage>` element's `line-rate` / `branch-rate`
  attribute × 100. The root must be `<coverage>` and the rate must be a fraction
  in `[0,1]`; a non-`<coverage>` root, or a rate that is `NaN`/`Inf`/`<0`/`>1`,
  is a measurement failure.
- A report with **zero** of the requested unit found (lcov `found` total 0, or a
  missing cobertura rate attribute) is a measurement failure (`no <metric>
  coverage data`) — never a silent 0 or 100.
- `value` = the percentage, `unit` = `"%"`, `breakdown` = null. Direction is
  `higher-is-better`; intended gate: `total` (a small `tolerance` absorbs
  rounding noise).

## Partial record (`--only`)

`pawl record --only <id>[,<id>…]` re-measures **only** the named dimensions and
writes a snapshot that keeps every other metric's committed value untouched. It
is the surgical counterpart to a full `record`: a full record re-measures and
re-blesses *every* dimension at once, so locking in a win on one dimension also
silently accepts whatever the others currently measure — including a regression
elsewhere you did not mean to bless. `--only` locks in the improved dimension
alone, so the committed baseline for the rest stays exactly where it was.

- Valid only on `record`; on any other command it is a usage error (exit 2).
  An empty list (`--only ""` / `--only ,`) is a usage error (exit 2).
- Every listed id must be a configured dimension id; an unknown id → exit 2
  (naming the id), before anything is measured or written.
- Requires an existing, **well-formed** snapshot to preserve: a missing snapshot,
  or one with shape errors, → exit 2 (naming the problem). "Preserve the rest"
  is meaningless without a baseline — run a full `pawl record` first.
- **Only the listed dimensions are measured.** An unrelated dimension whose
  adapter is currently broken therefore does not block locking in the win (that
  is the point). The written snapshot = the freshly measured listed dimensions,
  plus, for every **other configured** dimension, its metric copied verbatim
  from the existing snapshot.
- A metric in the existing snapshot whose dimension is no longer configured (an
  orphan) is dropped, exactly as a full `record` drops it — `--only` never writes
  an orphan back.
- A configured dimension that is neither listed nor present in the existing
  snapshot stays absent (it remains "new" until a full record, or an `--only`
  that names it).
- Output honors `--format` as a full `record` does (text table, `json`,
  `codeclimate`). The text footer names the re-recorded ids and the number of
  preserved metrics instead of the plain `📸 snapshot written` line. The output
  covers only the metrics actually written (measured or preserved); an
  intentionally-absent dimension is omitted, never rendered as a measured `0`.

## Snapshot — `pawl.snapshot.json`

```json
{
  "metrics": {
    "file-length": {
      "direction": "lower-is-better",
      "value": 3,
      "unit": "files > 500 lines",
      "breakdown": { "pkg/big.go": 612 },
      "tolerance": 1
    }
  }
}
```

- Field order per metric: `direction`, `value`, `unit`, `breakdown`, `tolerance`.
  `breakdown` is `null` when the measurement produced none. `tolerance` is present
  only when the dimension declares it (so `baseline-guard`, which never sees the
  config, grants the same slack the gate does).
- Metric ids are serialized in sorted order; 2-space indent; trailing newline.
- Numbers print in minimal decimal notation, never exponent form
  (`3613`, `72.41` — as by Go's `strconv.FormatFloat(v, 'f', -1, 64)`).

### Shape validation

`check`, `diff`, and `baseline-guard` refuse (exit 2) to compare against a
malformed snapshot. Shape errors, checked in this order per snapshot:

1. not a JSON object → `snapshot is not an object`
2. `metrics` missing or not an object → `snapshot.metrics is missing or not an object`
3. `metrics` empty → `snapshot.metrics is empty`
4. per metric: not an object → `metric "<id>" is not an object`;
   `value` missing or not a finite number → `metric "<id>" has no numeric value`

JSON.parse succeeding only proves valid JSON, not that the gate can trust the
shape — a truncated or hand-corrupted snapshot must not read as "consistent".

## Comparison semantics

### worse / better

`tolerance` is absolute slack in the worse direction. A value exactly AT the
tolerance boundary passes.

- `higher-is-better`: worse ⇔ `cur < base - tolerance`; better ⇔ `cur > base` (strict, no tolerance)
- `lower-is-better`: worse ⇔ `cur > base + tolerance`; better ⇔ `cur < base`

### Gate modes

The scalar total is ALWAYS checked (with tolerance). The per-file / per-key check
on top stops a localized regression from hiding behind a net-zero total (file A
improves, file B worsens, total unchanged).

- **`total`** — scalar only.
- **`per-file-count`** — offender count per file may not rise. Offender count =
  number of breakdown KEYS grouped by the key's file part (the substring before
  the first `:`; a key with no `:` is itself the file). Counting keys, not summing
  values, keeps the gate robust to code moving around inside a file. A file
  present only in the current breakdown regresses from 0. Tolerance does not
  apply to per-file counts (only to the scalar).
- **`per-key-value`** — every key of the BASELINE breakdown must not worsen
  (with tolerance, same `worse` predicate). Keys missing from the current
  breakdown are ignored (removal is legitimate); keys new in current are ignored
  (they had no baseline).

**Known limits (choose the gate accordingly).** `per-file-count` is the strong
net-zero defense for *issue-count* dimensions: swapping a fix in file A for a new
offender in file B fails because B's per-file count rose. It is deliberately
lenient *within* one file — if a file drops one offender and gains another, its
count is unchanged and the gate passes (offenders moving inside a file is
expected churn, not regression). `per-key-value` only guards keys that exist in
the baseline: a brand-new key elsewhere that a deletion nets to zero on the total
is not caught by the per-key check (only the scalar total would, and only if it
rose). And because keys are `"path:line"`, a pure line-number shift changes a
key's identity — so `per-key-value` is a fit for **stable-key numeric**
dimensions (a fixed set of keys whose *values* move), while `per-file-count` is
the fit for **issue-count** dimensions (offenders coming and going). Neither gate
is a total net-zero proof; they are targeted defenses for their intended shape.

Regression detail lines (exact formats, `<n>` in minimal decimal notation):

- scalar: `total <base> → <cur>`
- per-file-count: `<file>  <baseCount> → <curCount>` (two spaces)
- per-key-value: `<key>  <baseValue> → <curValue>` (two spaces)

A dimension present in config but absent from the snapshot is `new` — it cannot
regress. (It enters the gate at the next `record`.)

### Orphaned metrics

A snapshot metric whose id matches no configured dimension is an **orphan**:
`check`/`diff` refuse to run (exit 2, message lists the sorted orphan ids).
Deleting a dimension must also drop it from the snapshot (re-`record`), so a
regression can't hide behind a vanished measurement.

## baseline-guard

`pawl baseline-guard <ref>` compares the working tree's snapshot file against the
version committed at `<ref>` (in CI: the PR's base branch). This is what stops a
hand-edited snapshot from faking a pass — `check` alone only verifies consistency
between the snapshot on disk and a fresh measurement, not that the file's history
is honest.

Two-stage git lookup (the stages must not be conflated):

1. `git rev-parse --verify <ref>` — fails ⇒ **error** (exit 2, loud). A typo'd
   ref or shallow clone must not silently disable the anti-tamper gate.
2. `git show <ref>:<repo-relative snapshot path>` — fails ⇒ **absent**: the ref
   is fine but predates the snapshot. Print a skip message, exit 0.
   The repo-relative path is computed from `git rev-parse --show-toplevel`.

Then:

- Snapshot content at `<ref>` not valid JSON → exit 2.
- No snapshot in the working tree → exit 2 (`run \`pawl record\` first`).
- Shape errors on either side (prefixed `<ref>:` / `working tree:`) → exit 2.
- A metric present at `<ref>` but missing from the working tree snapshot →
  warning (`::warning::…` under `GITHUB_ACTIONS`, `⚠️  …` otherwise), not a
  failure — deleting a dimension is legitimate; the orphan check covers honesty.
- A metric that worsened (per its recorded `direction`, default
  `lower-is-better` if missing; slack = its recorded `tolerance`) → violation
  line `<id>: <base> → <cur>`; any violation ⇒ exit 1.
- Otherwise: consistency message, exit 0.

A metric present only in the working tree (newly added dimension) is ignored.

## Output

`record`/`check`/`diff` print a table to stdout:

```
metric        baseline    current       Δ  status
---------------------------------------------------
file-length          3          4      +1  ❌ worse
```

- Δ: `new` when no baseline, `±0`, else signed delta rounded to 2 decimals.
- status: `🆕 new` (no baseline) / `❌ worse` (regressed, including per-file/per-key
  regressions that leave the scalar unchanged — the gate's verdict overrides the
  scalar-only status) / `✅ within tolerance` (worse by scalar but inside declared
  slack) / `🎉 better` / `✅ same`.
- After the table, `check`/`diff` print a `❌ regressions:` block (dimension id,
  title, detail lines) when any, and a `🎉 improved: <ids>` block plus a hint to
  run `pawl record` when any dimension's scalar strictly improved.
- `check` under `GITHUB_ACTIONS=…` (env var set non-empty) additionally prints
  `::notice::pawl improved: <ids> — run \`pawl record\` to lock in the gains.`
  so an unrecorded win surfaces on the PR itself.
- `check` under `GITHUB_ACTIONS` also prints a GitHub `::error::` annotation per
  regression, so violations land inline on the PR diff. A `per-file-count`
  dimension emits one `::error file=<path>,line=<line>,title=pawl: <id>::…` per
  new offender key in a file whose offender count rose; `per-key-value` one per
  worsened key; a `total`-gate (or detail-less) regression a single file-less
  `::error title=pawl: <id>::<title> regressed: <base> → <cur>`. Annotations are
  additive — the human-readable `❌ regressions:` block always prints too.
- `record` prints the table, writes the snapshot, prints `📸 snapshot written to <path>`.

## Machine-readable output

`--format json` makes `record`/`check`/`diff` print **exactly one JSON object**
to stdout and nothing else (no table, no `❌ regressions:` block, no emoji, and
— because stdout must stay pure JSON — no `::error::`/`::notice::` GitHub
annotations). stderr (the `measuring <id>…` progress lines) is unchanged. The
exit code is identical to text mode. This is pawl's own stable verdict schema —
deliberately not rdjson, which cannot express a scalar total, an improvement, or
a `--since` suppression.

```json
{
  "schema_version": 1,
  "command": "check",
  "mode": "full",
  "since": null,
  "exit_code": 1,
  "metrics": [
    {
      "id": "eslint",
      "title": "ESLint issues",
      "direction": "lower-is-better",
      "gate": "per-file-count",
      "unit": "issues",
      "base": 10,
      "current": 12,
      "status": "worse",
      "improved": false,
      "regressions": [
        {
          "kind": "per-file-count",
          "key": "src/a.ts:5",
          "path": "src/a.ts",
          "line": 5,
          "base": 0,
          "current": 1,
          "message": "src/a.ts  0 → 1",
          "suppressed": false
        }
      ]
    }
  ]
}
```

- Top level: `schema_version` (int, currently `1`), `command`
  (`record`/`check`/`diff`), `mode` (`full` or `since`), `since` (the ref string
  when `mode` is `since`, else `null`), `exit_code` (the process exit code), and
  `metrics` — an array **sorted by `id`**.
- Each metric: `id`, `title`, `direction`, `gate` (`total` when unset), `unit`,
  `base` (the baseline value, `null` when the dimension is new), `current` (the
  measured value), `status` (`new`/`worse`/`within-tolerance`/`better`/`same`,
  the emoji-free form of the table status), `improved` (bool — scalar strictly
  improved), and `regressions` (array, empty when none).
- Each regression: `kind` (`total`/`per-file-count`/`per-key-value`), `key`
  (the breakdown key, `null` for a `total` regression), `path` and `line`
  (parsed from `key`; both `null` for `total`, `line` `null` when the key has no
  numeric line), `base` and `current` (the two compared numbers — for `total`
  the scalar values, for `per-file-count` the offender counts, for
  `per-key-value` the key's values), `message` (the exact text-mode detail line),
  and `suppressed` (bool — `true` only in `--since` mode when this regression was
  exempted for falling outside the changed lines; always `false` in `full` mode).
- Regressions within a metric are ordered as in text mode; `suppressed` ones are
  still listed (so the JSON is a faithful record) but do not affect `exit_code`.

## Code Quality output

`--format codeclimate` makes `record`/`check`/`diff` print a **Code Climate
issue array** (the format GitLab renders as its Merge Request *Code Quality*
widget and inline diff annotations) to stdout and nothing else — no table, no
emoji, no GitHub annotations. stderr (the `measuring <id>…` progress lines) and
the exit code are unchanged from text mode, so `pawl check --format codeclimate`
still exits 1 on a regression while writing the artifact.

This is **findings mode**, not the baseline delta: it lists *every current
offender* the gate can locate to a file and line, and leaves the new-vs-fixed
comparison to GitLab (which diffs the report on the MR branch against the report
on the target branch). The output is therefore independent of the snapshot — the
same command on any branch reports that branch's current offenders.

Only **per-file-count** dimensions produce findings: their breakdown is keyed by
`path:line`, so each offender has a location. `total` and `per-key-value`
dimensions carry no per-line location (a total has no attributable line; a
per-key-value key is an arbitrary label, not a source position), so they emit no
findings — their gate is still enforced through the exit code. A `check` whose
config has no per-file-count offenders prints `[]` (a valid empty report).

```json
[
  {
    "description": "TODO / FIXME markers",
    "check_name": "todo-markers",
    "fingerprint": "8f14e45fceea167a5a36dedd4bea2543",
    "severity": "major",
    "location": {
      "path": "src/a.ts",
      "lines": { "begin": 5 }
    }
  }
]
```

- One entry per per-file-count breakdown key. `check_name` is the dimension `id`;
  `description` is the dimension `title` (with ` ×<n>` appended when the offender
  count at that location exceeds 1). `severity` is always `major` (pawl has no
  per-issue severity). `location.path` and `location.lines.begin` come from the
  breakdown key `path:line`, split on the **last** colon (so a path that itself
  contains a colon keeps its line). A key with no colon, a non-numeric line, or a
  line ≤ 0 (the adapter's "unknown line") is skipped — Code Quality entries need
  a real line.
- `fingerprint` is a stable hex digest of `check_name`, `path`, and `line` — not
  the `description`, which carries the run-varying `×n` count. Identical
  locations yield an identical fingerprint across runs, so GitLab tracks the same
  issue across commits and never treats a re-measured offender as new.
- Entries are sorted by `path`, then `line`, then `check_name` — a deterministic
  array for reproducible artifacts and diffs.

## Diff-scoped checking

`pawl check --since <ref>` runs the normal gate — measure every dimension,
compare against the snapshot — then **scopes the verdict to lines changed since
`<ref>`**, so pre-existing debt does not block a PR while new regressions still
fail. It is the gate narrowed to new code, **not** a standalone new-code
scanner: it still requires the snapshot (missing snapshot → exit 2, like
`check`). `--since` is valid only on `check`.

**Changed-line set.** `git merge-base <ref> HEAD` (in `cfg.Dir`), then
`git diff --unified=0 --no-ext-diff <merge-base>..HEAD`; the added (`+`) lines
give `map[repo-relative path]set<new line number>`. Breakdown keys are
config-dir-relative, git paths are repo-toplevel-relative, so keys are converted
to repo-relative via `cfg.Dir`'s position under `git rev-parse --show-toplevel`
before intersecting. Failure to resolve the ref, compute a merge-base (e.g. a
shallow clone with no common ancestor), or run the diff is a measurement-style
failure → exit 2 (never a silent "nothing changed").

**Scoping rule, per dimension.** The 1-vs-2 exit split is preserved (measurement
failures are still exit 2).

Only `per-file-count` is diff-scoped; the others are enforced in full. This
keeps `--since` **exactly the full-mode verdict, narrowed to added lines** —
never inventing a regression full mode wouldn't raise, never dropping one it
would.

- **`total` and `per-key-value` dimensions** — enforced **at full strength** (the
  normal verdict, unscoped). A scalar total has no line to attribute; a
  `per-key-value` scalar is not a sum of its breakdown and its gate ignores new
  keys, so scoping it would diverge from full mode. Both are *not* silently
  exempted — the output lists them as "enforced in full". A `per-file-count`
  dimension with **no breakdown** is treated the same way.
- **`per-file-count` dimension (with a breakdown)** — its scalar is the count /
  sum of a `path:line` breakdown, so every contributor to a regression has a
  line and can be scoped. The verdict is **re-derived from the breakdown against
  the added lines**: a key is a *worse* offender when it is new, or when its
  value grew on an already-present key (a line edited to carry more offenders) —
  direction-agnostic, exactly what moves the full-mode count/scalar. A worse key
  is a **live** regression if its `"path:line"` lies on an added line, and
  `suppressed` (exempted) if on an unchanged line.
  - A worse key that is **not line-addressable** (`line` 0, no line, or a
    file-only key) cannot be proven pre-existing, so it is counted **live**
    (conservative — when in doubt, gate) and noted as an unscopeable offender.
  - The scalar total is **not** re-counted here (the per-key pass accounts for
    every contributor), but if it regressed it is still **listed** as a
    `suppressed` regression so the JSON stays a faithful record that the total
    moved.

**Line-based approximation.** Scoping is by line number, not content hash, so it
inherits `git diff`'s notion of a changed line (as do reviewdog, diff-cover, and
Sonar's clean-as-you-code). Two consequences: a pre-existing offender whose line
content is unchanged but shifts position is tracked by git as context and is
**not** flagged (ordinary code motion doesn't trip the gate); but an offender on
a line whose content genuinely changed is flagged even if it "morally" moved
there — pawl cannot tell "moved" from "new" without hashing. This errs on the
safe side (it never under-reports a changed line) and is why `--since` is
line-precise rather than the full mode's per-file key count. It also assumes the
adapter's breakdown fully accounts for its scalar; a deliberately partial
breakdown could hide a scalar rise the per-key pass never sees.

**Output.** Text mode prints a banner naming the mode, the `<ref>`, the resolved
merge-base short SHA, the dimensions scoped vs. enforced-in-full, and the count
of pre-existing regressions exempted. `--format json` sets `mode: "since"`,
`since: "<ref>"`, and carries `suppressed: true` on each exempted regression.
The exit code is 1 iff any live (non-suppressed) regression remains — including
a full-strength `total` regression — else 0.

## Trend

`pawl trend [<id>]` reconstructs each metric's value over time from the
**committed snapshot file's own git history** — no cloud, no external store, no
account. It is **read-only and never measures**: it walks `git log` for the
snapshot path, parses the snapshot committed at each commit that touched it, and
prints the series. This is the fully-local answer to "is this metric trending the
right way?" — the history is already in the repo.

- The snapshot's repo-relative path is resolved as `baseline-guard` does
  (`git rev-parse --show-toplevel`, then Rel). `trend` reads the config only for
  the snapshot path; it needs no measurement. Not inside a git repo, or the
  snapshot path outside the repo → exit 2.
- Commits are those from `git log --format=<sha><TAB><ISO-date> -- <path>`
  reachable from `HEAD` (newest first). If the path was **never committed** →
  exit 2 (`no committed history for <path> — commit the snapshot first`). History
  is traced at the snapshot's **current path**: renaming the snapshot file is a
  boundary (commits before the rename are not reconstructed under the old name).
- For each commit, `git show <sha>:<path>` is parsed. A commit is **skipped with
  a loud `::warning::` (under `GITHUB_ACTIONS`) / `⚠️` note** — never silently —
  when its snapshot cannot be read (`git show` fails, e.g. the commit that
  deleted the file), is **unparseable JSON**, or has an **invalid shape** (e.g. a
  metric with no numeric value). A malformed snapshot must never become a
  measured `0`, and one corrupt historical commit must not abort the whole trend.
  A commit whose (well-formed) snapshot simply lacks a given metric contributes
  no point for that metric (a gap — the dimension didn't exist yet).
- A metric's `direction` and `unit` are taken from its **most recent** appearance
  in the history (the current contract for that metric).
- `<id>` restricts output to that one metric; if it appears in **no** historical
  snapshot → exit 2 (`no metric "<id>" in the snapshot history`). With no `<id>`,
  every metric id that appears anywhere in the history is shown, sorted by id.
- `--limit <n>` (default 20) keeps only the `n` **most recent** snapshot commits;
  `--limit 0` means all. When the history is longer than the limit, a loud line
  (`showing <n> of <m> snapshots (--limit 0 for all)`) is printed — never a
  silent cap. Points are always ordered **oldest → newest** in the output, so the
  per-point Δ reads as "change from the previous point".

**Output (text).** For each metric: a header `<id>  (<direction>, <unit>)`, then
one row per kept commit, oldest first:

```
<short-sha>  <YYYY-MM-DD>  <value>  <Δ>
```

`<value>` is minimal-decimal; `<Δ>` is `—` for the first (oldest) point, else the
signed change from the previous point (`±0` when unchanged), same formatting as
the `check` table's Δ.

**Output (`--format json`).** One JSON object, `metrics` sorted by id, `points`
oldest → newest:

```json
{
  "schema_version": 1,
  "command": "trend",
  "snapshot": "pawl.snapshot.json",
  "metrics": [
    {
      "id": "file-length",
      "direction": "lower-is-better",
      "unit": "files > 500 lines",
      "points": [
        { "commit": "<full sha>", "date": "<ISO-8601>", "value": 3 }
      ]
    }
  ]
}
```

The exit code is 0 on success, 2 on any of the error cases above.

## Public Go API (package `pawl`)

The pure comparison core is exported so tests (and future embedders) hit the same
judgment the CLI uses — one source of truth for "did this get worse":

```go
type Direction string   // "lower-is-better" | "higher-is-better"
type GateMode string    // "total" | "per-file-count" | "per-key-value"

type Metric struct {
    Direction Direction          `json:"direction"`
    Value     float64            `json:"value"`
    Unit      string             `json:"unit"`
    Breakdown map[string]float64 `json:"breakdown"`          // nil ⇔ JSON null
    Tolerance *float64           `json:"tolerance,omitempty"` // nil ⇔ undeclared
}

type Snapshot struct {
    Metrics map[string]Metric `json:"metrics"`
}

type MetricSample struct {          // narrow input for regression checks
    Value     float64
    Breakdown map[string]float64
}

type GateSpec struct {              // how one dimension gates
    Direction Direction
    Gate      GateMode              // "" ⇒ total
    Tolerance float64
}

func Worse(d Direction, base, cur, tolerance float64) bool
func Better(d Direction, base, cur float64) bool
func OffenderCountsByFile(breakdown map[string]float64) map[string]int
func RegressionsOf(spec GateSpec, base, cur MetricSample) []string
func OrphanedMetrics(dimensionIDs []string, baseline map[string]Metric) []string
func BaselineGuardViolations(base, pr map[string]Metric) (violations, removed []string)
func SnapshotShapeErrors(parsed any) []string   // parsed = json.Unmarshal into any
func ImprovementNotice(improvedIDs []string, onCI bool) string // "" when not applicable
func FormatNumber(v float64) string             // minimal decimal notation
```

`BaselineGuardViolations` treats a metric with empty `Direction` as
`lower-is-better` (the conservative default for hand-crafted snapshots) and honors
the metric's own recorded `Tolerance`. Violations are reported in sorted id order;
`removed` is sorted. `OrphanedMetrics` returns sorted ids.
