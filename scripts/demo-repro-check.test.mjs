import { test } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  extractCheckBlocks,
  extractRegressionTitle,
  fixtureYaml,
  diffLines,
  verifyBlock,
} from './demo-repro-check.mjs'

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

test('extractCheckBlocks finds `$ pawl check` blocks and drops the command line', () => {
  const md = [
    'intro',
    '```console',
    '$ pawl check',
    'line one',
    'line two',
    '```',
    'between',
    '```console',
    '$ pawl record',
    'not a check block',
    '```',
  ].join('\n')
  assert.deepEqual(extractCheckBlocks(md), ['line one\nline two\n'])
})

test('extractCheckBlocks ignores docs with no check block', () => {
  assert.deepEqual(extractCheckBlocks('# no fences here'), [])
  assert.deepEqual(extractCheckBlocks('```js\n$pawl check\n```'), [])
})

test('extractRegressionTitle reads the title out of the regressions detail', () => {
  const block = '❌ regressions:\n  • file-length (Files over 500 lines)\n      total 3 → 4\n'
  assert.equal(extractRegressionTitle(block), 'Files over 500 lines')
  const zh = '❌ regressions:\n  • file-length (超过 500 行的文件)\n      total 3 → 4\n'
  assert.equal(extractRegressionTitle(zh), '超过 500 行的文件')
  assert.throws(() => extractRegressionTitle('no detail here\n'), /no `• file-length/)
})

test('fixtureYaml embeds the doc-supplied title', () => {
  const yaml = fixtureYaml('Files over 500 lines')
  assert.match(yaml, /title: "Files over 500 lines"/)
  assert.match(yaml, /threshold: 500/)
  // JSON.stringify keeps the yaml double-quoted string honest for zh titles
  assert.match(fixtureYaml('超过 500 行的文件'), /title: "超过 500 行的文件"/)
})

test('diffLines reports only the drifted lines', () => {
  assert.equal(diffLines('a\nb\n', 'a\nb\n'), '')
  const d = diffLines('a\nb\nc\n', 'a\nX\nc\n')
  assert.match(d, /doc line 2: "b"/)
  assert.match(d, /real line 2: "X"/)
})

const goAvailable = spawnSync('go', ['version'], { encoding: 'utf8' }).status === 0

// The lie detector must be proven to catch lies, not just to pass
// truthful docs: a tampered block (3 -> 4 flipped to 3 -> 3) must fail
// verification.
test('verifyBlock: real README block passes, tampered block fails', { skip: !goAvailable && 'go toolchain unavailable' }, () => {
  const work = mkdtempSync(path.join(tmpdir(), 'doc-repro-test-'))
  const binary = path.join(work, 'pawl')
  execFileSync('go', ['build', '-o', binary, './cmd/pawl'], { cwd: REPO_ROOT })

  const real = extractCheckBlocks(readFileSync(path.join(REPO_ROOT, 'README.md'), 'utf8'))[0]
  assert.ok(real, 'README.md must keep a `$ pawl check` console block')
  assert.equal(verifyBlock(binary, real, work), null)

  const tampered = real.replace('      total 3 → 4', '      total 3 → 3')
  assert.notEqual(tampered, real)
  const verdict = verifyBlock(binary, tampered, work)
  assert.match(verdict, /does not match real output/)
})

// Regression test for this gate's own first cloud run: under GitHub Actions
// the binary appends a `::error::` workflow annotation to stdout, which is CI
// chrome, not the documented output. verifyBlock must scrub GITHUB_ACTIONS so
// the bytes compared in CI equal the bytes a human sees locally.
test('verifyBlock: byte-faithful even when GITHUB_ACTIONS is set', { skip: !goAvailable && 'go toolchain unavailable' }, (t) => {
  const work = mkdtempSync(path.join(tmpdir(), 'doc-repro-test-ci-'))
  const binary = path.join(work, 'pawl')
  execFileSync('go', ['build', '-o', binary, './cmd/pawl'], { cwd: REPO_ROOT })

  const real = extractCheckBlocks(readFileSync(path.join(REPO_ROOT, 'README.md'), 'utf8'))[0]
  process.env.GITHUB_ACTIONS = 'true'
  t.after(() => delete process.env.GITHUB_ACTIONS)
  assert.equal(verifyBlock(binary, real, work), null)
})
