Part of the pawl engine contract. See [spec/README.md](../README.md).

## `check --base <git-ref>`

`pawl check --base <git-ref>` measures the current working tree and compares the
result with the snapshot committed at `<git-ref>`. It does not read or require
the working tree's snapshot. Editing or deleting that file therefore cannot
lower this invocation's comparison baseline.

- The ref must resolve to a commit, and that commit must contain a valid
  snapshot at the path selected by the current `pawl.yaml`. An invalid ref,
  missing snapshot, malformed JSON, or invalid snapshot shape is an honest
  comparison failure (exit 2), never a skip or a pass.
- Pawl runs each configured dimension once. `--current <path|->` supplies those
  same current measurements without running the dimensions again. The caller
  is responsible for producing and protecting that document.
- `--only <ids>` measures and compares only the named dimensions. A dimension
  configured now but absent from the ref snapshot is new and has no regression
  to compare yet. A dimension removed from the current config is no longer
  measured; its old ref value is not part of this invocation.
- For a configured dimension present in the ref snapshot, a non-empty
  definition fingerprint that differs from the current config makes the
  comparison incomparable (exit 2). `Pawl-Redefine` does not waive this check.
  Legacy snapshots without a fingerprint remain comparable under the current
  check configuration.
- A worse result fails (exit 1). `Pawl-Accept` trailers are not read and cannot
  waive a regression. To accept debt, update the snapshot on a trusted base
  branch through review, then compare against that ref. A definition change
  likewise needs to be recorded on a trusted base after review.
- JSON verdicts include `base_ref` with the supplied ref on both successful and
  exit-2 measurement verdicts. It is omitted when `--base` was not used; this
  adds an optional field to schema version 2.
- `--base` cannot be combined with `--since`; `--since` uses a separate Git diff
  to narrow findings and mixing the two reference points could hide a result.

This mode trusts the Git ref and its snapshot, plus the current Pawl binary,
configuration, measurement commands, CI workflow, and any supplied `--current`
document. A pull request can change its config, commands, or workflow, so this
is not protection against an arbitrary malicious change to those inputs. Keep
the CI job and toolchain trusted and review changes to them.
