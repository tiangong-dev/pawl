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
pawl [command] [-c <config>]

  record               measure every dimension and (over)write the snapshot
  check                measure + compare; exit 1 on any regression — the CI gate
  diff                 measure + compare, print the table, always exit 0
  baseline-guard <ref> compare the working tree's snapshot against the version
                       committed at <ref> — the anti-tamper gate
  version              print `pawl <version>` and exit 0
```

- No command defaults to `check`.
- `-c <path>` selects the config file; default `./pawl.yaml`.
- Unknown command → stderr message naming valid commands, exit 2.
- `pawl version` and `pawl --version` print exactly `pawl <version>\n` to
  stdout and exit 0 **without reading any config file** — they must work in a
  directory with no `pawl.yaml`. The version string defaults to `dev` and is
  overridden at build time via
  `-ldflags "-X github.com/tiangong-dev/pawl/internal/pawl.Version=<x.y.z>"`.

### Exit codes

| code | meaning |
|------|---------|
| 0 | pass (including `diff` with regressions, and legitimate baseline-guard skips) |
| 1 | `check`: at least one dimension regressed; `baseline-guard`: snapshot regressed vs `<ref>` |
| 2 | anything that prevents an honest verdict: unknown command, missing/invalid config, no dimensions, missing snapshot for `check`/`diff`, malformed snapshot shape, orphaned metric, measurement failure, unresolvable git ref |

The 1-vs-2 split is load-bearing: 1 means "measured fine, code got worse";
2 means "could not measure/compare honestly" and must never read as a pass.

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
