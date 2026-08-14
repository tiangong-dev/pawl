Part of the pawl engine contract. See [spec/README.md](../README.md).

## Output

`record`/`check`/`diff` print a table to stdout:

```
metric        baseline    current       Δ  status
---------------------------------------------------
file-length          3          4      +1  ❌ worse
```

- Δ: `new` when no baseline, `±0`, else signed delta rounded to 2 decimals.
- status: `🆕 new` (no baseline) / `❌ worse` (regressed, including per-file/per-key
  regressions that leave the scalar unchanged — the gate's verdict overrides the
  scalar-only status) / `✅ within tolerance` (worse by scalar but inside declared
  slack) / `🎉 better` / `✅ same`.
- After the table, `check`/`diff` print a `❌ regressions:` block (dimension id,
  title, detail lines) when any, and a `🎉 improved: <ids>` block plus a hint to
  run `pawl record --only <ids>` when any dimension's scalar strictly improved.
- `check` under `GITHUB_ACTIONS=…` (env var set non-empty) additionally prints
  `::notice::pawl improved: <ids> — run \`pawl record --only <ids>\` to lock in the gains.`
  so an unrecorded win surfaces on the PR itself.
- `check` under `GITHUB_ACTIONS` also prints a GitHub `::error::` annotation per
  regression, so violations land inline on the PR diff. A `per-file-count`
  dimension emits one `::error file=<path>,line=<line>,title=pawl: <id>::…` per
  new offender key in a file whose offender count rose; `per-key-value` one per
  worsened key; a `total`-gate (or detail-less) regression a single file-less
  `::error title=pawl: <id>::<title> regressed: <base> → <cur>`. Annotations are
  additive — the human-readable `❌ regressions:` block always prints too.
- `record` prints the table, then either writes the snapshot and prints
  `📸 snapshot written to <path>`, or — per [§ Accepted debt](../commands/record.md#accepted-debt---dry-run---accept-worse) —
  refuses with a `❌ record refused` block (nothing written), or, under
  `--dry-run`, previews with a `🔍 dry run` line (nothing written either way).

## Machine-readable output

`--format json` makes `record`/`check`/`diff` print **exactly one JSON object**
to stdout and nothing else (no table, no `❌ regressions:` block, no emoji, and
— because stdout must stay pure JSON — no `::error::`/`::notice::` GitHub
annotations). stderr (the `measuring <id>…` progress lines) is unchanged. The
exit code is identical to text mode. This is pawl's own stable verdict schema —
deliberately not rdjson, which cannot express a scalar total, an improvement, or
a `--since` suppression. `record --dry-run` sets `dry_run: true` on the object
(omitted, not `false`, on every other invocation — including on a refusal, so
a `--dry-run` refusal is distinguishable from a real one at the same exit
code); it is otherwise the normal record verdict shape, exit code included.
`record --accept-worse` (with or without `--dry-run`) sets `accepted_worse` to
one `{"id", "value"}` entry per dimension written (or, under `--dry-run`,
would-be-written) worse than the committed baseline — `value` is the same
number the text-mode `Pawl-Accept: <id> <value>` trailer hint prints. Omitted
when nothing was accepted as worse.

```json
{
  "schema_version": 2,
  "command": "check",
  "mode": "full",
  "since": null,
  "exit_code": 1,
  "failure_class": "regression",
  "metrics": [
    {
      "id": "eslint",
      "title": "ESLint issues",
      "direction": "lower-is-better",
      "gate": "per-file-count",
      "unit": "issues",
      "base": 10,
      "current": 12,
      "measurement_state": "measured",
      "status": "worse",
      "improved": false,
      "regressions": [
        {
          "kind": "per-file-count",
          "key": "src/a.ts:5",
          "path": "src/a.ts",
          "line": 5,
          "base": 0,
          "current": 1,
          "message": "src/a.ts  0 → 1",
          "suppressed": false
        }
      ]
    }
  ]
}
```

- Top level: `schema_version` (int, currently `2`), `command`
  (`record`/`check`/`diff`), `mode` (`full` or `since`), `since` (the ref string
  when `mode` is `since`, else `null`), `dry_run` (bool, present only when
  `true` — `record --dry-run`), `accepted_worse` (array, present only when
  non-empty — `record --accept-worse`; see [§ Accepted
  debt](../commands/record.md#accepted-debt---dry-run---accept-worse)), `exit_code` (the process exit code),
  `failure_class` (`"regression"` when `exit_code` is 1, `"could-not-measure"`
  when `exit_code` is 2, **omitted** on a pass), optional `error` / `failed_metrics`
  (exit 2 only), optional `watch` (`check`/`diff` only), and
  `metrics` — an array **sorted by `id`**.
- Exit 2 with `--format json` on `record`/`check`/`diff` still prints the human
  diagnostic on stderr, and also emits the verdict object on stdout:
  `failure_class` is `"could-not-measure"`, `error` is that diagnostic, and
  `failed_metrics` lists the dimension ids that failed to measure (omitted when
  the run never reached a dimension — missing snapshot, orphan, `--since` git
  failure). `metrics` is `[]`. Usage errors (unknown command, mis-scoped flag)
  and a missing/invalid config still print stderr only — there is no gate in
  progress. Text and codeclimate modes are unchanged (stderr only on exit 2).
- `watch` (`check`/`diff` only, omitted when empty or on `record`): files this
  invocation **touched** that are `near` or `over` a `file-length` / `file-bytes`
  threshold. Touched means the working tree vs `HEAD` (tracked edits + untracked),
  or vs the `--since` merge-base when that flag is set. A clean tree (CI) omits
  the field. Each entry is `{id, path, kind, value, threshold, headroom, status}`
  where `headroom` is `threshold - value` (negative when `over`) and `near` is
  strictly above `0.9 * threshold` and not over — the same cut `rank` uses.
  Sorted by `headroom` ascending, then `path`, then `id`. One entry per
  `path`+`kind` (when two dimensions share a builtin, the first in config order
  is kept). Watch never changes
  `exit_code`. A git failure computing the touched set omits `watch` rather than
  failing the gate.
- Each metric: `id`, `title`, `direction`, `gate` (`total` when unset), `unit`,
  `base` (the baseline value, `null` when the dimension is new), `current` (the
  measured value, or `null` for a preserved partial-record metric),
  `measurement_state` (`measured`/`preserved`), optional `snapshot_value` — for
  a `preserved` metric, the value already on disk (present regardless of
  whether this invocation writes, since a preserved value is unchanged either
  way); for a `measured` metric, the value actually written, so it is present
  only when the write happened (omitted on a refusal or under `--dry-run`,
  where `current` alone still reports what was measured) — `status`
  (`new`/`worse`/`within-tolerance`/`better`/`same`/`preserved`,
  the emoji-free form of the table status), `improved` (bool — scalar strictly
  improved), `next_action` (on `check`/`diff` only, and only when `improved` is
  true: the exact command `pawl record --only <id>`; omitted otherwise), and
  `regressions` (array, empty when none).
- Each regression: `kind` (`total`/`per-file-count`/`per-key-value`), `key`
  (the breakdown key, `null` for a `total` regression), `path` and `line`
  (parsed from `key`; both `null` for `total`, `line` `null` when the key has no
  numeric line), `base` and `current` (the two compared numbers — for `total`
  the scalar values, for `per-file-count` the contributing location key's
  finding counts (the message also carries the file totals), for
  `per-key-value` the key's values), `message` (a human-readable detail line),
  and `suppressed` (bool — `true` only in `--since` mode when this regression was
  exempted for falling outside the changed lines; always `false` in `full` mode).
- Regressions within a metric are ordered as in text mode; `suppressed` ones are
  still listed (so the JSON is a faithful record) but do not affect `exit_code`.

Schema 2 is a report-only migration: the committed snapshot format is unchanged,
so upgrading does not require re-recording. Consumers of schema 1 must accept the
nullable `current` field and the new `measurement_state` / optional
`snapshot_value` fields before upgrading. The corrected `per-file-count`
comparison can surface a regression that older versions missed when multiple
findings shared one breakdown key; that is an intentional tightening, not a
snapshot incompatibility.

## Code Quality output

`--format codeclimate` makes `record`/`check`/`diff` print a **Code Climate
issue array** (the format GitLab renders as its Merge Request *Code Quality*
widget and inline diff annotations) to stdout and nothing else — no table, no
emoji, no GitHub annotations. stderr (the `measuring <id>…` progress lines) and
the exit code are unchanged from text mode, so `pawl check --format codeclimate`
still exits 1 on a regression while writing the artifact.

This is **findings mode**, not the baseline delta: it lists *every current
offender* the gate can locate to a file and line, and leaves the new-vs-fixed
comparison to GitLab (which diffs the report on the MR branch against the report
on the target branch). The output is therefore independent of the snapshot — the
same command on any branch reports that branch's current offenders.

Only **per-file-count** dimensions produce findings: their breakdown is keyed by
`path:line`, so each offender has a location. `total` and `per-key-value`
dimensions carry no per-line location (a total has no attributable line; a
per-key-value key is an arbitrary label, not a source position), so they emit no
findings — their gate is still enforced through the exit code. A `check` whose
config has no per-file-count offenders prints `[]` (a valid empty report).

```json
[
  {
    "description": "TODO / FIXME markers",
    "check_name": "todo-markers",
    "fingerprint": "8f14e45fceea167a5a36dedd4bea2543",
    "severity": "major",
    "location": {
      "path": "src/a.ts",
      "lines": { "begin": 5 }
    }
  }
]
```

- One entry per per-file-count breakdown key. `check_name` is the dimension `id`;
  `description` is the dimension `title` (with ` ×<n>` appended when the offender
  count at that location exceeds 1). `severity` is always `major` (pawl has no
  per-issue severity). `location.path` and `location.lines.begin` come from the
  breakdown key `path:line`, split on the **last** colon (so a path that itself
  contains a colon keeps its line). A key with no colon, a non-numeric line, or a
  line ≤ 0 (the adapter's "unknown line") is skipped — Code Quality entries need
  a real line.
- `fingerprint` is a stable hex digest of `check_name`, `path`, and `line` — not
  the `description`, which carries the run-varying `×n` count. Identical
  locations yield an identical fingerprint across runs, so GitLab tracks the same
  issue across commits and never treats a re-measured offender as new.
- Entries are sorted by `path`, then `line`, then `check_name` — a deterministic
  array for reproducible artifacts and diffs.

