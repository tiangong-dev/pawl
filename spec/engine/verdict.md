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
- `check`/`diff --only` print, right after the table, an
  `ℹ️  <n> dimension(s) not measured this run (--only scope): <ids>` line naming
  the dimensions `--only` left out — the text-mode counterpart of the JSON
  `excluded` field below. Omitted when `--only` was not given.
- `check` (not `diff`/`record`) prints, on stderr, a
  `hint: for machine-readable output, use \`pawl check --format json\`` line
  whenever stdout is not a terminal (a pipe, a file, a subprocess capture) and
  `--format json` was not already requested — the case a script or an agent
  loop actually runs in. A real terminal, or `--format json`, suppresses it.

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
  when `mode` is `since`, else `null`), `only` (array, present only under
  `--only`: the measured dimension ids, deduplicated and sorted — see below),
  `excluded` (array, present whenever `only` is: the configured dimension ids
  `--only` left *unmeasured*, sorted — see below),
  `dry_run` (bool, present only when
  `true` — `record --dry-run`), `accepted_worse` (array, present only when
  non-empty — `record --accept-worse`; see [§ Accepted
  debt](../commands/record.md#accepted-debt---dry-run---accept-worse)), `exit_code` (the process exit code),
  `failure_class` (`"regression"` when `exit_code` is 1, `"could-not-measure"`
  when `exit_code` is 2, **omitted** on a pass), optional `error` / `failed_metrics`
  (exit 2 only), optional `watch` (`check`/`diff` only), and
  `metrics` — an array **sorted by `id`**.
- `only` makes a narrowed run self-describing. `check --only a` measures one
  dimension and can exit 0 while the full gate would exit 1, so the object must
  say what it covered: a consumer that only sees `metrics` cannot tell a green
  subset from a green full gate once the JSON leaves the invocation that
  produced it. It is present on `record`/`check`/`diff` whenever `--only` was
  given (on `record` alongside the per-metric `measurement_state`), omitted
  otherwise, and it survives onto the exit-2 object below. `mode` is unaffected:
  `--only` narrows *which dimensions* are measured, `--since` narrows *which
  lines* count, and the two compose.
- `excluded` is `only`'s complement against the full config: every dimension id
  configured but not passed to `--only`, sorted. It exists so a `--only` run
  stays legible on its own — an agent that narrows scope to fix one broken
  dimension has no other way to notice the rest of the gate still exists once
  this object is the only thing it reads. Present on `check`/`diff` whenever
  `only` is, omitted on a full run, and it survives onto the exit-2 object the
  same way `only` does — a could-not-measure verdict still reports what was
  left out, not just what failed. `record --only` does not set it: its
  existing per-metric `measurement_state: "preserved"` already names every
  dimension outside the scope, so a second field would repeat the same fact.
- Exit 2 with `--format json` on `record`/`check`/`diff` still prints the human
  diagnostic on stderr, and also emits the verdict object on stdout:
  `failure_class` is `"could-not-measure"`, `error` is that diagnostic, and
  `failed_metrics` lists the dimension ids that failed to measure (omitted when
  no dimension is what failed — missing snapshot, orphan, `--since` git failure,
  or a snapshot that could not be written after a clean measurement).
  `metrics` is `[]`. The object still describes the invocation's
  coverage: `mode`/`since` reflect `--since` (a `--since` run that could not
  resolve its ref is still `mode: "since"`), and `only` the narrowed dimension
  set. Usage errors (unknown command, mis-scoped flag)
  and a missing/invalid config still print stderr only — there is no gate in
  progress. Text mode is unchanged (stderr only on exit 2).
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
  true: the exact command `pawl record --only <id>`; omitted otherwise),
  `regressions` (array, empty when none), and optional `artifact`.
- `artifact` (present only when the measurement read a file off disk) is
  `{path, modified, age_seconds, generated}`: the path as configured (relative
  to the config dir), the file's mtime as an RFC 3339 timestamp, its age in
  whole seconds at measurement time, and whether this invocation's own
  `command` produced it. It is normally provenance only. A dimension may set
  `artifact_max_age` to make an old, non-generated artifact a measurement
  failure (exit 2); generated artifacts are fresh by construction. Without
  that option, age cannot change `exit_code` and no age is an error.
  A dimension configured with `file:` and no `command:` measures whatever is on
  disk. A report written last week parses exactly like one written a second ago
  and yields a number the verdict presents like any other — so the gate can be
  measuring the past while reading as current. pawl cannot tell from the bytes
  whether the artifact matches the working tree, so it reports what it does
  know and leaves the judgment to the reader. When `generated` is true the file
  is this run's own output and the age is ~0 by construction; the field is
  still present so a consumer never has to know which builtins write their own
  reports. `age_seconds` may be negative if the artifact carries a future mtime
  (clock skew, an extracted archive) — the sign is preserved rather than
  clamped, because the oddity is the signal. Artifact metadata is verdict-only:
  it never enters the committed snapshot, which must move only when a measured
  number moves.
  Text mode prints the same fact to **stderr**, next to the `measuring <id>…`
  progress lines and only when `generated` is false; stdout stays the table.
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

`artifact` is an additive optional field: it appears only where it has
something to say, so a schema-2 consumer that ignores unknown keys is
unaffected and the version does not move for it.

Schema 2 is a report-only migration: the committed snapshot format is unchanged,
so upgrading does not require re-recording. Consumers of schema 1 must accept the
nullable `current` field and the new `measurement_state` / optional
`snapshot_value` fields before upgrading. The corrected `per-file-count`
comparison can surface a regression that older versions missed when multiple
findings shared one breakdown key; that is an intentional tightening, not a
snapshot incompatibility.
