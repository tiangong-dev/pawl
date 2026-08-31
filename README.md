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

Coding agents are fast. They're also good at making a repo slightly worse in ways a diff review misses: coverage down a point, a new 800-line file, a lint you already banned. The PR looks fine. The floor moved.

pawl is a language-agnostic quality ratchet for CI. It records the numbers a repository already produces — coverage, lint findings, failing tests, file sizes, bundle size — as a baseline committed to Git, then exits 1 when a change makes one of them worse. Old mess can stay. New mess cannot. There is no server, no account, and no telemetry.

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

## Why not just set coverage to 80%?

Because the repo isn't at 80%. It's at 62%, with three 700-line files, and a deadline. A fixed bar either fails every PR or protects nothing.

pawl starts at whatever you have today. The first snapshot is the floor. After that, worse is a red CI. When you actually fix something, re-record that one metric so the new floor sticks. Per-file and per-key comparisons mean fixing file A does not pay for a new mess in file B.

The baseline is a JSON file in Git, so `pawl trend` reads history from commits you already have.

## How pawl compares

Stopping a number from getting worse is not a new idea. The implementations differ in where the verdict is computed, and in how wide the set of protected numbers is.

| | server or account | languages | what it protects |
|---|---|---|---|
| **pawl** | none — the baseline is a JSON file in the repository | any, via adapters and custom commands | any number a command can print |
| SonarQube Server / Cloud | required — the gate is evaluated by a server, including in the free self-hosted edition | 35+, via its own analyzers | what its analyzers report |
| Qlty (formerly Code Climate Quality) | not for the CLI; trend and pull-request dashboards are hosted | 40+, via 70+ bundled analyzers | what those analyzers report |
| Codecov / Coveralls | required — reports are uploaded to the service | any, via coverage report formats | coverage only |
| betterer | none | Node.js; the bundled tests target JS, TS, and CSS | anything its generic test API returns |
| git-ratchet | none — the baseline lives in git-notes | any, via `measure,value` on stdin | any number piped into it |

Two things are regularly mistaken for this and are worth separating:

**"Clean as You Code" is not a ratchet.** SonarQube applies fixed thresholds to *new code* — the lines a change touched. That is a different question from whether the repository-wide number is worse than the one recorded last week. Both let a legacy codebase stay legacy; only the second notices when the total drifts.

**A diff filter has no memory.** `golangci-lint --new-from-rev` and `reviewdog -filter-mode=added` narrow findings to changed lines, which is useful and cheap. But nothing there catches a metric that gets worse while the responsible line was never edited: a dependency that pulled in more code, a bundle that grew, a test that started being skipped.

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

File-backed report dimensions may set `artifact_max_age: "24h"` to turn an old
artifact into exit 2; command-produced reports are considered fresh by
construction. Without that option pawl reports age as provenance only.

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

      - uses: tiangong-dev/pawl@v0.8.2
        with:
          command: check
          args: --since origin/${{ github.base_ref || 'main' }}
          guard-ref: origin/${{ github.base_ref || 'main' }}
```

With `command: check`, the action enforces the exit code and can keep updating one PR comment with the JSON verdict instead of posting a new comment every run. Set `guard-ref` after fetching the base ref to make baseline protection part of the same action; if `args` contains `-c/--config`, the same config is used for the guard. The guard runs before the optional comment. Set `comment: 'false'` if logs and annotations are enough — and drop `pull-requests: write` from the job's `permissions:` block, since the comment is the only thing that needs it. Without `command`, the action only installs pawl on `PATH`.

### GitLab and other systems without a native pawl widget

`pawl check --format json` is the stable contract; [scripts/gitlab-codequality.mjs](scripts/gitlab-codequality.mjs) converts one verdict into a [GitLab Code Quality](https://docs.gitlab.com/ci/testing/code_quality/) report for the merge-request widget. It is a converter script, not a pawl output format — GitLab is not a pawl target the way GitHub Actions is, so this stays outside the CLI surface.

```yaml
quality:
  script:
    - curl -fsSL -o gitlab-codequality.mjs https://raw.githubusercontent.com/tiangong-dev/pawl/main/scripts/gitlab-codequality.mjs
    - pawl check --format json > pawl.json || rc=$?
    - node gitlab-codequality.mjs pawl.json > gl-code-quality.json
    - exit ${rc:-0}
  artifacts:
    when: always
    reports:
      codequality: gl-code-quality.json
```

`main` above always has the current script; pin to a specific tag or commit instead once one has shipped it (the same way `guard-ref`/the Action version are pinned elsewhere in this doc) — vendoring a copy into the repo works too. Capture `rc` before converting and `exit` after — `&&` would short-circuit on the regression/could-not-measure exit codes and skip the report exactly when the widget matters most. A could-not-measure verdict (exit 2) still produces a blocker-severity issue rather than an empty report, so a broken gate cannot read as a clean one.

pawl reports paths relative to the config file's directory, not the repository root. If `pawl check` above ran with `-c config/pawl.yaml`, pass that directory through so GitLab attaches issues to the right file: `node gitlab-codequality.mjs pawl.json --config-dir=config`. `--anchor` (used for the could-not-measure/total-only fallback location) is itself config-relative and defaults to `pawl.yaml`, so it already resolves under `--config-dir` correctly without repeating the directory — override it only if the config file has a different name, e.g. `--anchor=quality.yaml` for `config/quality.yaml`, not `--anchor=config/quality.yaml`.

### Other CI systems

Jenkins, CircleCI, Buildkite, Azure Pipelines, Woodpecker — anything that can run a binary needs no plugin. Use the release binary, the npm package, or:

```bash
npx -y @pawl-tools/cli@0.8.2 check
```

No server-side component is required.

## Agents

If an agent is writing code in this repo, tell it about the gate:

```bash
pawl agent --write agent      # AGENTS.md
pawl agent --write claude     # CLAUDE.md
```

CI is still the authority. `pawl agent` just writes the operating notes so the agent runs `pawl check`, reads the JSON verdict, and records a fix narrowly instead of rewriting the snapshot. Demo and fixtures: [demo/](./demo/README.md).

## Scope

pawl owns **measurement orchestration, comparison, and the verdict**. It is not a new linter, hosted dashboard, package manager, or auto-fixer. Projects continue to install and configure their own analyzers; pawl gives those measurements one consistent baseline and one enforceable answer.

- [Recipes](./RECIPES.md) — configurations to copy and adapt
- [Engine contract](./spec/README.md) — precise behavior and file formats
- [Contributing](./CONTRIBUTING.md) — development and test workflow
- [Changelog](./CHANGELOG.md) — release and migration notes

## License

MIT — see [LICENSE](./LICENSE).
