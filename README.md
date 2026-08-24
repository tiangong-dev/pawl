<p align="center">
  <img src="assets/banner.svg" alt="pawl — anti-regression quality gate" width="820">
</p>

<p align="center">
  <a href="./README.zh-CN.md">中文文档</a> · <a href="./SPEC.md">SPEC</a> · <a href="./RECIPES.md">Recipes</a>
</p>

<p align="center">
  <a href="https://github.com/tiangong-dev/pawl/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/tiangong-dev/pawl/ci.yml?branch=main&amp;label=CI&amp;logo=github" alt="CI"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/tiangong-dev/pawl"><img src="https://api.scorecard.dev/projects/github.com/tiangong-dev/pawl/badge?v=1" alt="OpenSSF Scorecard"></a>
  <a href="https://www.npmjs.com/package/@pawl-tools/cli"><img src="https://img.shields.io/npm/v/@pawl-tools/cli?logo=npm&amp;color=cb3837" alt="npm"></a>
  <a href="./go.mod"><img src="https://img.shields.io/github/go-mod/go-version/tiangong-dev/pawl?logo=go" alt="Go version"></a>
  <a href="https://github.com/marketplace/actions/setup-pawl"><img src="https://img.shields.io/badge/GitHub%20Marketplace-setup--pawl-2ea44f?logo=github" alt="GitHub Marketplace: setup-pawl"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/tiangong-dev/pawl?color=blue" alt="MIT license"></a>
</p>

**pawl is a quality gate that stops measurable code quality from moving backwards.** It records the state of a repository, runs the same measurements again, and fails when any of them regress.

Unlike a fixed threshold, pawl does not require a legacy codebase to become clean before the gate becomes useful. Today's numbers are the baseline. Existing debt may remain, but a pull request cannot add to it; when a number improves, the new baseline locks that improvement in.

```bash
pawl record                     # record the current baseline
pawl check                      # exit 1 if a metric regressed
pawl record --only line-coverage # lock in one improvement
pawl guard origin/main          # verify that the baseline was not weakened
```

A **dimension** can be anything that produces a number: coverage, passing tests, lint findings, long files, bundle size, dependency cycles, or a project-specific measurement. pawl supplies common adapters and accepts custom commands, so it works with the tools and languages already in the repository.

## Quickstart

Install the static binary through npm, Go, or the install script:

```bash
npm install -D @pawl-tools/cli
# or: go install github.com/tiangong-dev/pawl/cmd/pawl@latest
# or: curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

Then create a config and record the first baseline:

```bash
pawl init
pawl record
git add pawl.yaml pawl.snapshot.json
git commit -m "chore: add pawl quality gate"
```

Run `pawl check` locally and in CI. A regression produces a normal gate failure with enough detail to find the metric that moved:

```console
$ pawl check

metric         baseline    current       Δ  status
------------------------------------------------
file-length           3          4      +1  ❌ worse
panics                1          1      ±0  ✅ same
todo-markers         12         12      ±0  ✅ same

❌ regressions:
  • file-length (Files over 500 lines)
      total 3 → 4
```

That is the whole loop: measure, compare, fix regressions, and record genuine improvements.

## Why a ratchet?

A ratchet only turns one way. pawl works the same way: quality can move forward, but a baseline can't quietly slip back.

Fixed thresholds work once a project already meets them. They're much less useful on a mature repository with 62% coverage, a few 700-line files, and a deadline — set the bar at the goal and every change fails; set it at the status quo and it protects nothing.

pawl starts from wherever the repository already is. The first snapshot records existing debt as-is, no cleanup campaign required, but a regression from there fails immediately: a lower coverage number or a new oversized file exits 1. Improvements stick — re-record a dimension after fixing it, and the gate won't let it drift back. Per-file and per-key comparisons mean one file's fix can't quietly pay for another file's new problem.

The baseline itself is just a JSON file committed to Git. No account, no hosted service, no telemetry — `pawl trend` reads its history straight from commits you already have.

## How pawl measures a repository

`pawl.yaml` defines **dimensions**. Every dimension has an id, a direction, and one source of measurement:

- a zero-dependency primitive such as `file-length` or `pattern-count`;
- an adapter for a tool or report format such as ESLint, Oxlint, SARIF, JUnit, lcov, or cobertura;
- a custom command that prints a number or a measurement object.

The measuring tool is deliberately separate from the verdict. A team can replace one linter with another without replacing the baseline workflow or the CI gate.

### A small, useful config

```yaml
snapshot: "pawl.snapshot.json"

dimensions:
  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["src/**/*.ts", "src/**/*.go"]

  - id: "type-escapes"
    title: "TypeScript escape hatches"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: 'as\s+any|@ts-(ignore|nocheck)'
      include: ["src/**/*.ts", "src/**/*.tsx"]

  - id: "line-coverage"
    title: "Line coverage %"
    direction: "higher-is-better"
    tolerance: 0.5
    builtin: "coverage"
    options:
      file: "coverage/lcov.info"
      format: "lcov"
```

The [recipe cookbook](./RECIPES.md) has ready-to-edit examples for Go, TypeScript, Python, Rust, Swift, common linters, report formats, and custom commands.

### Gate modes

The scalar total is always compared. `gate` can add a more precise comparison:

| gate | use it for | behavior |
|---|---|---|
| `total` | coverage, bundle size, total findings | compares the single value |
| `per-file-count` | lint findings, suppressions, TODOs | findings in one file cannot be offset by fixes in another |
| `per-key-value` | per-package coverage, size by bundle | each key already present in the baseline is protected |

`lower-is-better` is for debt counts and size. `higher-is-better` is for coverage, passing tests, and similar floors. `tolerance` adds absolute slack in the worse direction for noisy measurements.

### Built-in integrations

| builtin | reads or runs | typical gate |
|---|---|---|
| `file-length`, `file-bytes` | repository files | `total` + `per-key-value` |
| `pattern-count` | regular-expression matches | `per-file-count` |
| `eslint`, `oxlint` | native linter output | `per-file-count` |
| `jscpd`, `swift-complexity` | tool-specific JSON | `total` / `per-file-count` |
| `json-value` | one numeric value from JSON | `total` / `per-key-value` |
| `lines` | line-oriented analyzer output | `per-file-count` |
| `sarif` | SARIF findings | `per-file-count` |
| `junit` | passing, failing, skipped, or total tests | `total` |
| `coverage` | lcov or cobertura coverage | `total` |

Named analyzers let several dimensions share one scan. Exact options and output semantics are part of the [engine contract](./spec/README.md).

### Custom commands and `extract`

For a tool pawl does not know, use a command dimension. The full adapter contract accepts a JSON object:

```json
{ "value": 42, "unit": "findings", "breakdown": { "src/a.ts:17": 2 } }
```

If the command already prints a number or one finding per line, `extract` avoids a wrapper script:

```yaml
- id: "circular-deps"
  title: "Import cycles"
  direction: "lower-is-better"
  command: "npx madge --circular --json src | jq 'length'"
  extract: number

- id: "todos"
  title: "TODO markers"
  direction: "lower-is-better"
  gate: "per-file-count"
  command: "grep -rn TODO src"
  valid_exit_codes: [0, 1]
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):'
```

Measurement failures are not clean results. A crashed command, malformed report, timeout, or extraction error makes pawl exit 2; it never turns “could not measure” into “measured zero.” Use `valid_exit_codes` for tools that legitimately report findings with a non-zero exit instead of hiding every failure behind `|| true`.

## Commands

| command | purpose |
|---|---|
| `pawl init` | write a starter `pawl.yaml` without overwriting an existing one |
| `pawl record` | measure and write the snapshot |
| `pawl check` | compare current measurements with the snapshot; the default command |
| `pawl measure` | print current measurements without reading a baseline or giving a verdict |
| `pawl guard <ref>` | compare the snapshot with the version at `<ref>` |
| `pawl trend [<id>]` | show committed snapshot history |
| `pawl rank` | rank included files by line or byte size |
| `pawl agent` | install or print operating instructions for coding agents |
| `pawl version` | print the installed version |

The important exit-code distinction is:

| code | meaning |
|---|---|
| `0` | measurement completed and the gate passed |
| `1` | measurement completed and a metric regressed |
| `2` | pawl could not produce an honest verdict |

Run `pawl help [command]` for the complete flag reference. The stable JSON verdict is available through `--format json`.

## Everyday workflow

### Lock in one improvement

Use a partial record after improving a metric:

```bash
pawl record --only line-coverage
```

Only that dimension is measured and updated. The others are copied from the existing snapshot unchanged, so an unrelated broken adapter cannot block the record and an unnoticed regression cannot be locked in along with it.

### Check only changed lines

For repositories with substantial existing debt:

```bash
pawl check --since origin/main
```

Line-addressable `per-file-count` findings are scoped to changed lines. Metrics that cannot be attributed to a line, such as total coverage, remain fully enforced and are labelled as such. Uncommitted and untracked changes are included.

### Accept debt explicitly

`pawl record` refuses worse values by default. When a regression is deliberate, preview it and record it explicitly:

```bash
pawl record --dry-run --accept-worse
pawl record --accept-worse
```

pawl prints a `Pawl-Accept: <id> <value>` commit trailer. `pawl guard` uses that trailer to distinguish reviewed debt from a weakened snapshot.

### Reuse one measurement

When dimensions read generated reports, measure once so the check and record see the same build:

```bash
pawl measure > .pawl/current.json
pawl check --current .pawl/current.json
pawl record --only line-coverage --current .pawl/current.json
```

### Inspect the trend

```bash
pawl trend
pawl trend line-coverage --limit 50
```

History comes from `pawl.snapshot.json` in Git; no separate database is needed.

## CI integration

Any CI system can install the binary and run `pawl check`. On pull requests, run `guard` as a separate step so changing the snapshot cannot lower the bar unnoticed.

### GitHub Actions

```yaml
permissions:
  contents: read
  pull-requests: write

jobs:
  quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      # Run tests or analyzers that produce reports before the gate.
      - run: npm test -- --coverage

      - uses: tiangong-dev/pawl@v0
        with:
          command: check
          args: --since origin/${{ github.base_ref || 'main' }}

      - if: github.event_name == 'pull_request'
        run: pawl guard origin/${{ github.base_ref }}
```

With `command: check`, the action enforces the exit code and can keep updating one PR comment with the JSON verdict instead of posting a new comment every run. Set `comment: 'false'` if logs and annotations are enough. Without `command`, the action only installs pawl on `PATH`.

### Other CI systems

Use the release binary, the npm package, or:

```bash
npx -y @pawl-tools/cli@0.8.0 check
```

No server-side component is required.

## Optional: coding-agent instructions

The gate works the same whether code was written by a person, a generator, or an agent. If an agent contributes to the repository, `pawl agent` can place its operating instructions in `AGENTS.md` or `CLAUDE.md` so it knows to run the gate, read the JSON verdict, and record improvements narrowly:

```bash
pawl agent --write agent      # AGENTS.md
pawl agent --write claude     # CLAUDE.md
```

This is an integration convenience, not a different mode of pawl. CI remains the authority. The evaluation and fixtures for this command live in [demo/](./demo/README.md).

## Scope

pawl owns **measurement orchestration, comparison, and the verdict**. It is not a new linter, hosted dashboard, package manager, or auto-fixer. Projects continue to install and configure their own analyzers; pawl gives those measurements one consistent baseline and one enforceable answer.

- [Recipes](./RECIPES.md) — configurations to copy and adapt
- [Engine contract](./spec/README.md) — precise behavior and file formats
- [Contributing](./CONTRIBUTING.md) — development and test workflow
- [Changelog](./CHANGELOG.md) — release and migration notes

## License

MIT — see [LICENSE](./LICENSE).
