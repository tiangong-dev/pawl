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
- A metric that worsened (per its recorded `direction`, default
  `lower-is-better` if missing; slack = its recorded `tolerance`) is a
  violation **unless** a `Pawl-Accept` trailer in `<ref>..HEAD` covers it (see
  [§ Accepted debt](record.md#accepted-debt---dry-run---accept-worse)), in which case it is printed as an
  accepted-regression notice instead. Any remaining violation → line
  `<id>: <base> → <cur>`, exit 1.
- Otherwise: consistency message, exit 0.

A metric present only in the working tree (newly added dimension) is ignored.

