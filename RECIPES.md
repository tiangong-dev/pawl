# pawl recipes

Copy-paste dimensions for `pawl.yaml`. Each measures one number; `pawl record`
snapshots it and `pawl check` fails a PR when it regresses. Mix and match — a
real config is a handful of these.

New to pawl? Run `pawl init` for a working starter config, then paste the
dimensions you want from here. Full behavioral contract: [SPEC.md](./SPEC.md).

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

---

## Linters & analyzers (you supply the tool)

### ESLint issues

```yaml
- id: "eslint"
  title: "ESLint problems"
  direction: "lower-is-better"
  gate: "per-file-count"
  builtin: "eslint"
  options:
    command: "npx eslint src --format json --no-inline-config"
    # rules: ["sonarjs/cognitive-complexity"]   # optional: count only these rules
```

Filter `rules` to gate one thing precisely — e.g. cognitive complexity via
`sonarjs/cognitive-complexity`, or `@typescript-eslint/no-explicit-any`.

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

### golangci-lint (no builtin needed — `extract`)

```yaml
- id: "golangci"
  title: "golangci-lint findings"
  direction: "lower-is-better"
  gate: "per-file-count"
  command: "golangci-lint run ./... | grep -E '^[^:]+:[0-9]+:' || true"
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+:'
```

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
[SPEC.md § Exec adapter contract](./SPEC.md) and
[§ Declarative extract layer](./SPEC.md).

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
