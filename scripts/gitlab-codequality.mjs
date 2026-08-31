// Converts a pawl `--format json` verdict (schema_version 2) into a GitLab
// Code Quality report — a JSON array of {description, check_name, fingerprint,
// severity, location} that the merge-request widget consumes. This is a
// converter, not a pawl output format: GitLab is not a pawl target the way
// GitHub Actions is, so it lives here instead of behind a CLI flag.
//
// A could-not-measure verdict (exit 2) still produces an issue — an empty
// array would make the widget read as clean on exactly the runs where the
// gate could not run at all, which is the one thing pawl's fail-closed
// contract must never let a converter undo. A verdict scoped by `--only` is
// rejected outright for the same reason: AGENTS.md is explicit that a
// narrowed run's exit 0 "is not a green full gate" and must not be passed on
// as the gate's answer — converting it would do exactly that.
//
// Run: node gitlab-codequality.mjs <pawl-verdict.json> [--anchor=<path>] [--config-dir=<path>]
// See README.md § GitLab and other systems without a native pawl widget.

import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const DEFAULT_ANCHOR = 'pawl.yaml'

// JSON.stringify delimits and escapes each part, so no separator choice can
// let two distinct inputs collide (a literal separator character embedded in
// an id/path would defeat a plain join).
const fingerprint = (parts) => createHash('sha1').update(JSON.stringify(parts)).digest('hex')

// pawl reports every path relative to the config directory (see
// spec/commands/since.md's "breakdown keys are config-dir-relative"), which is
// the repo root only when the config was not loaded from a subdirectory via
// `-c`/`--config`. `configDir` closes that gap; GitLab also requires no "./"
// prefix, which `path.posix.normalize` removes.
const toRepoPath = (p, configDir) => path.posix.normalize(path.posix.join(configDir || '.', p))

// GitLab line numbers are 1-based. pawl's own numbering can be 0 ("no source
// line", e.g. a SARIF result with no region — spec/adapters/ingest.md) or
// absent (`null`, for a total-gate regression); `??` alone would let 0 through
// as an invalid location instead of falling back.
const toLine = (n) => (Number.isInteger(n) && n > 0 ? n : 1)

// toCodeQualityIssues turns one parsed verdict into a GitLab Code Quality
// array. `anchor` is the file a whole-gate (could-not-measure, or a `total`
// regression with no path of its own) issue is attached to, since GitLab
// requires every issue to have a location; `configDir` rebases config-relative
// paths (including `anchor`) onto the repository root.
export function toCodeQualityIssues(report, { anchor = DEFAULT_ANCHOR, configDir = '' } = {}) {
  if (!report || report.schema_version !== 2) {
    throw new Error(`unsupported verdict: schema_version ${report && report.schema_version} (expected 2)`)
  }
  // schema_version 2 also covers `record` (spec/engine/verdict.md), and a
  // `record --accept-worse` verdict can exit 0 while `metrics[].regressions`
  // stays fully populated for the dimension it just authorized — confirmed by
  // running it: {"command":"record","exit_code":0,"accepted_worse":[...],
  // "metrics":[{"status":"worse","regressions":[{"suppressed":false,...}]}]}.
  // Converting that would report a deliberately-accepted, non-gating outcome
  // as a GitLab failure. Only `check` is pawl's CI gate; require it.
  if (report.command !== 'check') {
    throw new Error(
      `verdict is from \`pawl ${report.command}\`, not \`pawl check\` — only a check verdict is the CI ` +
        "gate's answer; a `record` verdict can carry regressions that were deliberately accepted " +
        '(`--accept-worse`) rather than gated on. Run `pawl check --format json` and convert that instead.',
    )
  }
  if (report.only) {
    throw new Error(
      `verdict is scoped to --only [${report.only.join(', ')}] — that is not the full gate; ` +
        'convert a `pawl check` run with no --only instead',
    )
  }

  // Checked against exit_code, not just failure_class: a verdict that says
  // exit 2 but omits/misreports failure_class would otherwise fall through to
  // the loop below and could convert to an empty array — a broken gate
  // reading as clean, the one outcome this converter must never produce even
  // when the input itself violates the verdict contract.
  if (report.exit_code === 2 || report.failure_class === 'could-not-measure') {
    const ids = report.failed_metrics && report.failed_metrics.length ? report.failed_metrics : [null]
    const error = report.error || `verdict reports exit_code 2 with no error message (failure_class: ${report.failure_class ?? 'missing'})`
    return ids.map((id) => ({
      description: id ? `pawl could not measure "${id}": ${error}` : `pawl could not measure: ${error}`,
      check_name: id ? `pawl:${id}` : 'pawl',
      fingerprint: fingerprint(['could-not-measure', id ?? error]),
      severity: 'blocker',
      location: { path: toRepoPath(anchor, configDir), lines: { begin: 1 } },
    }))
  }

  const issues = []
  for (const metric of report.metrics || []) {
    // Every unsuppressed regression becomes its own issue, `total` included
    // even alongside per-file/per-key detail: a `per-file-count` scalar can
    // rise by more than any per-file breakdown shows, because SARIF/Oxlint
    // findings with no source location count toward the scalar but are
    // omitted from the breakdown entirely (spec/adapters/ingest.md,
    // spec/adapters/builtins.md) — so `total` is not provably redundant with
    // the detail the way it looked at first. A duplicate-looking issue is the
    // safe failure mode here; silently folding it away risks under-reporting
    // a genuine regression, which pawl's fail-closed contract does not allow.
    for (const r of (metric.regressions || []).filter((r) => !r.suppressed)) {
      issues.push({
        description: `${metric.title}: ${r.message}`,
        check_name: metric.id,
        fingerprint: fingerprint([metric.id, r.kind, r.key ?? 'total']),
        severity: 'major',
        location: {
          path: toRepoPath(r.path ?? anchor, configDir),
          lines: { begin: toLine(r.line) },
        },
      })
    }
  }
  // internal/pawl/report.go's hasLiveRegression is exactly what sets exit_code
  // 1, so a genuine regression verdict always has at least one unsuppressed
  // regression somewhere in `metrics`. Zero issues here on a nonzero exit
  // means the input violated that contract (or this converter has a bug) —
  // fail loud rather than hand GitLab a report that reads as clean.
  if (report.exit_code === 1 && issues.length === 0) {
    throw new Error('verdict reports exit_code 1 (a regression) but no metric carries an unsuppressed regression')
  }
  return issues
}

export function main(argv) {
  const positional = []
  const opts = {}
  for (const arg of argv) {
    const m = /^--(anchor|config-dir)=(.*)$/.exec(arg)
    if (m) opts[m[1] === 'anchor' ? 'anchor' : 'configDir'] = m[2]
    else positional.push(arg)
  }
  const [reportPath] = positional
  if (!reportPath) {
    console.error('usage: node gitlab-codequality.mjs <pawl-verdict.json> [--anchor=<path>] [--config-dir=<path>]')
    return 2
  }
  const report = JSON.parse(readFileSync(reportPath, 'utf8'))
  const issues = toCodeQualityIssues(report, opts)
  process.stdout.write(JSON.stringify(issues, null, 2) + '\n')
  return 0
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  // Set exitCode and let the event loop drain instead of process.exit()-ing
  // immediately: stdout is a pipe in CI (`node ... | ...` or captured by a
  // job runner), where writes are async — an immediate exit can cut off a
  // large report mid-write before it reaches the reader.
  process.exitCode = main(process.argv.slice(2))
}
