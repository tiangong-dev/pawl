import { test } from 'node:test'
import assert from 'node:assert/strict'
import { upsertComment } from './upsert-comment.mjs'

// A fake GitHub API driven by a fixed set of comment pages. Records every call
// so tests can assert exactly one create/update and full pagination.
function fakeApi(pages) {
  const calls = []
  const api = async (method, path, payload) => {
    calls.push({ method, path, payload })
    if (method === 'GET') {
      const m = path.match(/[?&]page=(\d+)/)
      const page = m ? Number(m[1]) : 1
      return pages[page - 1] || []
    }
    return { id: 999 }
  }
  return { api, calls }
}

const marker = '<!-- pawl-report -->'
const full = (n, opts = {}) =>
  Array.from({ length: n }, (_, i) => ({ id: i + 1, body: opts.markerAt === i ? marker + ' x' : 'other' }))

test('creates a new comment when no page holds the marker', async () => {
  const { api, calls } = fakeApi([full(3)])
  const action = await upsertComment({ api, owner: 'o', repo: 'r', prNumber: 5, body: 'B', marker })
  assert.equal(action, 'created')
  assert.equal(calls.filter((c) => c.method === 'POST').length, 1)
  assert.equal(calls.filter((c) => c.method === 'PATCH').length, 0)
})

test('updates in place when the marker is on the first page', async () => {
  const { api, calls } = fakeApi([full(3, { markerAt: 1 })])
  const action = await upsertComment({ api, owner: 'o', repo: 'r', prNumber: 5, body: 'B', marker })
  assert.equal(action, 'updated')
  const patch = calls.find((c) => c.method === 'PATCH')
  assert.match(patch.path, /\/issues\/comments\/2$/)
  assert.equal(calls.filter((c) => c.method === 'POST').length, 0)
})

test('finds a marker past the first 100 comments and does NOT duplicate', async () => {
  // page 1 full (100, no marker), page 2 holds the marker.
  const { api, calls } = fakeApi([full(100), full(4, { markerAt: 2 })])
  const action = await upsertComment({ api, owner: 'o', repo: 'r', prNumber: 5, body: 'B', marker })
  assert.equal(action, 'updated')
  assert.equal(calls.filter((c) => c.method === 'POST').length, 0)
  // It must have paged: at least two GETs, page=1 then page=2.
  const gets = calls.filter((c) => c.method === 'GET')
  assert.ok(gets.length >= 2)
  assert.match(gets[1].path, /page=2/)
})

test('stops paging at the first short page (no infinite loop)', async () => {
  const { api, calls } = fakeApi([full(100), full(100), full(10)])
  const action = await upsertComment({ api, owner: 'o', repo: 'r', prNumber: 5, body: 'B', marker })
  assert.equal(action, 'created')
  // 3 GETs (100, 100, 10<100 stops) then 1 POST.
  assert.equal(calls.filter((c) => c.method === 'GET').length, 3)
})
