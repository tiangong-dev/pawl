import { test } from 'node:test'
import assert from 'node:assert/strict'
import { renderCommentBody, MARKER } from './render-comment.mjs'

const metric = (over) => ({
  id: 'eslint',
  title: 'ESLint issues',
  base: 10,
  current: 10,
  status: 'same',
  improved: false,
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

test('exit_code 0 renders the no-regressions header, non-zero renders the regressions header', () => {
  const clean = renderCommentBody({ exit_code: 0, metrics: [metric()] })
  assert.match(clean, /### pawl — ✅ no regressions/)

  const dirty = renderCommentBody({
    exit_code: 1,
    metrics: [metric({ current: 12, status: 'worse', regressions: [{ message: 'x', suppressed: false }] })],
  })
  assert.match(dirty, /### pawl — ❌ regressions/)
})

test('table header uses the metric/baseline/current/delta/status columns', () => {
  const body = renderCommentBody({ exit_code: 0, metrics: [metric()] })
  assert.match(body, /\| metric \| baseline \| current \| Δ \| status \|/)
})

test('a clean same-status metric with zero delta renders ±0 and the ✅ same icon cell', () => {
  const body = renderCommentBody({ exit_code: 0, metrics: [metric()] })
  assert.doesNotMatch(body, /\*\*Regressions\*\*/)
  assert.match(body, /\| eslint \| 10 \| 10 \| ±0 \| ✅ same \|/)
})

test('status "better" renders the 🎉 better icon cell', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: 10, current: 7, status: 'better' })],
  })
  assert.match(body, /\| eslint \| 10 \| 7 \| -3 \| 🎉 better \|/)
})

test('status "worse" renders the ❌ worse icon cell', () => {
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
  assert.match(body, /\| eslint \| 10 \| 12 \| \+2 \| ❌ worse \|/)
})

test('status "new" renders base "—", delta "new", and the 🆕 new icon cell', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: null, current: 3, status: 'new' })],
  })
  assert.match(body, /\| eslint \| — \| 3 \| new \| 🆕 new \|/)
})

test('status "within-tolerance" renders the ✅ within tolerance icon cell (space, not hyphen)', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: 10, current: 10.4, status: 'within-tolerance' })],
  })
  assert.match(body, /\| eslint \| 10 \| 10\.4 \| \+0\.4 \| ✅ within tolerance \|/)
  assert.doesNotMatch(body, /within-tolerance \|$/m)
})

test('a positive delta is rendered with an explicit plus sign', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: 10, current: 13, status: 'worse' })],
  })
  assert.match(body, /\| eslint \| 10 \| 13 \| \+3 \| ❌ worse \|/)
})

test('a negative delta is rendered with an ordinary ASCII minus sign', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ base: 10, current: 7, status: 'better' })],
  })
  assert.match(body, /\| eslint \| 10 \| 7 \| -3 \| 🎉 better \|/)
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
  assert.match(body, /\*\*Regressions\*\*/)
  assert.match(body, /src\/a\.ts {2}0 → 1/)
})

test('a suppressed-only regression is not listed and does not count', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ regressions: [{ message: 'moved line', suppressed: true }] })],
  })
  assert.doesNotMatch(body, /\*\*Regressions\*\*/)
  assert.doesNotMatch(body, /moved line/)
})

test('no metric improved: the improved footer is absent', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ improved: false }), metric({ id: 'type-coverage', improved: false })],
  })
  assert.doesNotMatch(body, /improved:/)
  assert.doesNotMatch(body, /pawl record/)
})

test('one metric improved: the footer names it with the exact wording', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [metric({ id: 'eslint', improved: true })],
  })
  assert.match(
    body,
    /🎉 \*\*improved:\*\* eslint — run `pawl record` to lock in the gains\.\n$/,
  )
})

test('multiple improved metrics are listed in metrics order, joined by comma-space', () => {
  const body = renderCommentBody({
    exit_code: 0,
    metrics: [
      metric({ id: 'file-length', improved: false }),
      metric({ id: 'eslint', improved: true }),
      metric({ id: 'code-duplication', improved: false }),
      metric({ id: 'type-coverage', improved: true }),
    ],
  })
  assert.match(
    body,
    /🎉 \*\*improved:\*\* eslint, type-coverage — run `pawl record` to lock in the gains\.\n$/,
  )
})
