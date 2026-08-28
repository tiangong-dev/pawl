Part of the pawl engine contract. See [spec/README.md](../README.md).

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

`file` alone has no stale-artifact guard, and cannot: pawl is being told to read
a report someone else produces, so there is nothing to delete and re-run. The
guard it can offer is disclosure — every measurement that reads a file reports
the file's mtime and age in the verdict (`artifact`, specified in
[§ Machine-readable output](../engine/verdict.md#machine-readable-output)), and
in text mode on stderr. By default that is provenance only; setting
`artifact_max_age` on the dimension turns an older file into a measurement
failure (exit 2), while reports produced by the same invocation are always
accepted. Without that opt-in, no age changes an exit code,
because a mtime is not evidence of staleness (a checkout, a `touch`, or a
restored cache all rewrite it) and a gate that fails on false positives gets
switched off. A pipeline that needs the guard should give the dimension the
`command` that produces the report.

The option is rejected on dimensions that do not read a report file; for the
`jscpd` builtin, its configured `report` path is the file being guarded.

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
  `LF:-1 LH:-1` would read as 100%). The cross-record **sums** must stay
  finite — an aggregate that overflows float64 is a measurement failure, not
  a fabricated 0%. `hit ≤ found` holds **per record** — an
  impossible record (2 lines hit out of 1 found) is a measurement failure even
  when another record's slack makes the global totals look consistent. The
  selected metric's found/hit counters must pair **within the same
  `SF:`…`end_of_record` record**, and a record carries **at most one** such
  pair (well-formed lcov emits exactly one summary pair per record; duplicate
  summaries in one record would dilute the percentage; a record with neither
  counter is fine). An unpaired
  counter — a truncated report's `LF` with no `LH`, or a cross-record
  complement (`LF` in one record, `LH` in another, which would read as 100%) —
  is a measurement failure, never a fabricated percentage. A counter **outside
  any record** (before the first `SF:` or after an `end_of_record`) is
  malformed even as a balanced pair — stray counters must not skew the total.
  An explicit `LH:0` is an honest 0%.
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
