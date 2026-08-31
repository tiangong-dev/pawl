import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import { toCodeQualityIssues, main } from './gitlab-codequality.mjs'

const SCRIPT_PATH = fileURLToPath(new URL('./gitlab-codequality.mjs', import.meta.url))

const metric = (over) => ({
  id: 'eslint',
  title: 'ESLint issues',
  regressions: [],
  ...over,
})

const regression = (over) => ({
  kind: 'per-file-count',
  key: 'src/a.ts:5',
  path: 'src/a.ts',
  line: 5,
  message: 'src/a.ts  0 → 1',
  suppressed: false,
  ...over,
})

test('an unsupported or missing schema_version is rejected', () => {
  assert.throws(() => toCodeQualityIssues({ schema_version: 1, metrics: [] }), /schema_version 1/)
  assert.throws(() => toCodeQualityIssues(null), /schema_version/)
  assert.throws(() => toCodeQualityIssues({}), /schema_version undefined/)
})

test('a clean verdict with no regressions converts to an empty array', () => {
  const issues = toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 0, metrics: [metric()] })
  assert.deepEqual(issues, [])
})

test('an unsuppressed regression becomes one issue with the metric id, title, and message', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [metric({ regressions: [regression()] })],
  })
  assert.equal(issues.length, 1)
  assert.equal(issues[0].check_name, 'eslint')
  assert.equal(issues[0].description, 'ESLint issues: src/a.ts  0 → 1')
  assert.equal(issues[0].severity, 'major')
  assert.deepEqual(issues[0].location, { path: 'src/a.ts', lines: { begin: 5 } })
})

test('a suppressed regression is not converted', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 0,
    metrics: [metric({ regressions: [regression({ suppressed: true })] })],
  })
  assert.deepEqual(issues, [])
})

test('a total-gate regression with no path/line anchors on the default pawl.yaml at line 1', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [
      metric({
        id: 'type-coverage',
        title: 'Type coverage',
        regressions: [regression({ kind: 'total', key: null, path: null, line: null, message: '92 → 90' })],
      }),
    ],
  })
  assert.deepEqual(issues[0].location, { path: 'pawl.yaml', lines: { begin: 1 } })
})

test('a custom anchor overrides the pawl.yaml default for path-less issues', () => {
  const issues = toCodeQualityIssues(
    {
      schema_version: 2,
    command: 'check',
      exit_code: 1,
      metrics: [metric({ regressions: [regression({ path: null, line: null })] })],
    },
    { anchor: 'config/pawl.yaml' },
  )
  assert.equal(issues[0].location.path, 'config/pawl.yaml')
})

test('a leading "./" is stripped from both regression and anchor paths', () => {
  const issues = toCodeQualityIssues(
    {
      schema_version: 2,
    command: 'check',
      exit_code: 1,
      metrics: [metric({ regressions: [regression({ path: './src/a.ts' })] })],
    },
    { anchor: './pawl.yaml' },
  )
  assert.equal(issues[0].location.path, 'src/a.ts')
})

test('the fingerprint is stable across runs and differs by kind and key', () => {
  const base = regression()
  const a = toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 1, metrics: [metric({ regressions: [base] })] })
  const b = toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 1, metrics: [metric({ regressions: [base] })] })
  assert.equal(a[0].fingerprint, b[0].fingerprint)

  const differentKey = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [metric({ regressions: [regression({ key: 'src/b.ts:5', path: 'src/b.ts' })] })],
  })
  assert.notEqual(a[0].fingerprint, differentKey[0].fingerprint)
})

test('multiple metrics each contribute their own regressions', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [
      metric({ id: 'eslint', regressions: [regression()] }),
      metric({ id: 'file-length', title: 'File length', regressions: [regression({ path: 'src/b.ts', key: 'src/b.ts:0', message: 'src/b.ts new offender' })] }),
    ],
  })
  assert.deepEqual(
    issues.map((i) => i.check_name),
    ['eslint', 'file-length'],
  )
})

test('a could-not-measure verdict with failed_metrics emits one blocker issue per id', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 2,
    failure_class: 'could-not-measure',
    error: 'measuring test-coverage failed: reading coverage/coverage-summary.json: file does not exist',
    failed_metrics: ['test-coverage', 'test-report'],
    metrics: [],
  })
  assert.equal(issues.length, 2)
  assert.equal(issues[0].check_name, 'pawl:test-coverage')
  assert.equal(issues[0].severity, 'blocker')
  assert.match(issues[0].description, /test-coverage/)
  assert.deepEqual(issues[0].location, { path: 'pawl.yaml', lines: { begin: 1 } })
  assert.notEqual(issues[0].fingerprint, issues[1].fingerprint)
})

test('a could-not-measure verdict without failed_metrics emits a single generic issue', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 2,
    failure_class: 'could-not-measure',
    error: 'no pawl.snapshot.json yet — run `pawl record` first.',
    metrics: [],
  })
  assert.equal(issues.length, 1)
  assert.equal(issues[0].check_name, 'pawl')
  assert.match(issues[0].description, /no pawl\.snapshot\.json yet/)
})

test('could-not-measure never produces an empty array — the widget must never read a broken gate as clean', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 2,
    failure_class: 'could-not-measure',
    error: 'boom',
    metrics: [],
  })
  assert.ok(issues.length > 0)
})

test('exit_code 2 with a missing/wrong failure_class is still treated as could-not-measure, never an empty array', () => {
  const issues = toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 2, metrics: [] })
  assert.ok(issues.length > 0)
  assert.equal(issues[0].severity, 'blocker')
})

test('exit_code 1 with no unsuppressed regression anywhere is a contract violation, not a clean report', () => {
  assert.throws(
    () => toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 1, metrics: [metric()] }),
    /exit_code 1/,
  )
})

test('anchor is resolved relative to configDir, not doubled when both are set', () => {
  const issues = toCodeQualityIssues(
    {
      schema_version: 2,
    command: 'check',
      exit_code: 1,
      metrics: [metric({ regressions: [regression({ kind: 'total', path: null, line: null, key: null })] })],
    },
    { anchor: 'quality.yaml', configDir: 'config' },
  )
  assert.equal(issues[0].location.path, 'config/quality.yaml')
})

test('a verdict scoped by --only is rejected, not silently reported as a clean gate', () => {
  assert.throws(
    () => toCodeQualityIssues({ schema_version: 2,
    command: 'check', exit_code: 0, only: ['eslint'], metrics: [metric()] }),
    /--only/,
  )
})

test('a record --accept-worse verdict is rejected, not reported as a GitLab failure it never gated on', () => {
  // Reproduces the actual shape `pawl record --accept-worse --format json`
  // prints on a worsened dimension: exit_code 0, accepted_worse populated,
  // yet the dimension's own regressions array stays unsuppressed — verified
  // by running it, not assumed.
  assert.throws(
    () =>
      toCodeQualityIssues({
        schema_version: 2,
        command: 'record',
        exit_code: 0,
        accepted_worse: [{ id: 'eslint', value: 10 }],
        metrics: [metric({ regressions: [regression()] })],
      }),
    /pawl record.*pawl check/,
  )
})

test('a regression with line: 0 (no source line) maps to a valid GitLab line, not 0', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [metric({ regressions: [regression({ line: 0 })] })],
  })
  assert.equal(issues[0].location.lines.begin, 1)
})

test('configDir rebases both regression and anchor paths onto the repository root', () => {
  const issues = toCodeQualityIssues(
    {
      schema_version: 2,
    command: 'check',
      exit_code: 1,
      metrics: [
        metric({ regressions: [regression({ path: 'src/a.ts' })] }),
        metric({
          id: 'type-coverage',
          regressions: [regression({ kind: 'total', path: null, line: null, key: null })],
        }),
      ],
    },
    { configDir: 'config' },
  )
  assert.equal(issues[0].location.path, 'config/src/a.ts')
  assert.equal(issues[1].location.path, 'config/pawl.yaml')
})

test('main() parses --anchor= and --config-dir= flags and rebases the resulting locations', () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'gitlab-codequality-'))
  try {
    const reportPath = path.join(dir, 'pawl.json')
    writeFileSync(
      reportPath,
      JSON.stringify({
        schema_version: 2,
    command: 'check',
        exit_code: 1,
        metrics: [metric({ regressions: [regression({ path: 'src/a.ts' })] })],
      }),
    )
    let printed = ''
    const originalWrite = process.stdout.write
    process.stdout.write = (chunk) => {
      printed += chunk
      return true
    }
    let exitCode
    try {
      exitCode = main([reportPath, '--config-dir=config', '--anchor=config/pawl.yaml'])
    } finally {
      process.stdout.write = originalWrite
    }
    assert.equal(exitCode, 0)
    const issues = JSON.parse(printed)
    assert.equal(issues[0].location.path, 'config/src/a.ts')
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('a per-file-count metric with both a total and per-file regression emits both — an unlocated finding can inflate the scalar without any per-file entry', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [
      metric({
        gate: 'per-file-count',
        regressions: [
          regression({ kind: 'total', key: null, path: null, line: null, message: '3 → 5' }),
          regression({ kind: 'per-file-count', path: 'src/a.ts', key: 'src/a.ts:5' }),
        ],
      }),
    ],
  })
  assert.equal(issues.length, 2)
  assert.deepEqual(
    issues.map((i) => i.location.path).sort(),
    ['pawl.yaml', 'src/a.ts'],
  )
})

test('a per-key-value metric with both a total and per-key regression emits both — they are independent checks', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [
      metric({
        gate: 'per-key-value',
        id: 'type-coverage',
        regressions: [
          regression({ kind: 'total', key: null, path: null, line: null, message: '92 → 90' }),
          regression({ kind: 'per-key-value', key: 'src/a.ts', path: 'src/a.ts', message: 'src/a.ts  95 → 91' }),
        ],
      }),
    ],
  })
  assert.equal(issues.length, 2)
  assert.deepEqual(
    issues.map((i) => i.location.path).sort(),
    ['pawl.yaml', 'src/a.ts'],
  )
})

test('a metric with only a total regression (no breakdown) still reports it as a fallback', () => {
  const issues = toCodeQualityIssues({
    schema_version: 2,
    command: 'check',
    exit_code: 1,
    metrics: [
      metric({
        id: 'type-coverage',
        regressions: [regression({ kind: 'total', key: null, path: null, line: null, message: '92 → 90' })],
      }),
    ],
  })
  assert.equal(issues.length, 1)
  assert.equal(issues[0].location.path, 'pawl.yaml')
})

test('a large report survives a piped child process without being truncated', () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'gitlab-codequality-'))
  try {
    const count = 4000
    const regressions = Array.from({ length: count }, (_, i) =>
      regression({ key: `src/file${i}.ts:1`, path: `src/file${i}.ts`, message: `src/file${i}.ts  0 → 1` }),
    )
    const reportPath = path.join(dir, 'pawl.json')
    writeFileSync(
      reportPath,
      JSON.stringify({ schema_version: 2,
    command: 'check', exit_code: 1, metrics: [metric({ regressions })] }),
    )
    const result = spawnSync(process.execPath, [SCRIPT_PATH, reportPath], {
      encoding: 'utf8',
      maxBuffer: 32 * 1024 * 1024,
    })
    assert.equal(result.status, 0, result.stderr)
    assert.ok(result.stdout.length > 65536, 'test payload should exceed the pipe buffer size it is guarding against')
    const issues = JSON.parse(result.stdout)
    assert.equal(issues.length, count)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('main() without a report path prints usage and returns exit code 2, never throws', () => {
  const originalError = console.error
  let logged = ''
  console.error = (msg) => {
    logged += msg
  }
  try {
    assert.equal(main([]), 2)
  } finally {
    console.error = originalError
  }
  assert.match(logged, /usage: node gitlab-codequality\.mjs/)
})
