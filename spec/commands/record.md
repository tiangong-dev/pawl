Part of the pawl engine contract. See [spec/README.md](../README.md).

## Partial record (`--only`)

`pawl record --only <id>[,<id>…]` re-measures **only** the named dimensions and writes a snapshot that keeps every other metric's committed value untouched.

This exists because a full `record` re-accepts *every* dimension's current value as the new baseline, all at once. Locking in a win on one dimension would otherwise silently accept whatever the others currently measure, including a regression you weren't looking at. `--only` is the narrow version: the improved dimension moves, the rest of the baseline stays exactly where it was.

The rules:

- Valid on `record` and `check`. On any other command it is a usage error (exit 2). So is an empty list (`--only ""` / `--only ,`).
- On `check`, only the listed dimensions are measured and compared; an unlisted dimension's regression or broken adapter does not affect the verdict. The snapshot orphan check still uses the **full** config, so a stale extra metric cannot hide behind `--only`. CI should keep running a full `pawl check`.
- Every listed id must be a configured dimension id. An unknown id is exit 2 (naming the id), before anything is measured or written.
- Requires an existing, well-formed snapshot. "Preserve the rest" is meaningless without a baseline, so a missing or malformed snapshot is exit 2 (naming the problem). Run a full `pawl record` first.
- Only the listed dimensions are measured. A broken adapter on an unrelated dimension does not block locking in the win; that is the point of the flag. The written snapshot is the freshly measured listed dimensions plus, for every other configured dimension, its metric copied verbatim from the existing snapshot.
- A metric whose dimension is no longer configured (an orphan) is dropped, exactly as a full `record` would drop it. `--only` never writes an orphan back.
- A configured dimension that is neither listed nor present in the existing snapshot stays absent. It remains "new" until a full record, or an `--only` that names it.
- Text output shows preserved metrics with `current —`, delta `—`, and status `preserved`. JSON uses `measurement_state:"preserved"`, `current:null`, and `snapshot_value`; measured metrics use `measurement_state:"measured"`.

One interaction with fingerprints: a full record also establishes `definition` fingerprints for new or changed measurement definitions, treating their old values as incomparable rather than better or worse. A partial record can do that for a selected dimension, but **refuses** when an unselected configured dimension's fingerprint changed. Preserving that old value would knowingly write a snapshot the next check cannot compare, so after reviewing a multi-dimension definition change, use a full `pawl record`.

## Accepted debt (`--dry-run`, `--accept-worse`)

By default, `record` refuses to write a snapshot that would move any dimension backwards against the committed baseline. Without that refusal, running `record` to lock in one win could silently accept a regression in a dimension you weren't watching. Accepting debt is an explicit act, not a side effect of locking in a gain.

- **Default (neither flag given):** if any measured dimension regressed against the committed baseline (per its own gate mode, the same predicate `check`'s exit code uses — a per-file-count or per-key-value regression that leaves the scalar unchanged still counts), record writes **nothing** and exits 1. Text output prints the table, then, for each regressed dimension, the same gate-aware detail lines `check`'s `❌ regressions:` block prints. A bare scalar `base → current` would misrepresent a per-file regression that left the scalar unchanged or even improved, so it is not what you get. The output points at `--accept-worse`. `--format json` renders the normal record verdict with `exit_code: 1` and every regressed metric's `status: "worse"`.
- **`--accept-worse`:** authorizes the write. Every dimension that would have been refused is written as-is, and text output additionally prints one `Pawl-Accept: <id> <value>` line per accepted dimension (`<value>` is the dimension's scalar total, matching what `guard`'s own violation check compares). That line is a trailer: add it to the commit that includes the snapshot change so `guard` (below) can recognize the debt as deliberate. `--format json` additionally sets `accepted_worse` on the verdict — see [§ Machine-readable output](../engine/verdict.md#machine-readable-output) — so an automated caller can build the same trailer without re-deriving it from `metrics[].status`.
- **`--dry-run`:** previews what record would do — the same table, plus which dimensions would change (`id base→current`, `id new→current` for a dimension with no prior baseline, or `id current (breakdown changed)` when the scalar held but the per-file/per-key breakdown didn't; a net-zero-scalar regression still writes different bytes) — and writes **nothing**, regardless of `--accept-worse`. The exit code matches what a real record would produce: 1 if it would have been refused, 0 otherwise. Note the order: **the refusal check runs before `--dry-run` is considered**, so `record --dry-run` alone on a regression refuses exactly like a real record; it does not preview past the refusal. Combined with `--accept-worse`, it previews the `Pawl-Accept` trailer lines without writing them. `--format json` sets `dry_run: true` on the verdict unconditionally under `--dry-run`, including on a refusal, so a caller can tell a `--dry-run` refusal apart from a real one that hit the same exit code.
- Both flags apply identically under `--only`, scoped to the listed dimensions. A preserved (unlisted) dimension is copied verbatim from the baseline and can never be "worse".

### `Pawl-Accept` trailers and `guard`

`guard <ref>` (see [§ guard](guard.md#guard)) reads every commit message in `<ref>..HEAD` for lines of the form `Pawl-Accept: <id> <value>`. A metric that regressed against `<ref>` is downgraded from a violation to an accepted notice when both of these hold:

1. at least one trailer names that metric's id, **and**
2. the metric's current value is no worse (per its recorded direction and tolerance) than the **worst** value declared across all matching trailers in range. Multiple trailers for the same id accumulate; the gate honors the most debt any of them declared.

Edge cases, each chosen so a sloppy or hostile history can't quietly widen the gate:

- A trailer for an id the snapshot doesn't regress on is unused.
- A trailer line that fails to parse (no id, or a non-numeric value) is skipped. A malformed trailer must never disable the gate.
- Trailers outside `<ref>..HEAD` (already on `<ref>`, or on an unrelated branch) are not read and do not count.
- `git log <ref>..HEAD` failing (say, a shallow clone missing the needed history) is a measurement failure: exit 2, not a silent "no trailers found".

The comparison is on values, not strings. The trailer stays valid even if the committed number shifts slightly between when it was written and when CI re-derives it (a rebase re-running the same `--accept-worse` record with a marginally different measurement), as long as it is no worse than what was declared.

Nothing about `--accept-worse` is written into `pawl.snapshot.json` itself, so `guard`'s anti-tamper comparison (`base` vs `pr`, both read straight off disk) works exactly as before. The trailer lives in git history, which is what `guard` audits.
