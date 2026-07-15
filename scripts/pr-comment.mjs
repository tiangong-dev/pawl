// Upsert one sticky PR comment rendered from a `pawl check --format json`
// report. Run by the setup-pawl action's comment step; the action, not the
// consumer, owns this — so `--format json` has a first-class consumer. Keyed by
// a hidden marker so each push updates the same comment instead of stacking a
// new one. Best-effort: a posting failure warns but never fails the gate (the
// action enforces the verdict in a separate step).

import fs from 'node:fs'
import { renderCommentBody, MARKER } from './render-comment.mjs'
import { upsertComment } from './upsert-comment.mjs'

const token = process.env.GITHUB_TOKEN
const reportPath = process.env.PAWL_REPORT
const repo = process.env.GITHUB_REPOSITORY // "owner/repo"
const eventPath = process.env.GITHUB_EVENT_PATH
const apiUrl = process.env.GITHUB_API_URL || 'https://api.github.com'

const skip = (why) => {
  console.log(`pawl: skipping PR comment — ${why}`)
  process.exit(0)
}

if (!token) skip('no token available')
if (!repo || !eventPath) skip('not running in a GitHub Actions event context')

let report
try {
  report = JSON.parse(fs.readFileSync(reportPath, 'utf8'))
} catch (e) {
  skip(`no readable report at ${reportPath} (${e.message})`)
}

const body = renderCommentBody(report)
if (body === null) skip('report has no metrics array')

const event = JSON.parse(fs.readFileSync(eventPath, 'utf8'))
const prNumber = event.pull_request && event.pull_request.number
if (!prNumber) skip('event is not a pull_request')
const [owner, repoName] = repo.split('/')

const api = async (method, path, payload) => {
  const res = await fetch(`${apiUrl}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      accept: 'application/vnd.github+json',
      'content-type': 'application/json',
    },
    body: payload ? JSON.stringify(payload) : undefined,
  })
  if (!res.ok) throw new Error(`${method} ${path} → ${res.status} ${await res.text()}`)
  return res.json()
}

try {
  const action = await upsertComment({
    api,
    owner,
    repo: repoName,
    prNumber,
    body,
    marker: MARKER,
  })
  console.log(`pawl: ${action} PR comment`)
} catch (e) {
  // Auxiliary output must not mask the gate verdict — warn loudly, exit clean.
  console.log(`::warning::pawl: failed to post PR comment: ${e.message}`)
}
