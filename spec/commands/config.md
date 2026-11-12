Part of the pawl engine contract. See [spec/README.md](../README.md).

## Config — `pawl.yaml`

```yaml
# Optional. Snapshot path, resolved relative to the config file's directory.
snapshot: "pawl.snapshot.json"

# Optional. Exclusion globs shared by every filesystem-scanning dimension:
# appended to each such dimension's own exclude. See § Shared exclude below.
exclude: ["**/node_modules/**", "**/dist/**"]

analyzers:
  - id: "frontend-lint"         # required, unique across analyzers
    builtin: "eslint"           # eslint, oxlint, sarif or lines
    timeout: "10m"              # applies to acquisition and verification
    verify:                     # optional ESLint/Oxlint --print-config commands
      - "npx eslint --print-config src/probe.ts"
    options:
      command: "npx eslint src --format json"
      min_files: 1              # optional completeness floor

dimensions:
  - id: "nolint-count"          # required, unique across dimensions
    title: "nolint suppressions" # required, human-readable
    direction: "lower-is-better" # required: lower-is-better | higher-is-better
    gate: "per-file-count"       # optional: total (default) | per-file-count | per-key-value
    tolerance: 0.0               # optional, absolute slack in the worse direction, default 0
    timeout: "10m"               # optional Go duration, default "10m"
    artifact_max_age: "24h"      # optional freshness bound for file-backed reports
    command: "./scripts/count-nolint.sh"  # exactly one of command | builtin | source
    valid_exit_codes: [0, 1]     # optional, command dimensions only; default [0]

  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    builtin: "file-length"
    options:                     # builtin-specific options
      threshold: 500
      include: ["**/*.go"]
      exclude: ["vendor/**"]
      min_files: 1              # optional completeness floor for this native scan

  - id: "eslint-rule"
    title: "One ESLint rule"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "frontend-lint"      # filters the named analyzer's decoded findings
    options:
      rules: ["plugin/rule"]
```

Validation errors (all exit 2): missing/duplicate `id`, missing `title`, missing or
invalid `direction`, invalid `gate`, not exactly one of `command`/`builtin`/`source`, unknown
`builtin` name, invalid builtin options (bad regexp, missing `include`, …),
`extract` set on a `builtin` dimension (extract is an exec-adapter feature),
unknown `extract` form, an `extract` object with neither/both of `regex`/`json_path`,
an uncompilable `extract.regex`, an empty `extract.json_path`,
an unknown named-analyzer acquisition option or dimension selector,
invalid `artifact_max_age` (it must be a positive duration and the dimension must read a configured report file), `valid_exit_codes` on a `builtin`/`source` dimension or holding a non-integer or
out-of-range entry, zero dimensions, unparseable YAML, config file not found.

An *unknown* key is not in that list — see the next section.

### Shared `exclude`

An optional top-level `exclude: [glob…]` is the exclusion list shared by every
dimension that scans the filesystem: `file-length`, `file-bytes`, and
`pattern-count`. It is **appended** to that dimension's own `exclude`, so both
lists apply and a dimension's own globs keep working. Glob syntax and the
config-directory-relative matching rules are the same as a dimension's
`exclude`.

It reaches only the filesystem-scanning builtins. A dimension whose input is a
command (`command`/`extract`) or a named analyzer (`source`) walks no directory
and ignores the list entirely. Its purpose is the `node_modules`/`dist`/`vendor`
list a repository otherwise repeats in every dimension: write it once at the top.

The **definition fingerprint** is computed from the merged, de-duplicated
effective exclude set, so lifting a glob that was repeated in each dimension up
to the top level leaves every recorded measurement comparable — no re-baseline,
no redefined metric. A change that genuinely alters the effective set (adding,
removing, or editing a glob) changes the fingerprint, and `check` reports the
affected metrics as redefined.

### Unknown keys

A key the schema above does not define is dropped by the parser, so a typo
(`gat:` for `gate:`, `optoins:` for `options:`) silently removes the field and
the dimension falls back to its defaults. Pawl reports every such key on
stderr — at the top level, inside each `analyzers[]` entry, and inside each
`dimensions[]` entry — with the full key path (`dimensions[2].gat`) and, when a
defined key at that level is close by, a "did you mean `<key>`" suggestion. All
stray keys are listed, not just the first. Keys beginning with `x-` are
reserved for user extensions (YAML anchors, tool metadata) and are never
reported.

This is a warning: the exit code is unchanged and the run proceeds. It is not
progress, so `-q` / `--quiet` does not suppress it — a config mistake must not
be silent. Under `GITHUB_ACTIONS` each line is emitted as a `::warning::`
annotation, still on stderr; with `--format json` the warning goes to stderr
too, so stdout remains a single JSON object. The next minor release plans to
promote an unknown key to a load error (exit 2). The `x-` prefix stays valid.
The one command that already treats an unknown key as an error is
[§ validate](validate.md#validate), whose whole job is judging the config: it
exits 2 rather than measuring against a key the author probably misspelled.

YAML merge keys are not reported: `<<` (`!!merge`) is a language feature, and
an alias where a mapping or sequence is expected is skipped rather than
dereferenced. The consequence is a known limitation — a key a merge pulls in
through `<<` is not visible to the scan, so a typo carried that way is not
reported.

The accepted-key vocabulary is derived from the parser's own struct tags, so
it cannot drift from what `pawl` accepts.

### Editor schema

[`schema/pawl.schema.json`](../../schema/pawl.schema.json) (JSON Schema
draft-07, `$id`
`https://raw.githubusercontent.com/tiangong-dev/pawl/main/schema/pawl.schema.json`)
describes this file: the top-level keys, `analyzers[]`, `dimensions[]`, the
enumerated values (`direction`, `gate`, every builtin), and the options each
builtin and analyzer reads. An editor reading it completes key names, marks a
value outside an enum, and shows the meaning of the thing under the cursor.

A config opts in with a directive on its own first line — the line
[`pawl init`](init.md) writes into every scaffold:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/tiangong-dev/pawl/main/schema/pawl.schema.json
```

The URL is the schema's own `$id` on the default branch, so an editor fetches it
with no configuration; an editor that maps schemas by setting can point there
instead of editing the file.

The schema is an editing aid, never the verdict. `pawl validate` (and every
measuring command) loads the config itself, and where an editor's suggestion and
pawl disagree, pawl is authoritative — the schema can lag behind a released
pawl on a detail no test pins.

Which unknown keys it closes follows the same rule as the warning above: the
three levels pawl warns about (top level, `analyzers[]`, `dimensions[]`) plus a
named analyzer's `options`, where an unknown option is already a config error,
set `additionalProperties: false`; `^x-` stays open at every one of them.
Inside a dimension's `options` and inside the `extract` object it stays open,
because pawl reads the keys it knows there and ignores anything else — an option
name typo is not something pawl reports today.

Two things keep the file honest: the key sets and the enumerated values are
compared against the parser and the builtin registries by Go tests, so adding a
key or a builtin without updating the schema fails the build; and nothing in the
schema is a dependency of the binary — a missing or stale schema changes no
verdict.

### `valid_exit_codes`

Optional on a `command` dimension (raw exec or `extract`), and spelled the same
way as the named SARIF analyzer's acquisition option. Absent, the contract is
the historical one: exit 0 or the run is a measurement failure. Present, it is
the complete set of exit codes that count as a successful run.

It exists for tools that report findings through a non-zero exit. Appending
`|| true` to the command says something weaker and more dangerous: it accepts
*every* exit code, so a crashed tool, a missing binary, or a rejected flag
becomes a clean measurement of whatever partial output existed. Declaring the
set keeps "could not measure" and "measured zero" apart, which is the property
the whole engine is built on.

### Named analyzers

A named analyzer is the explicit sharing boundary for expensive tool scans. Pawl
acquires and decodes it once per process invocation, then each referencing
dimension applies pure `rules` / `levels` filters. Pawl never deduplicates
anonymous dimensions merely because their command strings happen to match:
timeouts, files, exit policies, decoders, and command side effects are part of
the source identity.

- `builtin: eslint`: `options.command` is required and uses ESLint's 0/1-valid,
  2+-fatal exit contract. `verify` is an optional list of commands producing
  ESLint `--print-config` JSON for representative files. Every rule selected by
  a referencing dimension must be enabled in at least one verified config or
  measurement fails; zero findings remains valid. `min_files` optionally
  requires the JSON report to contain at least that many file results.
- `builtin: oxlint`: `options.command` is required and must produce Oxlint
  `--format json` on stdout. Exit 0 and 1 are accepted only with a complete
  native report; every other exit is fatal. `verify` is an optional list of
  Oxlint `--print-config` commands. Referencing dimensions select native diagnostic
  codes such as `eslint(no-debugger)` or `unicorn(no-invalid-fetch-options)`;
  verification maps those to config keys `no-debugger` and
  `unicorn/no-invalid-fetch-options` and requires each selected rule to be
  enabled in at least one probe. Dimensions may also select `levels`
  (`error`/`warning`/`advice`). `min_files` uses the report's required
  `number_of_files`.
- `builtin: sarif`: the normal SARIF `command` / `file` acquisition contract
  applies. `min_files` optionally requires at least that many SARIF artifacts.
  `valid_exit_codes` can declare the producer's successful/finding exit codes;
  any other exit then fails even if a parseable report was written.
  `verify_rules: true` makes every filtered rule id prove its presence in the
  report's `tool.driver.rules` catalog, so a misspelling cannot become a clean
  zero. It requires at least one `rules` selector and fails if the producer
  omits that catalog. Referencing dimensions may filter `rules` and/or `levels`.
- `builtin: lines`: the tool-agnostic kind. `options.command` and
  `options.regex` are required; `options.valid_exit_codes` is accepted with the
  same meaning it has on a dimension. The regex is compiled at config load and
  applied to every non-empty output line; an unmatched line is a measurement
  failure, not a skip. Named groups `path`, `line`, `rule`, and `level` are all
  optional — `path`/`line` build the per-file breakdown, `rule`/`level` are what
  referencing dimensions select on. Level names are not checked against any
  vocabulary, because they belong to the tool.

  It refuses two options the other kinds accept, rather than approximating
  them: `min_files`, because line output reveals the files that *had* findings
  and not the files that were scanned, and `verify`/`verify_rules`, because
  there is no rule catalog in line output to verify against. Strict matching is
  the partial substitute: a pattern that stops matching because the tool changed
  its output format fails loudly instead of quietly zeroing every dimension that
  sources it.

  `lines` is a named-analyzer kind only. On a dimension it is a validation
  error naming the analyzer form — a single line-oriented dimension wants
  `command:` with `extract: regex` instead.
- Acquisition options live on the analyzer; selectors live on referencing
  dimensions. Unknown options/selectors fail validation instead of being
  silently ignored.
- `record --only` executes only analyzers needed by the selected dimensions. If
  several selected dimensions share one analyzer, it still executes once.
