# pawl

**A language-agnostic anti-regression quality gate.**

中文文档见 [README.zh-CN.md](./README.zh-CN.md) · Full behavioral contract in [SPEC.md](./SPEC.md).

Each **dimension** measures one number — files over a length limit, duplicated
lines, functions over a complexity threshold, test coverage, whatever you can
express as a command that prints a number. `pawl record` snapshots those numbers;
`pawl check` re-measures and **fails CI when any dimension gets worse**. Numbers
can only hold or improve — the gate never slips backward.

```bash
pawl record                     # measure everything, write the baseline
pawl check                      # CI gate: exit 1 on any regression
pawl diff                       # measure + compare, print the table, never fail
pawl baseline-guard origin/main # anti-tamper: catch hand-edited baselines
```

The measuring tool is an implementation detail of each dimension. Swapping ESLint
for another linter, or migrating a whole project onto pawl, means rewriting one
adapter command — the baseline and the CI gate stay put.

---

## Why a quality gate?

A one-shot threshold ("coverage must be ≥ 80%") either blocks the team on day one
or is set so loose it never bites. An anti-regression quality gate instead locks
in *wherever you are today* and only lets it improve: a PR that adds a 600-line
file, a new `as any`, or drops coverage fails; a PR that removes them re-baselines
lower. You pay down debt monotonically without ever picking a magic number.

pawl also guards **honesty**, not just the numbers:

- A measurement that *can't run* (tool crash, missing report, timeout) exits `2`
  — never silently reads as "measured zero". "Could not measure" and "measured
  zero" are different things.
- `baseline-guard` compares the committed snapshot against the PR's base branch,
  so a hand-edited baseline faking a pass is caught.

## Install

```bash
npm install -D @pawl-tools/cli                          # prebuilt binary via npm
go install github.com/tiangong-dev/pawl/cmd/pawl@latest # or build from source
curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

`install.sh` uses whatever is already on the machine — Go, else npm, else a direct
binary download. Prebuilt binaries cover darwin / linux / win32 on x64 / arm64 and
are fully static (CGO-free), so one Linux binary works on glibc and musl alike.

pawl itself is a small dependency-free Go binary. Adapters bring their own runtime
(Node for an ESLint dimension, etc.) — see [Custom adapters](#custom-adapters).

## Quickstart

**1. Write `pawl.yaml`** at your repo root:

```yaml
dimensions:
  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["src/**/*.ts"]
      exclude: ["**/*.d.ts"]

  - id: "todos"
    title: "TODO / FIXME markers"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "TODO|FIXME"
      include: ["src/**/*.ts"]
```

**2. Record the baseline** — commit the generated `pawl.snapshot.json`:

```bash
pawl record
git add pawl.yaml pawl.snapshot.json && git commit -m "chore: add pawl gate"
```

**3. Gate every PR** — `pawl check` exits `1` if any dimension regressed:

```bash
pawl check
```

**4. Lock in wins.** When a PR improves a number, `check` tells you to re-record;
`pawl record` writes the new, lower baseline so it can never slip back up.

## Commands

| command | what it does |
|---|---|
| `pawl record` | measure every dimension and (over)write the snapshot |
| `pawl check` | measure + compare; **exit 1 on any regression** — the CI gate |
| `pawl diff` | measure + compare, print the table, always exit 0 |
| `pawl baseline-guard <ref>` | compare the working-tree snapshot against the version committed at `<ref>` — the anti-tamper gate |
| `pawl version` | print `pawl <version>` (works with no config present) |

`-c <path>` selects the config file (default `./pawl.yaml`). No command defaults
to `check`.

**Flags.** `--format json` makes `record`/`check`/`diff` print a stable
machine-readable verdict instead of the table ([schema](./SPEC.md)) — pawl stays
the gate, any reporter consumes the JSON. `--format codeclimate` emits a
[Code Climate issue array](#gitlab-code-quality) for GitLab's Code Quality widget.
`check --since <ref>` scopes the gate to lines changed since `<ref>`
([clean-as-you-code](#diff-scoped-checking)).

### Exit codes

| code | meaning |
|------|---------|
| **0** | pass (also `diff` with regressions, and legitimate `baseline-guard` skips) |
| **1** | `check`: a dimension regressed · `baseline-guard`: the snapshot regressed vs `<ref>` |
| **2** | cannot measure/compare honestly: bad config, missing/malformed snapshot, tool crash, timeout, unknown command, … |

The **1-vs-2 split is load-bearing**: `1` means "measured fine, code got worse";
`2` means "could not measure honestly" and must never read as a pass.

## Configuration

`pawl.yaml` lists dimensions; each is either a **built-in** or a **custom command**
(exactly one of `builtin` / `command`).

```yaml
snapshot: "pawl.snapshot.json"   # optional, relative to this file

dimensions:
  - id: "cognitive-complexity"   # required, unique
    title: "Functions over cc 15" # required, human-readable
    direction: "lower-is-better" # required: lower-is-better | higher-is-better
    gate: "per-file-count"       # optional: total (default) | per-file-count | per-key-value
    tolerance: 0                 # optional, absolute slack in the worse direction
    timeout: "10m"               # optional Go duration, default 10m
    builtin: "eslint"            # a built-in adapter …
    options:
      command: "npx eslint src --format json --no-inline-config"
      rules: ["sonarjs/cognitive-complexity"]

  - id: "coverage"
    title: "Line coverage"
    direction: "higher-is-better"
    gate: "per-key-value"
    tolerance: 1
    command: "./scripts/coverage.sh"   # … or a custom command
```

## Built-in adapters

Two tiers. **Primitives** are Go-native (zero dependencies). **Tool adapters**
run an analyzer *you* invoke and parse its machine output — pawl owns the format
knowledge, you own the tool setup.

| builtin | tier | measures | typical gate |
|---|---|---|---|
| `file-length` | primitive | files whose line count exceeds `threshold` | `total` |
| `pattern-count` | primitive | regexp matches (suppressions, escape hatches: `as any`, `//nolint`, `try!`) | `per-file-count` |
| `eslint` | adapter | counted ESLint messages (optionally filtered by `rules`) | `per-file-count` |
| `jscpd` | adapter | duplicated lines from a jscpd JSON report | `total` |
| `swift-complexity` | adapter | Swift **cognitive** complexity offenders (what SwiftLint can't) | `per-file-count` |
| `json-value` | adapter | one number out of any tool's JSON (coverage %, passing tests, type-coverage) — the home of `higher-is-better` | `per-key-value` |

Each builtin's exact options, exit-code handling, and breakdown shape are in
[SPEC.md § Built-in adapters](./SPEC.md). Full example configs live in the
consuming projects.

## Custom adapters

**pawl imposes no language requirement.** A dimension's `command` is run via
`sh -c` — it can be a shell script, Node, Python, Go, a compiled binary, `curl |
jq`, anything. It just has to honor the contract:

- Print **exactly one JSON object** to stdout:
  ```json
  { "value": 42, "unit": "things", "breakdown": { "src/a.ts:17": 2 } }
  ```
  `value` is required and finite. `unit` defaults to `"count"`. `breakdown` is
  optional (`null` / omitted are equivalent).
- **Exit 0 = a measurement. Non-zero / timeout / non-JSON stdout = a measurement
  failure** → pawl aborts with exit 2. This is why a raw command beats `tool ||
  true`: real failures stay detectable.
- cwd is the config directory; `PAWL_ROOT` is set to its absolute path; stderr
  passes through for human diagnostics.

The `breakdown` keys power the [gate modes](#gate-modes): use `"<path>:<line>"`
keys for `per-file-count`, or named keys (`"pkg-a": 91.2`) for `per-key-value`.

> This is how any project migrates onto pawl without pawl needing to understand
> its tools: wrap the existing measurement in a command that prints that JSON.

### Skip the wrapper: `extract`

When the tool already prints the number (or a greppable finding list), declare
`extract` on the `command` dimension and pawl derives the measurement — no JSON
wrapper script. Four forms:

```yaml
- id: todos
  command: "grep -rn TODO src || true"
  direction: "lower-is-better"
  extract: lines            # value = non-empty line count

- id: golangci
  command: "golangci-lint run ./... | grep -E '^[^:]+:[0-9]+:' || true"
  direction: "lower-is-better"
  gate: "per-file-count"
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):'   # value = matches; path/line → breakdown
```

Also `extract: number` (stdout is one number) and `extract: { json_path: "a.b.c" }`
(read one number from the command's stdout JSON). Same honesty rule: a non-zero
exit, or output that can't be extracted, is a measurement failure (exit 2) — with
`regex`, every non-empty line must match, so a mistyped pattern can't report a
silent zero. Details in [SPEC.md § Declarative extract layer](./SPEC.md).

## Gate modes

The scalar total is **always** checked (with `tolerance`). A per-breakdown check
on top stops a localized regression from hiding behind a net-zero total (file A
improves, file B worsens, total unchanged):

- **`total`** — scalar only. (Growing an already-long file shouldn't fail; only a
  new file crossing the limit, which moves the total, should.)
- **`per-file-count`** — offender *count* per file may not rise. The file is the
  substring before the first `:` in each breakdown key. Counts keys, not values,
  so code moving inside a file doesn't trip it.
- **`per-key-value`** — every baseline key's *value* may not worsen (with
  tolerance). New keys and removed keys are ignored. Ideal for per-package
  coverage / type-coverage.

`tolerance` is absolute slack in the worse direction; a value exactly at the
boundary passes. `higher-is-better` and `lower-is-better` flip the comparison.

Pick the gate for the shape: `per-file-count` is the strong net-zero defense for
*issue-count* dimensions (offenders coming and going); `per-key-value` fits
*stable-key numeric* dimensions (a fixed key set whose values move) and only
guards keys already in the baseline. Neither is a universal net-zero proof — the
[SPEC](./SPEC.md#gate-modes) spells out the edges.

## CI integration

pawl is a single binary — any CI can run it. Two common wirings:

### GitHub Actions

The action installs the binary; on its own that is all it does:

```yaml
- uses: tiangong-dev/pawl@v0.3.0   # puts the pawl binary on PATH — no Go/Node
  with:
    version: v0.3.0                # optional; defaults to the latest release
- run: pawl check
- run: pawl baseline-guard origin/${{ github.base_ref }}   # on PRs
```

Pass `command` and the action also runs the gate and, on a pull request, upserts
one sticky comment with the result (rendered from the `--format json` verdict) —
no bespoke `github-script` step:

```yaml
- run: # any pre-steps the gate needs, e.g. build exec adapters
- uses: tiangong-dev/pawl@v0.3.0
  with:
    command: check
    args: --since origin/${{ github.base_ref }}   # optional extra args
    # comment: 'true'   # default; set 'false' to skip the PR comment
```

The comment step needs `permissions: pull-requests: write`. The gate's exit code
is enforced after the comment, so a regression still fails the job while the
comment posts. Under `GITHUB_ACTIONS`, `check` also emits inline `::error::`
annotations on the PR diff for each regression, and a `::notice::` when a
dimension improved but the baseline wasn't re-recorded.

### GitLab Code Quality

`--format codeclimate` emits a Code Climate issue array — every current
per-file-count offender as a located finding — which GitLab renders as the Merge
Request **Code Quality** widget and inline diff annotations. New-vs-fixed is
GitLab's own comparison of the MR-branch report against the target branch, so the
job just publishes the artifact:

```yaml
quality-gate:
  image: node:22
  script:
    - npx -y @pawl-tools/cli@0.3.0 check --format codeclimate > gl-code-quality-report.json
  artifacts:
    when: always                 # publish the report even when the gate fails
    reports:
      codequality: gl-code-quality-report.json
```

`check`'s exit code still gates the pipeline (1 on a regression vs the snapshot);
`total`/`per-key-value` dimensions have no per-line location and so add no inline
findings, but their gate is still enforced through that exit code.

### Anything else

pawl is a single binary — run `npx -y @pawl-tools/cli@0.3.0 check` (or download
the release binary) in any CI.

### Anti-tamper

`pawl check` only proves the snapshot on disk matches a fresh measurement — not
that the snapshot's history is honest. `pawl baseline-guard <base-ref>` compares
the committed snapshot against the PR's base branch and fails if it was
hand-edited to a worse value. Run it on PRs alongside `check`.

## Diff-scoped checking

`pawl check --since <ref>` keeps the full gate but **only fails on regressions
introduced by lines changed since `<ref>`** — pre-existing debt on untouched lines
is exempted, so a large legacy baseline doesn't block every PR while new code
still can't regress. It still needs the snapshot (it's the gate narrowed to new
code, not a standalone scanner).

```bash
pawl check --since origin/main        # on a PR: gate only the changed lines
```

`per-file-count` dimensions (breakdown keyed `"path:line"`, scalar = the offender
count) are scoped to the added lines; `total` and `per-key-value` dimensions
have no line to attribute faithfully and are **enforced in full** (loudly
labelled, never silently skipped), keeping `--since` exactly the full-mode
verdict narrowed to changed lines. The output reports the merge-base, what was
enforced in full, and how many pre-existing regressions were exempted; add
`--format json` for the machine-readable form (`mode: "since"`, each exempted
regression flagged `suppressed`).

Scoping is line-based (like reviewdog / Sonar clean-as-you-code): a pre-existing
offender that merely shifts position isn't flagged, but one on a line whose
content actually changed counts even if it "moved" there — it never
under-reports a changed line. Details in [SPEC.md](./SPEC.md#diff-scoped-checking).

## Scope boundary

pawl is a **quality gate + honesty guard, not a code analyzer** — it never parses a
language. Line counting and regexp matching are Go-native because they need no
grammar; everything requiring real language semantics (complexity, type escapes)
is delegated to that language's own best analyzer through an adapter, so the gate
agrees with what developers already see in their IDE. Rationale in
[SPEC.md § Scope boundary](./SPEC.md).

## License

MIT — see [LICENSE](./LICENSE).
