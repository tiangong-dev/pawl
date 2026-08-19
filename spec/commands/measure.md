Part of the pawl engine contract. See [spec/README.md](../README.md).

## `measure`

`pawl measure` runs every configured dimension and writes the resulting
**measurement document** to stdout. It reads no snapshot, renders no verdict,
and writes no file.

- Output is the snapshot format, byte for byte — the same bytes
  [`record`](record.md) would write. There is no second format, because a
  snapshot *is* a measurement someone decided to keep, so
  `pawl measure > pawl.snapshot.json` means exactly what it looks like.
- `--only <id>[,<id>…]` scopes the document to those dimensions.
- `--format` is a usage error (exit 2): the document is the output.
- Exit 0 when every dimension in scope measured. Exit 2 on any measurement
  failure, with the usual `measuring <id> failed: …` diagnostic on stderr and
  nothing on stdout — a partial document must never be mistaken for a complete
  one.
- Progress lines (`  measuring <id>…`) go to stderr, so stdout stays a clean
  document for a pipe.

## `--current`

`check` and `record` accept `--current <path>` (or `--current -` for stdin).
The named measurement document supplies the current numbers, and **the
dimensions do not run**.

Everything else is unchanged: `check` compares against the snapshot and exits 1
on a regression, `record` applies the same accepted-debt refusal, `--only`
scopes the same way. `--current` changes where the numbers come from, never what
the gate does with them.

- A document missing any dimension in scope is a **measurement failure** naming
  it (exit 2, `failure_class: "could-not-measure"`, the id in
  `failed_metrics`), never a quietly narrower run. Entries the config does not
  declare are ignored, so a document from a wider scope still works.
- The document is read and validated **before** the snapshot is read or any
  dimension runs, so a malformed or missing document costs nothing.
- Metrics carry no `artifact` block on a `--current` run: pawl opened no report
  this time, and claiming provenance for a file it never read would be worse
  than reporting none.
- On any command other than `check`/`record`, `--current` is a usage error
  (exit 2).

### Why this exists

Measuring twice is how a gate ends up recording numbers nobody verified. The
inner loop `check` and the `record` that follows it are two separate
measurement passes over a tree that may have changed in between — and when a
dimension reads a report off disk (JUnit, SARIF, coverage), they can even read
two different builds. One `measure`, then a `check` and a `record` that both
consume it, removes the gap:

```sh
pawl measure > .pawl/current.json
pawl check  --current .pawl/current.json          # the verdict
pawl record --only <id> --current .pawl/current.json  # lock in exactly what was verified
```

`--current` trusts its input. That trust boundary is deliberate and it is the
caller's to hold: produce the document in the same job that consumes it. It is
not a way around [`baseline-guard`](guard.md), which compares committed
snapshots and is unaffected.
