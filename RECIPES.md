# pawl recipes

Copy-paste dimensions for `pawl.yaml`. Each measures one number; `pawl record`
snapshots it and `pawl check` fails a PR when it regresses. Mix and match — a
real config is a handful of these.

New to pawl? Run `pawl init` for a working starter config, then paste the
dimensions you want from here. Full behavioral contract: [SPEC.md](./SPEC.md) ([spec/](./spec/README.md)).

**Picking a gate** (the `gate:` field):

| your metric looks like… | use | why |
|---|---|---|
| one scalar (a %, a total, a size) | `total` (default) | there's nothing per-file to attribute |
| a list of findings that come and go (lint issues, TODOs) | `per-file-count` | a fix in file A can't be traded for a new offender in file B |
| a fixed set of named values (per-package coverage) | `per-key-value` | each key is guarded against dropping |

**Picking a direction:** `lower-is-better` for counts of bad things (issues,
duplication, long files); `higher-is-better` for good things that must not drop
(coverage, passing tests).

---

## Primitives (zero dependencies)

### Long files

```yaml
- id: "file-length"
  title: "Files over 500 lines"
  direction: "lower-is-better"
  builtin: "file-length"
  options:
    threshold: 500
    include: ["src/**/*"]
    exclude: ["**/*.snap", "**/*.min.*"]
```

Pair a second dimension on the same builtin with `per-key-value` so already-long
files cannot keep growing (the usual AI failure mode). `total` still catches a
*new* file crossing the limit; `per-key-value` ignores new keys, so the two are
complements, not duplicates.

```yaml
- id: "file-length-growth"
  title: "Already-long files must not grow"
  direction: "lower-is-better"
  gate: "per-key-value"
  builtin: "file-length"
  options:
    threshold: 500
    include: ["src/**/*"]
    exclude: ["**/*.snap", "**/*.min.*"]
```

### Large files by bytes

Closer proxy for token cost than line count. Same pairing as `file-length`.

```yaml
- id: "file-bytes"
  title: "Files over 32 KiB"
  direction: "lower-is-better"
  builtin: "file-bytes"
  options:
    threshold: 32768
    include: ["src/**/*"]
    exclude: ["**/*.snap", "**/*.min.*"]
```

### Escape hatches / suppressions

The generic "count the debt marker" dimension. Swap `pattern` for your stack:

```yaml
- id: "ts-any"
  title: "`as any` / `: any` escapes"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "pattern-count"
  options:
    pattern: "as any|: any|@ts-(ignore|expect-error|nocheck)"
    include: ["src/**/*.ts", "src/**/*.tsx"]
```

Other patterns worth gating: `//nolint` (Go), `# type: ignore` (Python),
`eslint-disable`, `try!` / `as!` (Swift), `!important` (CSS), `console\.log`.

### AI-generated debt

These are the markers agent-written code tends to introduce. They are all
`pattern-count` — no language parser. Use them on the inner loop
(`pawl check --only …`); keep ESLint / coverage on CI.

```yaml
- id: "todos"
  title: "TODO / FIXME markers"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "pattern-count"
  options:
    pattern: "TODO|FIXME"
    include: ["src/**/*"]

- id: "lint-suppressions"
  title: "Lint / type suppressions"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "pattern-count"
  options:
    pattern: "eslint-disable|@ts-(ignore|expect-error|nocheck)|nolint|# type: ignore"
    include: ["src/**/*"]

- id: "skipped-tests"
  title: "Skipped tests"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "pattern-count"
  options:
    pattern: "\\.(skip|only)\\(|xtest|xdescribe|it\\.skip|describe\\.skip|test\\.skip"
    include: ["**/*.{test,spec}.*", "**/test/**", "**/tests/**"]
```

Swap `include` / `pattern` for the stack. Combine with the `file-length` +
`file-length-growth` pair above so new code cannot hide in already-long files.

---

## Linters & analyzers (you supply the tool)

### ESLint issues

```yaml
analyzers:
  - id: "frontend-eslint"
    builtin: "eslint"
    verify:
      - "npx eslint --print-config src/probe.ts"
    options:
      command: "npx eslint src --format json --no-inline-config"
      min_files: 1

dimensions:
  - id: "cognitive-complexity"
    title: "Cognitive complexity"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend-eslint"
    options:
      rules: ["sonarjs/cognitive-complexity"]

  - id: "type-escapes"
    title: "Explicit any"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend-eslint"
    options:
      rules: ["@typescript-eslint/no-explicit-any"]
```

The analyzer runs once; each dimension filters the decoded findings. `verify`
commands are representative ESLint `--print-config` probes. They turn a missing,
misspelled, or disabled filtered rule into exit 2 without treating a legitimate
clean zero as suspicious.

### Oxlint (Oxc)

Oxlint has a first-class native JSON analyzer. It reports the number of scanned
files directly, preserves multiple diagnostics on the same line, and runs only
once when several dimensions select different rules or severities:

```yaml
analyzers:
  - id: "frontend-oxlint"
    builtin: "oxlint"
    verify:
      # Add representative files when nested configs/overrides differ.
      - "npx oxlint --print-config src/probe.ts"
    options:
      command: "npx oxlint src --format json"
      min_files: 1

dimensions:
  - id: "debugger-statements"
    title: "Debugger statements"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend-oxlint"
    options:
      # Selectors use native diagnostic codes. The matching config key for
      # eslint(no-debugger) is no-debugger.
      rules: ["eslint(no-debugger)"]

  - id: "invalid-fetch-options"
    title: "Invalid fetch options"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend-oxlint"
    options:
      # unicorn/no-invalid-fetch-options in --print-config maps to this code.
      rules: ["unicorn(no-invalid-fetch-options)"]
      levels: ["error", "warning"]
```

Oxlint exit 0 (no error-level diagnostics) and exit 1 (errors found) are valid
only when stdout is a complete native JSON report. A fatal invocation that also
uses exit 1 emits no valid report and therefore fails measurement instead of
becoming zero; every exit other than 0/1 is fatal. `verify` is optional, but
recommended whenever `rules` selectors are used: every selected diagnostic code
must map to an enabled rule in at least one `--print-config` result.

Pin Oxlint in the project rather than invoking `@latest`; rules and defaults may
grow in non-breaking Oxlint releases, and Pawl should record such changes as an
intentional baseline update.

### golangci-lint via SARIF

golangci-lint can feed several dimensions from one named SARIF analyzer.
Disable its default truncation and per-line deduplication or Pawl cannot recover
findings the producer omitted:

```yaml
analyzers:
  - id: "go-lint"
    builtin: "sarif"
    options:
      command: >-
        golangci-lint run
        --output.sarif.path=.pawl/golangci.sarif
        --max-issues-per-linter=0 --max-same-issues=0 --uniq-by-line=false
        ./...
      file: ".pawl/golangci.sarif"
      valid_exit_codes: [0, 1] # 1 is golangci-lint's default "issues found"

dimensions:
  - id: "go-lint-findings"
    title: "Go lint findings"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "go-lint"
```

`min_files` is also available as an opt-in completeness floor, but it counts
SARIF `artifacts`. Enable it only after confirming the pinned producer emits
that optional catalog; locations inside `results` alone cannot prove how many
files were scanned.

### Code duplication (jscpd)

```yaml
- id: "duplication"
  title: "Duplicated lines"
  direction: "lower-is-better"
  builtin: "jscpd"
  options:
    command: "npx jscpd src --min-tokens 50 --reporters json --output .pawl/jscpd --silent"
    report: ".pawl/jscpd/jscpd-report.json"
```

### Swift cognitive complexity

```yaml
- id: "swift-complexity"
  title: "Swift functions over cognitive 15"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "swift-complexity"
  options:
    command: "swift-complexity Sources --recursive --format json"
    threshold: 15
    metric: "cognitive"   # or "cyclomatic"
```

### Legacy line-oriented linter output (`extract`)

```yaml
- id: "legacy-lint"
  title: "Legacy linter findings"
  direction: "lower-is-better"
  gate: "per-file-count"
  command: "my-linter --format concise"
  valid_exit_codes: [0, 1]   # this linter exits 1 when it finds something
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+:'
```

`valid_exit_codes` is the complete set of exit codes that count as a successful
run; leave it out and the contract is the default `[0]`. Reach for it instead of
appending `|| true`, which accepts *every* exit code — a crashed linter, a typo
in a flag, or a missing binary would then measure a clean zero.

---

## Report-format ingest (sit on top of the ecosystem)

These read the ecosystem's standard machine formats — a scanner's SARIF, a
runner's JUnit XML, a coverage report — so the tool needs no wrapper. They gate
on a **parseable report**, not the exit code (these producers exit non-zero to
signal findings/failures).

### SARIF scanners (Semgrep, CodeQL, Trivy, …)

```yaml
- id: "semgrep"
  title: "Semgrep findings"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "sarif"
  options:
    command: "semgrep --config auto --sarif src || true"
    # file: "results.sarif"            # …or read a report a prior step wrote
    # levels: ["error", "warning"]     # optional: count only these levels
    # rules: ["python.lang.security.audit.xss"]   # optional: only these ruleIds
```

### Passing tests must not drop (JUnit)

```yaml
- id: "tests-passing"
  title: "Passing tests"
  direction: "higher-is-better"
  builtin: "junit"
  options:
    command: "pytest --junitxml=.pawl/junit.xml || true"
    file: ".pawl/junit.xml"
    count: "passing"   # or "failures" (lower-is-better), "tests", "skipped"
```

### Coverage must not drop (lcov / cobertura)

```yaml
- id: "coverage"
  title: "Line coverage %"
  direction: "higher-is-better"
  tolerance: 0.5          # absorb rounding noise
  builtin: "coverage"
  options:
    file: "coverage/lcov.info"
    format: "lcov"        # or "cobertura"
    metric: "lines"       # or "branches"; "functions" (lcov only)
    # command: "npm test -- --coverage || true"   # optional: produce the file first
```

A `coverage-summary.json` (Istanbul/nyc) is a one-number read — use `json-value`:

```yaml
- id: "coverage-summary"
  title: "Line coverage %"
  direction: "higher-is-better"
  tolerance: 0.5
  builtin: "json-value"
  options:
    file: "coverage/coverage-summary.json"
    path: "total.lines.pct"
    unit: "%"
```

---

## Read one number out of any JSON (`json-value`)

The generic reader behind coverage, passing-test counts, `type-coverage`, and
anything else that prints a JSON number.

```yaml
- id: "type-coverage"
  title: "TypeScript type coverage %"
  direction: "higher-is-better"
  tolerance: 0.5
  builtin: "json-value"
  options:
    command: "npx type-coverage --json"
    path: "percentage"
    unit: "%"
```

Per-package values with `per-key-value` gating — a command that prints
`{ "pkg-a": 91.2, "pkg-b": 88.0 }` and a matching custom adapter (see below)
guards each key from dropping.

---

## Anything else (custom command)

pawl is language-agnostic: a dimension's `command` runs via `sh -c` and just
has to print one JSON object `{ "value": <number>, "unit"?: …, "breakdown"?: … }`.

```yaml
# Bundle size ceiling — the command prints a single number, extract reads it.
- id: "bundle-kb"
  title: "Bundle size (KB)"
  direction: "lower-is-better"
  command: "du -k dist/bundle.js | cut -f1"
  extract: number

# Circular dependencies (madge prints a JSON array).
- id: "circular-deps"
  title: "Import cycles"
  direction: "lower-is-better"
  command: "npx madge --circular --json src | jq 'length'"
  extract: number
```

For the full raw-JSON contract, the four `extract` forms, and the exec adapter
environment (`PAWL_ROOT`, cwd, timeout, exit-code honesty), see
[SPEC.md § Exec adapter contract](./spec/adapters/exec.md) and
[§ Declarative extract layer](./spec/adapters/extract.md).

---

## After you have a config

```bash
pawl record        # snapshot the baseline — commit pawl.snapshot.json
pawl check         # the CI gate: exit 1 on any regression
pawl diff          # see the table without gating (always exit 0)
pawl trend         # each metric's value over the committed snapshot's git history
```

- **Lock in a win on one dimension** without re-blessing the rest:
  `pawl record --only <id>`.
- **Only fail on new code** (grandfather existing debt): `pawl check --since origin/main`.
- **Stop hand-edited baselines**: `pawl baseline-guard origin/main` in CI.
- **Give a noisy metric slack**: set `tolerance` (absolute, in the worse direction).
