Part of the pawl engine contract. See [spec/README.md](../README.md).

A point carries its snapshot metric's optional `definition` in JSON. When adjacent points carry different non-empty definitions, text output prints `redefined` in the delta column instead of subtracting values from different measurement scales. A missing definition is the legacy compatibility state: legacy points omit the field and remain numerically comparable during migration.

## Trend

`pawl trend [<id>]` reconstructs each metric's value over time from the
**committed snapshot file's own git history** — no cloud, no external store, no
account. It is **read-only and never measures**: it walks `git log` for the
snapshot path, parses the snapshot committed at each commit that touched it, and
prints the series. This is the fully-local answer to "is this metric trending the
right way?" — the history is already in the repo.

- The snapshot's repo-relative path is resolved as `guard` does
  (`git rev-parse --show-toplevel`, then Rel). `trend` reads the config only for
  the snapshot path; it needs no measurement. Not inside a git repo, or the
  snapshot path outside the repo → exit 2.
- Commits are those from `git log --format=<sha><TAB><ISO-date> -- <path>`
  reachable from `HEAD` (newest first). If the path was **never committed** →
  exit 2 (`no committed history for <path> — commit the snapshot first`). History
  is traced at the snapshot's **current path**: renaming the snapshot file is a
  boundary (commits before the rename are not reconstructed under the old name).
- For each commit, `git show <sha>:<path>` is parsed. A commit is **skipped with
  a loud `::warning::` (under `GITHUB_ACTIONS`) / `⚠️` note** — never silently —
  when its snapshot cannot be read (`git show` fails, e.g. the commit that
  deleted the file), is **unparseable JSON**, or has an **invalid shape** (e.g. a
  metric with no numeric value). A malformed snapshot must never become a
  measured `0`, and one corrupt historical commit must not abort the whole trend.
  A commit whose (well-formed) snapshot simply lacks a given metric contributes
  no point for that metric (a gap — the dimension didn't exist yet).
- A metric's `direction` and `unit` are taken from its **most recent** appearance
  in the history (the current contract for that metric).
- `<id>` restricts output to that one metric; if it appears in **no** historical
  snapshot → exit 2 (`no metric "<id>" in the snapshot history`). With no `<id>`,
  every metric id that appears anywhere in the history is shown, sorted by id.
- `--limit <n>` (default 20) keeps only the `n` **most recent** snapshot commits;
  `--limit 0` means all. When the history is longer than the limit, a loud line
  (`showing <n> of <m> snapshots (--limit 0 for all)`) is printed — never a
  silent cap. Points are always ordered **oldest → newest** in the output, so the
  per-point Δ reads as "change from the previous point".

**Output (text).** For each metric: a header `<id>  (<direction>, <unit>)`, then
one row per kept commit, oldest first:

```
<short-sha>  <YYYY-MM-DD>  <value>  <Δ>
```

`<value>` is minimal-decimal; `<Δ>` is `—` for the first (oldest) point, else the
signed change from the previous point (`±0` when unchanged), same formatting as
the `check` table's Δ.

**Output (`--format json`).** One JSON object, `metrics` sorted by id, `points`
oldest → newest:

```json
{
  "schema_version": 1,
  "command": "trend",
  "snapshot": "pawl.snapshot.json",
  "metrics": [
    {
      "id": "file-length",
      "direction": "lower-is-better",
      "unit": "files > 500 lines",
      "points": [
        { "commit": "<full sha>", "date": "<ISO-8601>", "value": 3 }
      ]
    }
  ]
}
```

The exit code is 0 on success, 2 on any of the error cases above.

