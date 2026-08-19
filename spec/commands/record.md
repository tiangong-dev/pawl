Part of the pawl engine contract. See [spec/README.md](../README.md).

## Partial record (`--only`)

`pawl record --only <id>[,<id>…]` re-measures **only** the named dimensions and
writes a snapshot that keeps every other metric's committed value untouched. It
is the surgical counterpart to a full `record`: a full record re-measures and
re-blesses *every* dimension at once, so locking in a win on one dimension also
silently accepts whatever the others currently measure — including a regression
elsewhere you did not mean to bless. `--only` locks in the improved dimension
alone, so the committed baseline for the rest stays exactly where it was.

- Valid on `record` and `check`; on any other command it is a usage
  error (exit 2). An empty list (`--only ""` / `--only ,`) is a usage error
  (exit 2).
- On `check`, only the listed dimensions are measured and compared.
  An unlisted dimension's regression or broken adapter does not affect the
  verdict. The snapshot orphan check still uses the **full** config, so a stale
  extra metric cannot hide behind `--only`. CI should keep running a full
  `pawl check`.
- Every listed id must be a configured dimension id; an unknown id → exit 2
  (naming the id), before anything is measured or written.
- Requires an existing, **well-formed** snapshot to preserve: a missing snapshot,
  or one with shape errors, → exit 2 (naming the problem). "Preserve the rest"
  is meaningless without a baseline — run a full `pawl record` first.
- **Only the listed dimensions are measured.** An unrelated dimension whose
  adapter is currently broken therefore does not block locking in the win (that
  is the point). The written snapshot = the freshly measured listed dimensions,
  plus, for every **other configured** dimension, its metric copied verbatim
  from the existing snapshot.
- A metric in the existing snapshot whose dimension is no longer configured (an
  orphan) is dropped, exactly as a full `record` drops it — `--only` never writes
  an orphan back.
- A configured dimension that is neither listed nor present in the existing
  snapshot stays absent (it remains "new" until a full record, or an `--only`
  that names it).
- Text output shows preserved metrics with `current —`, delta `—`, and status
  `preserved`. JSON uses `measurement_state:"preserved"`, `current:null`, and
  `snapshot_value`; measured metrics use `measurement_state:"measured"`.

## Accepted debt (`--dry-run`, `--accept-worse`)

A full `record` (or `record --only`) that would write a dimension worse than
the value currently committed at `SnapshotPath` is refused by default —
otherwise the person running `record` to lock in one win can silently bless a
regression in a dimension they weren't looking at. Accepting debt is an
explicit act, not a side effect of locking in a gain.

- **Default (neither flag given):** if any measured dimension regressed
  against the committed baseline (per its own gate mode — the same predicate
  `check`'s exit code uses, so a per-file-count or per-key-value regression
  that leaves the scalar unchanged still counts), record writes **nothing**
  and exits 1. Text output prints the table, then — for each regressed
  dimension — the same gate-aware detail lines `check`'s `❌ regressions:`
  block would print (not just a scalar `base → current`, since that alone
  would misrepresent a per-file-count/per-key-value regression that left the
  scalar unchanged or even improved), and points at `--accept-worse`.
  `--format json` renders the normal record verdict with `exit_code: 1` and
  every regressed metric's `status: "worse"`.
- **`--accept-worse`:** authorizes the write. Every dimension that would have
  been refused is written as-is, and text output additionally prints one
  `Pawl-Accept: <id> <value>` line per accepted dimension (`<value>` is the
  dimension's scalar total, matching what `baseline-guard`'s own violation
  check compares) — the trailer to add to the commit that includes the
  snapshot change, so `baseline-guard` (see below) can recognize it as
  deliberate. `--format json` additionally sets `accepted_worse` on the
  verdict — see [§ Machine-readable output](../engine/verdict.md#machine-readable-output) — so an
  automated caller can build the same trailer without re-deriving it from
  `metrics[].status`.
- **`--dry-run`:** previews what record would do — the same table, plus a
  summary of which dimensions would change (`id base→current`, `id
  new→current` for a dimension with no prior baseline, or `id current
  (breakdown changed)` when the scalar held but the per-file/per-key
  breakdown didn't — a net-zero-scalar regression still writes different
  bytes) — and writes **nothing**, regardless of `--accept-worse`. Its exit
  code matches what a real record would produce: 1 if it would have been
  refused (worse dimensions present and `--accept-worse` not given), 0
  otherwise — **the refusal check runs before `--dry-run` is considered**, so
  `record --dry-run` alone on a regression refuses exactly like a real record
  would, it does not preview past the refusal. Combined with
  `--accept-worse`, it previews the `Pawl-Accept` trailer lines without
  writing them. `--format json` sets `dry_run: true` on the verdict
  unconditionally under `--dry-run` — including on a refusal, so a caller can
  tell a `--dry-run` refusal apart from a real one that hit the same exit
  code.
- Both flags apply identically under `--only`, scoped to the listed
  dimensions only — a preserved (unlisted) dimension is copied verbatim from
  the baseline and can never be "worse".

### `Pawl-Accept` trailers and `baseline-guard`

`baseline-guard <ref>` (see [§ baseline-guard](guard.md#baseline-guard)) reads every
commit message in `<ref>..HEAD` for lines of the form `Pawl-Accept: <id>
<value>`. A metric that regressed against `<ref>` is downgraded from a
violation to an accepted notice when:

1. at least one trailer names that metric's id, **and**
2. the metric's current value is no worse (per its recorded direction and
   tolerance) than the **worst** value declared across all matching trailers
   in range — multiple trailers for the same id accumulate; the gate honors
   the most debt any of them declared.

A trailer for an id the working tree's snapshot doesn't regress on is simply
unused. A trailer line that fails to parse (no id, or a non-numeric value) is
skipped — a malformed trailer must never silently disable the gate. Trailers
outside `<ref>..HEAD` (already on `<ref>`, or on an unrelated branch) are not
read and do not count. `git log <ref>..HEAD` failing (e.g. a shallow clone
missing the needed history) is a measurement failure: exit 2, not a silent
"no trailers found".

The accepted-value comparison, not string equality, means the trailer stays
valid even if the exact committed number shifts slightly between when it was
written and when CI re-derives it (e.g. a rebase re-running the same
`--accept-worse` record with a marginally different measurement) — it only
has to not be worse than what was declared.

The snapshot file itself is never touched by acceptance: nothing about
`--accept-worse` is written into `pawl.snapshot.json`, so `baseline-guard`'s
anti-tamper comparison (`base` vs `pr`, both read straight off disk) is
exactly as before. The trailer lives in git history, which is what
`baseline-guard` audits.

