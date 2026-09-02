import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const workflow = readFileSync(path.join(REPO_ROOT, '.github/workflows/codeql.yml'), 'utf8')

function namedStep(name) {
  const marker = `      - name: ${name}\n`
  const start = workflow.indexOf(marker)
  assert.notEqual(start, -1, `missing workflow step: ${name}`)

  const next = workflow.indexOf('\n      - name:', start + marker.length)
  return workflow.slice(start, next === -1 ? workflow.length : next)
}

test('CodeQL ratchet uses its own config and guards only pull requests', () => {
  const checkout = namedStep('Checkout code')
  assert.match(checkout, /\n\s+fetch-depth:\s*0\s*$/m)

  const ratchet = namedStep('pawl check — CodeQL findings ratchet (${{ matrix.language }})')
  assert.match(
    ratchet,
    /\n\s+args:\s*'-c pawl\.codeql\.yaml --only codeql-\$\{\{ matrix\.language \}\}'\s*$/m,
  )
  assert.match(
    ratchet,
    /\n\s+guard-ref:\s*\$\{\{\s*github\.event_name\s*==\s*'pull_request'\s*&&\s*format\('origin\/\{0\}',\s*github\.base_ref\)\s*\|\|\s*''\s*\}\}\s*$/m,
  )
})
