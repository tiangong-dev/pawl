<!-- pawl:begin -->
## Quality gate (pawl)

`pawl check` is this repo's regression gate. Run it yourself before you call a
task done — not the underlying tool, not `wc -l`, not a prediction from reading
the diff. Those can agree with the gate by luck and still leave you asserting
an outcome you never measured.

1. Inner loop: `pawl check --format json` (add `--only <id>` while iterating on
   one dimension, `--since HEAD` before a commit). Read `failure_class`,
   `next_action`, and `watch`.
2. Exit 1 / `failure_class: "regression"` → fix the source. Exit 2 /
   `failure_class: "could-not-measure"` → fix the environment named in `error`
   and `failed_metrics` (a missing tool, a stale or absent report file). Never
   write a snapshot number by hand to make either go away.
3. `"status": "better"` → run that metric's `next_action`, which is
   `pawl record --only <id>`. Never a full `pawl record` to lock in one win: it
   silently re-blesses every dimension you did not touch.
4. A verdict carrying a top-level `only` array measured just those dimensions,
   and `excluded` names the ones it skipped. Exit 0 there is not a green gate —
   say what was actually measured. CI runs a full `pawl check` with no `--only`.
5. `watch` entries are files this run touched that sit near or over a
   threshold, with the `headroom` left. They never change the exit code, so
   judging them is your job: do not spend the last of a file's headroom without
   saying so.
6. A metric carrying an `artifact` block read that file off disk.
   `generated: false` with a large `age_seconds` means the number describes an
   old report, not the current tree — regenerate it before you trust or record
   the value.

Never hand-edit the snapshot file. Do not loosen a threshold, an exclude list,
or a dimension in `pawl.yaml` to turn a red check green unless the task is
explicitly about the gate's configuration — and say so out loud when it is.
<!-- pawl:end -->
