# pawl

A language-agnostic anti-regression quality ratchet: dimensions measure numbers,
`record` snapshots them, `check` fails CI when any dimension regresses, and
`baseline-guard <ref>` catches hand-edited snapshots. Adapters are plain commands
printing `{"value": n}` JSON — any language's tooling plugs in.

```bash
pawl record                     # establish / update the baseline
pawl check                      # CI gate: exit 1 on regression
pawl diff                       # compare without gating
pawl baseline-guard origin/main # anti-tamper gate for PRs
```

## Install

```bash
npm install -D @pawl-tools/cli                     # prebuilt binary via npm
go install github.com/tiangong-dev/pawl/cmd/pawl@latest   # or build from source
curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

The `install.sh` one-liner uses whatever is already on the machine — Go, else
npm, else a direct binary download. Prebuilt binaries cover darwin/linux/win32
on x64/arm64 (`npm/build.js` cross-compiles and generates the platform packages;
`npm/publish.js` publishes them).

### GitHub Actions

```yaml
- uses: tiangong-dev/pawl@v0.1.2        # puts the pawl binary on PATH (no Go/Node)
  with:
    version: v0.1.2              # optional; defaults to the latest release
- run: pawl check
```

Configure dimensions in `pawl.yaml`. Built-ins come in two tiers: zero-dependency
primitives (`file-length`, `pattern-count`) and tool adapters that parse a
well-known analyzer's machine output while the project supplies the invocation
(`eslint`, `jscpd`, `swift-complexity` for Swift cognitive complexity, and
`json-value` — read any number out of a tool's JSON, e.g. a coverage percentage).

pawl never parses a language itself — anything needing real language semantics is
delegated to that language's own analyzer through an adapter. See the *Scope
boundary* in [SPEC.md](./SPEC.md), which also holds the full behavioral contract
(exit codes, exec adapter protocol, gate modes, snapshot format).
