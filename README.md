# pawl

**A language-agnostic anti-regression quality gate.**

中文文档见 [README.zh-CN.md](./README.zh-CN.md) · Full behavioral contract in [SPEC.md](./SPEC.md) ([spec/](./spec/README.md)).

Each **dimension** measures one number — files over a length limit, duplicated
lines, functions over a complexity threshold, test coverage, whatever you can
express as a command that prints a number. `pawl record` snapshots those numbers;
`pawl check` re-measures and **fails CI when any dimension gets worse**. Numbers
can only hold or improve — the gate never slips backward.

```bash
pawl record                     # measure everything, write the baseline
pawl check                      # CI gate: exit 1 on any regression
pawl baseline-guard origin/main # anti-tamper: catch hand-edited baselines
```

The measuring tool is an implementation detail of each dimension. Swapping ESLint
for another linter, or migrating a whole project onto pawl, means rewriting one
adapter command — the baseline and the CI gate stay put.

## Why pawl

- **Measure anything, in any language.** A dimension is any command that prints a
  number — coverage, passing-test count, bundle size, `as any` count, cycles in a
  dependency graph, a metric only your codebase has. pawl parses no code and ships
  no plugins to keep current; it ratchets whatever number you feed it.
- **The baseline lives in your repo.** The snapshot is one JSON file committed to
  git — the single source of truth for *where you are today*. Fully local and
  offline: no account, no server, no dashboard, nothing about your code leaves CI.
- **A ratchet, not a threshold.** It locks in wherever you stand and only lets it
  improve. Existing debt is grandfathered per file, so you are never forced to fix
  everything at once — and the number can never slip back up.
- **Per-file / per-key precision.** A localized regression can't hide behind a
  net-zero total, and an offender that merely moves *within* a file doesn't cry
  wolf — the baseline remembers each file's count, not just the grand total.
- **Honest by construction.** "Couldn't measure" (exit `2`) is never mistaken for
  "measured zero", and a hand-edited baseline is caught by `baseline-guard`. The
  gate would rather stop loud than pass a lie.
- **One static binary.** Drop it into any CI in seconds; it sits on top of the
  tools you already run, and renders natively as GitHub PR comments and
  annotations.

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

`pawl init` scaffolds a working starter config (then paste more from the
[recipe cookbook](./RECIPES.md)); or write `pawl.yaml` by hand:

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

**5. Tell your coding agent the gate exists** — otherwise it finds out from a
red CI run, or not at all:

```bash
pawl agent-md --write   # appends the operating loop to AGENTS.md
```

## Commands

| command | what it does |
|---|---|
| `pawl init` | scaffold a starter `pawl.yaml` (never overwrites) |
| `pawl agent-md [--write]` | print the operating loop a coding agent needs to use this gate, or append it to `AGENTS.md` |
| `pawl measure` | measure every dimension and print the numbers — no baseline, no verdict; the document is the snapshot format |
| `pawl record` | measure every dimension and (over)write the snapshot |
| `pawl check` | measure + compare; **exit 1 on any regression** — the CI gate |
| `pawl baseline-guard <ref>` | compare the working-tree snapshot against the version committed at `<ref>` — the anti-tamper gate |
| `pawl trend [<id>]` | print each metric's value across the committed snapshot's git history — a fully local trend, no cloud |
| `pawl rank` | rank included files by line or byte size (including near-threshold files) |
| `pawl version` | print `pawl <version>` (works with no config present) |
| `pawl help [<command>]` | print global or command help (also `-h` / `--help`) |

`-c <path>` selects the config file (default `./pawl.yaml`). Omitting the
command runs `check`.

**Flags.** `--format json` makes `record`/`check` print a stable
machine-readable verdict instead of the table ([schema](./spec/engine/verdict.md#machine-readable-output)) — pawl stays
the gate, any reporter consumes the JSON.
`check --since <ref>` scopes the gate to lines changed in the working tree since `<ref>`
([clean-as-you-code](#diff-scoped-checking)). `--only <id>[,<id>…]` on `record`
re-records just those dimensions and preserves the rest of the committed
baseline; on `check` it measures and compares only those dimensions (the
agent inner loop — CI should still run a full `check`). `record` refuses to write
a dimension worse than the committed baseline unless you pass `--accept-worse`;
`--dry-run` previews what a record would write without writing it
([accepted debt](#accepting-debt---accept-worse---dry-run)).
`check`/`record --current <path|->` judge or record a `pawl measure` document
instead of running the dimensions, so one measurement drives every decision that
follows it ([measure](./spec/commands/measure.md)).

### Exit codes

| code | meaning |
|------|---------|
| **0** | pass (including legitimate `baseline-guard` skips) |
| **1** | `check`: a dimension regressed · `baseline-guard`: the snapshot regressed vs `<ref>` (and isn't covered by an accepted-debt trailer) · `record`: refused a worse value without `--accept-worse` |
| **2** | cannot measure/compare honestly: bad config, missing/malformed snapshot, tool crash, timeout, unknown command, … |

The **1-vs-2 split is load-bearing**: `1` means "measured fine, code got worse";
`2` means "could not measure honestly" and must never read as a pass.

## Configuration

`pawl.yaml` lists dimensions; each is a **built-in**, a **custom command**, or a
projection from a named analyzer (exactly one of `builtin` / `command` / `source`).
Named analyzers run once per invocation and let several dimensions filter one
complete report — ESLint, Oxlint, or SARIF, or a `lines` analyzer that turns any
tool's `path:line: message` output into the same findings via one regex, so a
tool pawl has never heard of gets the same shared-scan treatment.

```yaml
snapshot: "pawl.snapshot.json"   # optional, relative to this file

analyzers:
  - id: "frontend-lint"
    builtin: "eslint"
    # Optional representative --print-config probes. Every filtered rule must
    # be enabled by at least one probe; a clean zero remains a valid zero.
    verify:
      - "npx eslint --print-config src/probe.ts"
    options:
      command: "npx eslint src --format json --no-inline-config"
      min_files: 1

dimensions:
  - id: "cognitive-complexity"   # required, unique
    title: "Functions over cc 15" # required, human-readable
    direction: "lower-is-better" # required: lower-is-better | higher-is-better
    gate: "per-file-count"       # optional: total (default) | per-file-count | per-key-value
    tolerance: 0                 # optional, absolute slack in the worse direction
    source: "frontend-lint"      # reuse the analyzer output …
    options:
      rules: ["sonarjs/cognitive-complexity"]

  - id: "coverage"
    title: "Line coverage"
    direction: "higher-is-better"
    gate: "per-key-value"
    tolerance: 1
    command: "./scripts/coverage.sh"   # … or a custom command
```

## Built-in adapters

Three tiers. **Primitives** are Go-native (zero dependencies). **Tool adapters**
run an analyzer *you* invoke and parse its machine output. **Report-format
ingest** reads the ecosystem's standard machine formats, so any tool that emits
one becomes a dimension with no wrapper — pawl owns the format knowledge, you own
the tool setup.

| builtin | tier | measures | typical gate |
|---|---|---|---|
| `file-length` | primitive | files whose line count exceeds `threshold` | `total` (pair with `per-key-value` to block growth of already-long files) |
| `file-bytes` | primitive | files whose byte size exceeds `threshold` | `total` (same pairing as `file-length`) |
| `pattern-count` | primitive | regexp matches (suppressions, escape hatches: `as any`, `//nolint`, `try!`) | `per-file-count` |
| `eslint` | adapter | counted ESLint messages (optionally filtered by `rules`) | `per-file-count` |
| `oxlint` | adapter | Oxlint native JSON diagnostics, filtered by rule/severity and shareable across dimensions | `per-file-count` |
| `jscpd` | adapter | duplicated lines from a jscpd JSON report | `total` |
| `swift-complexity` | adapter | Swift **cognitive** complexity offenders (what SwiftLint can't) | `per-file-count` |
| `json-value` | adapter | one number out of any tool's JSON (coverage %, passing tests, type-coverage) — the home of `higher-is-better` | `per-key-value` |
| `lines` | adapter | any tool's `path:line: message` output, via one regex — one scan, several rule/level-filtered dimensions (named analyzers only) | `per-file-count` |
| `sarif` | ingest | findings in a SARIF log (CodeQL, Semgrep, …), filtered by rule/level | `per-file-count` |
| `junit` | ingest | failing / passing / total tests from a JUnit XML report | `total` |
| `coverage` | ingest | line/branch/function coverage % from lcov or cobertura | `total` |

Report-format producers exit non-zero to signal findings/failures, so the ingest
builtins gate on a **parseable report**, not the exit code. A coverage floor is
one dimension away from any lcov report your CI already produces:

```yaml
  - id: "line-coverage"
    title: "Line coverage %"
    direction: "higher-is-better"
    builtin: "coverage"
    options: { file: "coverage/lcov.info", format: "lcov" }
```

Each builtin's exact options, exit-code handling, and breakdown shape are in
[SPEC.md § Built-in adapters](./spec/adapters/builtins.md) and [§ Report-format ingest](./spec/adapters/ingest.md).
Copy-paste configs for all of them — plus SARIF/JUnit/coverage/complexity/
duplication — are in the [recipe cookbook](./RECIPES.md).

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

- id: legacy-lint
  command: "my-linter --format concise"
  direction: "lower-is-better"
  gate: "per-file-count"
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):'   # value = matches; path/line → breakdown
```

Also `extract: number` (stdout is one number) and `extract: { json_path: "a.b.c" }`
(read one number from the command's stdout JSON). Same honesty rule: a non-zero
exit, or output that can't be extracted, is a measurement failure (exit 2). With
`regex`, every non-empty line must match, so a mistyped pattern can't report a
silent zero. Tools whose "findings found" exit code is non-zero should use a
report-format builtin (for example the shared SARIF recipe) rather than hiding
every failure with `|| true`. Details in
[SPEC.md § Declarative extract layer](./spec/adapters/extract.md).

## Gate modes

The scalar total is **always** checked (with `tolerance`). A per-breakdown check
on top stops a localized regression from hiding behind a net-zero total (file A
improves, file B worsens, total unchanged):

- **`total`** — scalar only. (Growing an already-long file shouldn't fail; only a
  new file crossing the limit, which moves the total, should.)
- **`per-file-count`** — summed offender *count* per file may not rise.
  Breakdown values are finding multiplicity at each `path:line`, so two rules or
  matches on one line count as two. Moving the same number of findings within a
  file still does not trip the gate.
- **`per-key-value`** — every baseline key's *value* may not worsen (with
  tolerance). New keys and removed keys are ignored. Ideal for per-package
  coverage / type-coverage.

`tolerance` is absolute slack in the worse direction; a value exactly at the
boundary passes. `higher-is-better` and `lower-is-better` flip the comparison.

Pick the gate for the shape: `per-file-count` is the strong net-zero defense for
*issue-count* dimensions (offenders coming and going); `per-key-value` fits
*stable-key numeric* dimensions (a fixed key set whose values move) and only
guards keys already in the baseline. Neither is a universal net-zero proof — the
[SPEC](./spec/engine/comparison.md#gate-modes) spells out the edges.

## Locking in one win (`record --only`)

A full `pawl record` re-measures and re-blesses **every** dimension at once — so
locking in a win on one metric silently accepts whatever the others currently
measure, including a regression elsewhere you did not mean to accept. When you
improve one dimension, lock in just that one:

```console
$ pawl record --only line-coverage
📸 re-recorded line-coverage; preserved 4 other metric(s) → pawl.snapshot.json
```

Only the listed dimensions are re-measured; every other metric keeps its
committed value verbatim. A broken adapter on an unrelated dimension doesn't
block the win either — it isn't run at all.

Preserved rows show `current —` / `preserved`; JSON uses
`measurement_state:"preserved"`, `current:null`, and `snapshot_value`. A partial
record cannot emit Code Climate because that format has no way to distinguish a
preserved finding from current evidence.

## Accepting debt (`--accept-worse`, `--dry-run`)

`record` (with or without `--only`) refuses to write a dimension worse than the
committed baseline — a full record must not silently bless a regression in a
dimension nobody was looking at:

```console
$ pawl record
❌ record refused — 1 dimension(s) would be recorded worse than the committed baseline:
  • complexity  12 → 15
re-run with --accept-worse to record this as accepted debt, or fix the regression first.
```

`--accept-worse` writes it anyway and prints the trailer to add to your commit
message, so `baseline-guard` can tell the regression was deliberate rather than
hand-edited:

```console
$ pawl record --accept-worse
📸 snapshot written to pawl.snapshot.json
⚠️  recorded worse — add these trailers to the commit that includes the snapshot:
    Pawl-Accept: complexity 15
baseline-guard treats a worsened metric without a matching trailer as unauthorized.
```

`baseline-guard <ref>` reads `Pawl-Accept: <id> <value>` trailers from every
commit in `<ref>..HEAD`; a regression whose current value is no worse than the
declared one is printed as an accepted notice instead of a violation. The
trailer lives in git history, not in the snapshot file, so the anti-tamper
comparison itself is unchanged.

`--dry-run` previews the table (and, combined with `--accept-worse`, the
trailer lines) without writing anything; its exit code matches what a real
record would do.

## Watching the trend (`pawl trend`)

The snapshot is a committed file, so its git history **is** the metric history.
`pawl trend [<id>]` renders it — fully local, no cloud, no account:

```console
$ pawl trend line-coverage
line-coverage  (higher-is-better, %)
  6952777  2026-07-13  71.2  —
  165cabc  2026-07-14  72.4  +1.2
  0142640  2026-07-16  74.0  +1.6
```

Omit the id to see every dimension. `--limit <n>` caps the rows (default 20,
`0` = all); `--format json` emits machine-readable points for your own plotting.

## CI integration

pawl is a single binary — any CI can run it. pawl's own CI dogfoods the whole
loop: it pipes `go test` through go-junit-report and gates its passing-test
floor with the `junit` builtin, so the ingest path runs on real report output
on every commit ([pawl.yaml](./pawl.yaml)). Two common wirings:

### GitHub Actions

The action installs the binary; on its own that is all it does:

```yaml
- uses: tiangong-dev/pawl@v0.6.0   # puts the pawl binary on PATH — no Go/Node
  with:
    version: v0.6.0                # optional; defaults to the latest release
- run: pawl check
- run: pawl baseline-guard origin/${{ github.base_ref }}   # on PRs
```

Pass `command` and the action also runs the gate and, on a pull request, upserts
one sticky comment with the result (rendered from the `--format json` verdict) —
no bespoke `github-script` step:

```yaml
# ... your pre-steps here, e.g. build exec adapters ...
- uses: tiangong-dev/pawl@v0.6.0
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

### Anything else

pawl is a single binary — run `npx -y @pawl-tools/cli@0.6.0 check` (or download
the release binary) in any CI.

### Anti-tamper

`pawl check` only proves the snapshot on disk matches a fresh measurement — not
that the snapshot's history is honest. `pawl baseline-guard <base-ref>` compares
the committed snapshot against the PR's base branch and fails if it was
hand-edited to a worse value. Run it on PRs alongside `check`.

## Diff-scoped checking

`pawl check --since <ref>` keeps the full gate but **only fails on regressions
introduced by lines changed in the working tree since `<ref>`** — pre-existing
debt on untouched lines is exempted, so a large legacy baseline doesn't block
every PR while new code still can't regress. Uncommitted and untracked edits
count (so a local agent loop that has not committed yet still gates new debt).
It still needs the snapshot (it's the gate narrowed to new code, not a
standalone scanner).

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
under-reports a changed line. Details in [SPEC.md](./spec/commands/since.md#diff-scoped-checking).

## Using pawl from an AI agent

pawl is a gate, not an analyzer. The loop is one command:

1. `pawl check --format json` (optionally `--only <id>`, `--since HEAD` before commit).
2. Read `failure_class`, `next_action`, and `watch`. Do not grow `near`/`over` files; `headroom` is what is left. Watch does not change the exit code.
3. On `status: better`, run the metric's `next_action` (`pawl record --only <id>`), not a full `record`.
4. Exit 1 / `failure_class: regression` → fix code. Exit 2 / `could-not-measure` → fix the environment (`error`, `failed_metrics`); do not invent numbers. CI: full `pawl check` (never `--only`).
5. A verdict with a top-level `only` array covered just those dimensions — exit 0 on it is not a green full gate.
6. A metric with an `artifact` block read that file off disk. `generated: false` plus a large `age_seconds` means the number describes an old report, not the current tree — regenerate it before trusting or recording the value. The age never changes the exit code.

Copy-paste dimensions that catch AI-shaped debt are in [RECIPES.md](./RECIPES.md#ai-generated-debt).

## Scope boundary

pawl is a **quality gate + honesty guard, not a code analyzer** — it never parses a
language. Line counting and regexp matching are Go-native because they need no
grammar; everything requiring real language semantics (complexity, type escapes)
is delegated to that language's own best analyzer through an adapter, so the gate
agrees with what developers already see in their IDE. Rationale in
[SPEC.md § Scope boundary](./spec/engine/scope.md).

Pawl owns the verdict, not the toolchain: projects install, pin, configure, and
invoke analyzers. Automatic tool/runtime installation, language detection,
formatting, autofix, a plugin marketplace, and cross-run analyzer caching are
deliberate non-goals. Tool-specific adapters are added only when a standard
report cannot preserve an honest measurement.

## License

MIT — see [LICENSE](./LICENSE).
