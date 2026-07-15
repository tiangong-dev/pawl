import { test } from 'node:test'
import assert from 'node:assert/strict'
import { renderCommentBody, MARKER } from './render-comment.mjs'

const metric = (over) => ({
  id: 'eslint',
  title: 'ESLint issues',
  base: 10,
  current: 10,
  status: 'same',
  regressions: [],
  ...over,
})

test('null / metric-less reports render nothing', () => {
  assert.equal(renderCommentBody(null), null)
  assert.equal(renderCommentBody({}), null)
  assert.equal(renderCommentBody({ metrics: 'nope' }), null)
})

test('body starts with the sticky marker so upserts find it', () => {
  const body = renderCommentBody({ exit_code: 0, metrics: [metric()] })
  assert.ok(body.startsWith(MARKER))
})

test('a clean report reads as no regressions and has no Regressions block', () => {
  const body = renderCommentBody({ exit_code: 0, metrics: [metric()] })
  assert.match(body, /✅ no regressions/)
  assert.doesNotMatch(body, /\*\*Regressions\*\*/)
  assert.match(body, /\| eslint \| 10 \| 10 \| 0 \| same \|/)
})

test('an unsuppressed regression reads as regressions and lists the message', () => {
  const body = renderCommentBody({
    exit_code: 1,
    metrics: [
      metric({
        current: 12,
        status: 'worse',
        regressions: [{ message: 'src/a.ts  0 → 1', suppressed: false }],
      }),
    ],
  })
  assert.match(body, /❌ regressions/)
  assert.match(body, /\*\*Regressions\*\*/)
  assert.match(body, /src\/a\.ts {2}0 → 1/)
  assert.match(body, /\| eslint \| 10 \| 12 \| \+2 \| worse \|/)
})

test('a suppressed-only regression is not listed and does not count', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ regressions: [{ message: 'moved line', suppressed: true }] })],
  })
  assert.doesNotMatch(body, /\*\*Regressions\*\*/)
  assert.doesNotMatch(body, /moved line/)
})

test('a new dimension shows base "—" and delta "new"', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: null, current: 3, status: 'new' })],
  })
  assert.match(body, /\| eslint \| — \| 3 \| new \| new \|/)
})

test('a decrease renders a signed negative delta', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: 10, current: 7, status: 'better' })],
  })
  assert.match(body, /\| eslint \| 10 \| 7 \| -3 \| better \|/)
})
