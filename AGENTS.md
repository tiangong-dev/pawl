# Agent notes for this repo

pawl is a quality gate. Do not treat a non-zero exit as “go rewrite the code”
until you know *why*.

## Loop

1. Inner loop: `pawl check --format json` (optionally `--only <id>` for a
   primitive, `--since HEAD` before commit). Read `failure_class`,
   `next_action`, and `watch`.
2. `watch` entries are `near` / `over` files this invocation touched, with
   remaining `headroom`. Do not grow them. Watch does not change the exit code.
3. `"status": "better"` → run that metric's `next_action`
   (`pawl record --only <id>`). Never a full `pawl record` to lock one win.
4. `failure_class: "regression"` / exit 1 → fix source.
   `failure_class: "could-not-measure"` / exit 2 → fix the environment
   (`error`, `failed_metrics`). Do not invent snapshot numbers.
   CI must run a full `pawl check` with no `--only`.
5. A verdict carrying a top-level `only` array measured just those dimensions —
   exit 0 there is not a green full gate. Report it as what it is, and do not
   pass it on as the gate's answer.
6. A metric carrying `artifact` read that file off disk. `generated: false` with
   a large `age_seconds` means the number describes that old report, not the
   current tree — regenerate the artifact before you trust the value or record
   it. The age never changes the exit code; judging it is your job.

## This repository

This repo dogfoods pawl (`pawl.yaml`). After changing Go that moves a dimension,
run `pawl check --format json` and, on a genuine improvement, `pawl record --only <id>`.

The behavioral law is [`spec/`](spec/README.md) (start at `spec/README.md`). Do
not read all of it — pick the file for the change. Root `SPEC.md` is only the
index. `AGENTS.md` is the operator loop, not a copy of the contract.
