Part of the pawl engine contract. See [spec/README.md](../README.md).

## Diff-scoped checking

`pawl check --since <ref>` runs the normal gate — measure every dimension,
compare against the snapshot — then **scopes the verdict to lines changed since
`<ref>`**, so pre-existing debt does not block a PR while new regressions still
fail. It is the gate narrowed to new code, **not** a standalone new-code
scanner: it still requires the snapshot (missing snapshot → exit 2, like
`check`). `--since` is valid only on `check`.

**Changed-line set.** `git merge-base <ref> HEAD` (in `cfg.Dir`), then
`git diff --unified=0 --no-ext-diff --find-renames <merge-base>` (working tree vs
merge-base, so staged and unstaged edits are included) unioned with every line of
every untracked, non-ignored file from
`git ls-files --others --exclude-standard --full-name -z`.
The added (`+`) lines give `map[repo-relative path]set<new line number>`. A
`\ No newline at end of file` marker inside a hunk annotates the line before it
rather than being a line of the file, so it does not advance the new-side
counter (counting it would shift every following added line down by one and drop
the changed line out of the set). Path listings are read `-z` and each record is
taken verbatim between the NULs, blanks at either end included.
Untracked files are treated as all-added; an untracked entry that is not a
readable **regular** file (a dangling symlink, a socket, a file whose
permissions deny reading) is skipped, not an error — measurement skips
non-regular files too, so no offender can hide behind one. The diff is run with
`--src-prefix=a/ --dst-prefix=b/` and every git invocation with
`-c core.quotePath=false`, so a developer's `diff.mnemonicPrefix`,
`diff.noprefix`, `diff.srcPrefix`/`dstPrefix`, or a non-ASCII filename cannot
rewrite the paths pawl parses (a path that fails to parse would empty the
added-line set and silently exempt every new offender). In CI the working tree matches HEAD,
so this is the same set as the committed range `<merge-base>..HEAD`. Locally it
is what an agent that has not committed yet actually changed. Breakdown keys are
config-dir-relative, git paths are repo-toplevel-relative, so keys are converted
to repo-relative via `cfg.Dir`'s position under `git rev-parse --show-toplevel`
before intersecting. Failure to resolve the ref, compute a merge-base (e.g. a
shallow clone with no common ancestor), or run the diff / `ls-files` is a
measurement-style failure → exit 2 (never a silent "nothing changed").

**Renames.** `--find-renames` is pinned for the same reason as the header
prefixes: whether git reports a rename as a rename or as a delete-plus-add
otherwise follows the developer's `diff.renames`, and the add form counts every
line of the moved file as new debt, so the same commit would pass on one machine
and fail on another. A detected rename is attributed to its new path: a pure
rename contributes no added lines at all, and a rename with edits contributes
only the lines genuinely added to the moved file. The old path is not a deletion
when git pairs it with the new one.

**Unstaged `mv`.** A plain `mv` that is not `git add`ed leaves the new path
untracked, so git cannot pair it with the deleted old path: every line of the
moved file is unioned in as all-added and its pre-existing findings read as new
debt. pawl cannot write the index, so when a `--since` run unions in at least one
untracked file while the same diff shows at least one deleted file, it prints one
advisory line on stderr naming the cause and the fixes:

```
pawl: --since: 1 untracked file(s) count as all-new while 1 file(s) were deleted; if this is a rename, run `git add -N <paths>` or `git mv` so renames are detected
```

That line is progress output: `-q` silences it, it never appears on stdout, and
it never changes the exit code or a metric value. It is a heuristic (an unrelated
untracked file beside an unrelated deletion also triggers it), phrased as such —
it says "if this is a rename".

**Scoping rule, per dimension.** The 1-vs-2 exit split is preserved (measurement
failures are still exit 2).

Only `per-file-count` is diff-scoped; the others are enforced in full. This
keeps `--since` **exactly the full-mode verdict, narrowed to added lines** —
never inventing a regression full mode wouldn't raise, never dropping one it
would.

- **`total` and `per-key-value` dimensions** — enforced **at full strength** (the
  normal verdict, unscoped). A scalar total has no line to attribute; a
  `per-key-value` scalar is not a sum of its breakdown and its gate ignores new
  keys, so scoping it would diverge from full mode. Both are *not* silently
  exempted — the output lists them as "enforced in full". A `per-file-count`
  dimension with **no breakdown** is treated the same way.
- **`per-file-count` dimension (with a breakdown)** — its scalar is the count /
  sum of a `path:line` breakdown, so every contributor to a regression has a
  line and can be scoped. The verdict is **re-derived from the breakdown against
  the added lines**: a key is a *worse* offender when it is new, or when its
  value grew on an already-present key (a line edited to carry more offenders) —
  direction-agnostic, exactly what moves the full-mode count/scalar. A worse key
  is a **live** regression if its `"path:line"` lies on an added line, and
  `suppressed` (exempted) if on an unchanged line.
  - A worse key that is **not line-addressable** (`line` 0, no line, or a
    file-only key) cannot be proven pre-existing, so it is counted **live**
    (conservative — when in doubt, gate) and noted as an unscopeable offender.
  - The scalar total is **not** re-counted here (the per-key pass accounts for
    every contributor), but if it regressed it is still **listed** as a
    `suppressed` regression so the JSON stays a faithful record that the total
    moved.

**Line-based approximation.** Scoping is by line number, not content hash, so it
inherits `git diff`'s notion of a changed line (as do reviewdog, diff-cover, and
Sonar's clean-as-you-code). Two consequences: a pre-existing offender whose line
content is unchanged but shifts position is tracked by git as context and is
**not** flagged (ordinary code motion doesn't trip the gate); but an offender on
a line whose content genuinely changed is flagged even if it "morally" moved
there — pawl cannot tell "moved" from "new" without hashing. This errs on the
safe side (it never under-reports a changed line) and is why `--since` is
line-precise rather than the full mode's per-file key count. It also assumes the
adapter's breakdown fully accounts for its scalar; a deliberately partial
breakdown could hide a scalar rise the per-key pass never sees.

**Output.** Text mode prints a banner naming the mode, the `<ref>`, the resolved
merge-base short SHA, the dimensions scoped vs. enforced-in-full, and the count
of pre-existing regressions exempted. `--format json` sets `mode: "since"`,
`since: "<ref>"`, and carries `suppressed: true` on each exempted regression.
The unstaged-`mv` advisory described above goes to stderr in both formats, so
stdout stays the single verdict object either way.
The exit code is 1 iff any live (non-suppressed) regression remains — including
a full-strength `total` regression — else 0.

