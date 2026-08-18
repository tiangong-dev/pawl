Part of the pawl engine contract. See [spec/README.md](../README.md).

## Config — `pawl.yaml`

```yaml
# Optional. Snapshot path, resolved relative to the config file's directory.
snapshot: "pawl.snapshot.json"

analyzers:
  - id: "frontend-lint"         # required, unique across analyzers
    builtin: "eslint"           # eslint, oxlint or sarif
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
`valid_exit_codes` on a `builtin`/`source` dimension or holding a non-integer or
out-of-range entry, zero dimensions, unparseable YAML, config file not found.

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
- Acquisition options live on the analyzer; selectors live on referencing
  dimensions. Unknown options/selectors fail validation instead of being
  silently ignored.
- `record --only` executes only analyzers needed by the selected dimensions. If
  several selected dimensions share one analyzer, it still executes once.

