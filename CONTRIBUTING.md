# Contributing to pawl

pawl is a language-agnostic anti-regression quality gate. This repository uses pawl on its own Go source, so `pawl check` must pass on every pull request.

## Before you start

- The behavioral contract lives in [`spec/`](spec/README.md); root [`SPEC.md`](SPEC.md) is its index. Read the relevant section before changing CLI behavior, snapshot format, comparison semantics, or output, and update the contract with the implementation. The contract is authoritative.
- [`AGENTS.md`](AGENTS.md) describes the local pawl workflow, including scoped checks and baseline updates.

## Development setup

You need Go (see `go.mod` for the version) and Node.js 22+ for the npm package and repository scripts.

```bash
go build -o /tmp/pawl ./cmd/pawl
/tmp/pawl check   # gate this repo's own Go source
```

## Testing

Run the same checks as CI before opening a pull request:

```bash
gofmt -l .                          # must print nothing
go vet ./...
go test -v ./...
node --test npm/cli/test/*.test.js  # npm distribution tests
node --test scripts/*.test.mjs      # PR-comment rendering tests
```

A confirmed bug fix should include a regression test. Tests should verify the behavior described in `spec/`, not mirror implementation details.

## Making changes

- Keep each PR scoped to one independently verifiable change.
- If a change moves a dimension in `pawl.yaml`, run `pawl check --format json`. For a genuine improvement, use `pawl record --only <id>`; do not run a full record to update one metric.
- Do not hand-edit generated files such as `pawl.snapshot.json` and `pawl-junit.xml`.
- Match the existing style. Comments should explain non-obvious decisions.

## Submitting a PR

CI runs formatting, vetting, Go and Node tests, pawl's self-check and guard, a diff-scoped check, and npm package validation. See [`.github/workflows/ci.yml`](.github/workflows/ci.yml) for the exact commands.

The pull request description should explain what changed and why. If pawl's own metrics moved, include that result as well.

## Reporting issues

Open a GitHub issue with a minimal reproduction: ideally `pawl.yaml`, the command, the JSON verdict, and the expected behavior.
