Part of the pawl engine contract. See [spec/README.md](../README.md).

## Measurement-definition compatibility

Two numbers carrying non-empty `definition` values are comparable only when those fingerprints match. An empty fingerprint is the single legacy compatibility state at every comparison boundary: it remains numerically comparable to a fingerprinted value during migration, rather than being treated as proof of redefinition. `check` refuses a proven mismatch as could-not-measure (exit 2) before reporting the number as better or worse. A full `record` is the explicit operation that establishes new definitions; `record --only` may redefine selected dimensions but refuses to preserve an incompatible unselected metric. Legacy snapshot and supplied-measurement entries remain readable; recording a measured or supplied selected metric binds it to the current definition.

`guard` skips numeric comparison for a redefined metric and reports the definition change for review. `trend` marks the boundary as `redefined` instead of calculating a delta across unlike scales.

## Comparison semantics

### worse / better

`tolerance` is absolute slack in the worse direction. A value exactly AT the
tolerance boundary passes.

- `higher-is-better`: worse ⇔ `cur < base - tolerance`; better ⇔ `cur > base` (strict, no tolerance)
- `lower-is-better`: worse ⇔ `cur > base + tolerance`; better ⇔ `cur < base`

### Gate modes

The scalar total is ALWAYS checked (with tolerance). The per-file / per-key check
on top stops a localized regression from hiding behind a net-zero total (file A
improves, file B worsens, total unchanged).

- **`total`** — scalar only.
- **`per-file-count`** — offender count per file may not rise. Offender count is
  the sum of breakdown values grouped by the path part of the final numeric
  `:<line>` suffix; a key without such a suffix is file-only. Values carry
  finding multiplicity, so several rules or regexp matches on one line remain
  several offenders. Moving the same multiplicity within a file is still
  ignored. A file present only in current regresses from 0. Tolerance does not
  apply to per-file counts (only to the scalar).
- **`per-key-value`** — every key of the BASELINE breakdown must not worsen
  (with tolerance, same `worse` predicate). Keys missing from the current
  breakdown are ignored (removal is legitimate); keys new in current are ignored
  (they had no baseline).

**Known limits (choose the gate accordingly).** `per-file-count` is the strong
net-zero defense for *issue-count* dimensions: swapping a fix in file A for a new
offender in file B fails because B's per-file count rose. It is deliberately
lenient *within* one file — if a file drops one offender and gains another, its
count is unchanged and the gate passes (offenders moving inside a file is
expected churn, not regression). `per-key-value` only guards keys that exist in
the baseline: a brand-new key elsewhere that a deletion nets to zero on the total
is not caught by the per-key check (only the scalar total would, and only if it
rose). And because keys are `"path:line"`, a pure line-number shift changes a
key's identity — so `per-key-value` is a fit for **stable-key numeric**
dimensions (a fixed set of keys whose *values* move), while `per-file-count` is
the fit for **issue-count** dimensions (offenders coming and going). Neither gate
is a total net-zero proof; they are targeted defenses for their intended shape.

Regression detail lines (exact formats, `<n>` in minimal decimal notation):

- scalar: `total <base> → <cur>`
- per-file-count: `<file>  <baseCount> → <curCount>` (two spaces)
- per-key-value: `<key>  <baseValue> → <curValue>` (two spaces)

A dimension present in config but absent from the snapshot is `new` — it cannot
regress. (It enters the gate at the next `record`.)

### Orphaned metrics

A snapshot metric whose id matches no configured dimension is an **orphan**:
`check`/`diff` refuse to run (exit 2, message lists the sorted orphan ids).
Deleting a dimension must also drop it from the snapshot (re-`record`), so a
regression can't hide behind a vanished measurement.

