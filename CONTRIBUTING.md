# Contributing to pawl

pawl is a language-agnostic anti-regression quality gate. This repo also
dogfoods itself: pawl gates its own Go source (`pawl.yaml`), so `pawl check`
must pass on every PR just like it does for any other user of the tool.

## Before you start

- The behavioral contract lives in [`spec/`](spec/README.md) (start at
  `spec/README.md`, indexed by root [`SPEC.md`](SPEC.md)). For any change that
  touches CLI behavior, snapshot format, comparison semantics, or output, read
  the relevant spec file first and update it alongside the code — the spec is
  authoritative, not a description of what the Go implementation happens to do.
- [`AGENTS.md`](AGENTS.md) documents the operator loop for running pawl against
  itself while iterating (`pawl check --format json`, `--only <id>`, etc.).

## Development setup

Requires Go (see `go.mod` for the version) and Node.js 22+ (for the npm
distribution and the PR-comment rendering scripts).

```bash
go build -o /tmp/pawl ./cmd/pawl
/tmp/pawl check   # dogfood: gate this repo's own Go source
```

## Testing

Run the same checks CI runs before opening a PR:

```bash
gofmt -l .                          # must print nothing
go vet ./...
go test -v ./...
node --test npm/cli/test/*.test.js  # npm distribution tests
node --test scripts/*.test.mjs      # PR-comment rendering tests
```

Any confirmed bug fix should come with a regression test. Tests should verify
behavior described in `spec/`, not just mirror the implementation.

## Making changes

- Keep each PR scoped to one independently verifiable change.
- If you add or change a dimension in `pawl.yaml`, or the change moves a
  number pawl measures about this repo, run `pawl check --format json` and,
  on a genuine improvement, `pawl record --only <id>` to update the baseline.
  Never run a full `pawl record` to lock in a single win.
- Generated files (e.g. `pawl.snapshot.json`, `pawl-junit.xml`) are produced by
  tooling — don't hand-edit them. `pawl baseline-guard` catches hand-edited
  baselines.
- Match existing code style; comments should explain non-obvious *why*, not
  restate *what* the code does.

## Submitting a PR

CI runs `gofmt`, `go vet`, the Go and Node test suites, a self-check
(`pawl check` on this repo), `pawl baseline-guard origin/main`, a diff-scoped
self-check (`pawl check --since origin/main`), and validates the npm release
path (build + dry-run publish). All of these should pass locally before you
open a PR — see [`.github/workflows/ci.yml`](.github/workflows/ci.yml) for the
exact steps.

## Reporting issues

Please open a GitHub issue with a minimal reproduction — ideally a `pawl.yaml`
plus the command and output you expected versus what you got.
