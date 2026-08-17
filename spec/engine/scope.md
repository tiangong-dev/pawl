Part of the pawl engine contract. See [spec/README.md](../README.md).

## Scope boundary (design decision)

pawl is a **quality gate + honesty guard**, not a code analyzer. It never parses a
language. The line is drawn by the two-tier built-in design:

- **Primitives** (`file-length`, `file-bytes`, `pattern-count`) are Go-native only because they
  are both trivial *and* genuinely language-agnostic — counting lines, bytes, and regexp
  matches needs no grammar.
- Anything requiring real language semantics (complexity, type escapes, dead
  code) is delegated to that language's own best analyzer through a **tool
  adapter** — pawl parses the analyzer's machine output, never the source.

This is deliberate. Complexity — cognitive complexity especially — is strongly
language-specific, and a home-grown metric would disagree with the ecosystem
tool developers already see in their IDE, so the gate would lose trust.
Reimplementing a multi-language AST metric engine is exactly what qlty already
is; if cross-language *uniform* metrics are ever wanted, the move is to adopt
qlty as one more adapter (pinned version, telemetry off), not to rebuild it. So
pawl stays a small, verifiable binary over a clean adapter contract.

### Integration acceptance criteria

Pawl owns measurement integrity, normalization, committed baselines, comparison,
and verdicts. The project owns analyzer installation, version pinning,
configuration, and invocation. In particular, Pawl does **not** grow into a tool
manager: automatic language detection, analyzer/runtime installation, a plugin
marketplace, cross-run result caching, formatting, autofix, and bundled
security-scanner orchestration are explicit non-goals.

Standard report protocols are preferred: SARIF for findings, JUnit for tests,
LCOV/Cobertura for coverage, and the exec JSON contract for arbitrary metrics.
A tool-specific adapter is accepted only when all of these are true:

1. A standard report or declarative adapter cannot preserve an honest verdict.
2. The adapter closes a concrete false-pass risk such as ambiguous exit codes,
   incomplete scan evidence, stale output, or a misspelled selector becoming
   zero.
3. The project still supplies and pins the tool; Pawl only acquires and decodes
   its machine output.
4. The normalized result remains the same scalar plus optional finding
   breakdown consumed by the generic gate engine.

Shared analyzers are an execution boundary inside one Pawl invocation, not the
start of a general plugin runtime: they may prevent duplicate scans, but do not
install tools, discover languages, or cache results across runs.

