Part of the pawl engine contract. See [spec/README.md](../README.md).

## guard

`pawl guard <ref>` compares the working tree's snapshot file against the
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
- A metric whose `definition` changed is reported as redefined and skipped for
  numeric comparison: values from different measurement semantics are not
  ordered. A redefined metric must be confirmed by a `Pawl-Redefine: <id>`
  trailer in `<ref>..HEAD` (one line may name several ids, separated by commas);
  without it guard prints the missing trailer and exits 1. A trailer line that
  names no id is ignored rather than treated as an error. Legacy metrics with no
  definition compare as before.
- A metric that worsened (per its recorded `direction`, default
  `lower-is-better` if missing; slack = its recorded `tolerance`) is a
  violation **unless** a `Pawl-Accept` trailer in `<ref>..HEAD` covers it (see
  [§ Accepted debt](record.md#accepted-debt---dry-run---accept-worse)), in which case it is printed as an
  accepted-regression notice instead. Any remaining violation → line
  `<id>: <base> → <cur>`, exit 1.
- A metric under a `per-file-count` gate is compared per file as well: any file
  whose summed offender count rose — including a file absent at `<ref>` — is a
  violation even when the scalar total held or improved. This is the same
  per-file predicate `check` applies (see
  [§ Gate modes](../engine/comparison.md#gate-modes)), so the scalar total's
  tolerance does not apply to the per-file counts. The violation line lists
  `file base → cur` for up to 10 files, then `and N more`. A `Pawl-Accept`
  trailer covering the metric's total accepts its per-file violations too: the
  author accepted that metric's debt, wherever inside it the count moved. Git's
  detected renames are mapped first — see
  [§ Renames in the per-file comparison](#renames-in-the-per-file-comparison).
- Otherwise: consistency message, exit 0.

A metric present only in the working tree (newly added dimension) is ignored.

### Renames in the per-file comparison

Moving a file does not change what is wrong inside it, so a per-file comparison
that read the new path as a brand-new file would fail every refactor. Before
comparing, guard maps git's detected renames, and that mapping can only re-key
base counts git attributes to the same file — it never widens what passes:

- **Detection.** One
  `git diff --find-renames --name-status -z --diff-filter=R --no-ext-diff`
  over `merge-base(<ref>, HEAD)..HEAD`. `--find-renames` is pinned so the
  repository's `diff.renames` setting cannot switch detection off, `-z` keeps
  paths with spaces and non-ASCII bytes intact, and only `R` records are read.
- **Copies are not mapped.** `-C`/`--find-copies` is not used: an added copy is
  new code with its own offenders, not a move of the original, so a copy whose
  content matches an existing file still regresses from 0.
- **1:1 only.** Each old path maps to at most one new path and each new path
  comes from at most one old path. A rename that cannot be applied that way —
  two of them claiming one destination, or a destination the base already
  records a count for — is dropped whole, and that file is compared per path as
  before.
- **Path conversion.** Rename paths are repo-relative while breakdown keys are
  relative to the config directory, which may be a subdirectory of the
  repository. A rename is mapped only when both ends are inside the config
  directory.
- **Comparison after mapping.** The mapped base count is then compared by the
  same per-file predicate as above, so a rename that also adds an offender still
  fails (`b.go 2 → 3`), and the line names the mapped origin:
  `b.go 2 → 3 (renamed from a.go)`.
- **Degradation.** If git cannot answer — a shallow clone with no merge base
  between `<ref>` and `HEAD`, or a failed diff — guard does not fail on that
  account: it prints a notice that rename detection is unavailable and a purely
  renamed file may be reported as a new offender, then compares without mapping.
  That is the stricter reading: every file is compared under its own path.

Renames reach only a `per-file-count` metric; `total` and `per-key-value`
compare exactly as they always have.

### Where guard gets a gate mode

Guard's per-file comparison resolves each metric's gate mode from the working
tree's `pawl.yaml`: a snapshot records `direction` and `tolerance` but no gate.
This is not a bypass. `gate` is part of the definition fingerprint, so relaxing
a dimension from `per-file-count` to `total` redefines it — guard then demands
the `Pawl-Redefine` trailer instead of silently comparing under the weaker
predicate — and a snapshot whose recorded definition no longer matches the
config is what `check` refuses (exit 2) as could-not-measure. A metric with no
configured dimension (an orphan) has no gate mode here and is compared on its
scalar alone.
