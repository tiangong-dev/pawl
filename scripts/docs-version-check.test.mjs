import { test } from 'node:test'
import assert from 'node:assert/strict'
import { findStaleReferences } from './docs-version-check.mjs'

test('a doc pinned to the shipped version reports nothing', () => {
  const doc = ['      - uses: tiangong-dev/pawl@v0.8.2', 'npx -y @pawl-tools/cli@0.8.2 check'].join('\n')
  assert.deepEqual(findStaleReferences(doc, '0.8.2'), [])
})

test('a stale Action pin reports its line and the replacement', () => {
  const doc = ['first line', '      - uses: tiangong-dev/pawl@v0.8.0'].join('\n')
  const stale = findStaleReferences(doc, '0.8.2')

  assert.equal(stale.length, 1)
  assert.equal(stale[0].line, 2)
  assert.equal(stale[0].kind, 'GitHub Action')
  assert.equal(stale[0].found, 'tiangong-dev/pawl@v0.8.0')
  assert.equal(stale[0].want, 'tiangong-dev/pawl@v0.8.2')
})

test('a stale npm pin reports its line and the replacement', () => {
  const stale = findStaleReferences('npx -y @pawl-tools/cli@0.8.0 check', '0.8.2')

  assert.equal(stale.length, 1)
  assert.equal(stale[0].kind, 'npm package')
  assert.equal(stale[0].want, '@pawl-tools/cli@0.8.2')
})

test("third-party pins are somebody else's version", () => {
  const doc = ['      - uses: actions/checkout@v4', '      - uses: actions/setup-node@v4.0.1'].join('\n')
  assert.deepEqual(findStaleReferences(doc, '0.8.2'), [])
})

test('a floating major alias has nothing to drift out of sync with', () => {
  assert.deepEqual(findStaleReferences('      - uses: tiangong-dev/pawl@v0', '0.8.2'), [])
})

test('every stale reference on one line is reported, not just the first', () => {
  const doc = 'tiangong-dev/pawl@v0.8.0 and @pawl-tools/cli@0.7.1'
  const stale = findStaleReferences(doc, '0.8.2')

  assert.equal(stale.length, 2)
  assert.deepEqual(
    stale.map((r) => r.found).sort(),
    ['@pawl-tools/cli@0.7.1', 'tiangong-dev/pawl@v0.8.0'],
  )
})

test('scanning twice gives the same answer', () => {
  // The patterns are module-level and carry /g; a shared lastIndex would make
  // the second scan miss matches the first one found.
  const doc = 'tiangong-dev/pawl@v0.8.0'
  assert.deepEqual(findStaleReferences(doc, '0.8.2'), findStaleReferences(doc, '0.8.2'))
})
