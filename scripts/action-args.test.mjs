import { test } from 'node:test'
import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'

const action = readFileSync(new URL('../action.yml', import.meta.url), 'utf8')

function runPawlScript() {
  const start = action.indexOf('      run: |', action.indexOf('    - name: Run pawl'))
  const end = action.indexOf('\n    - name: Guard pawl baseline', start)
  assert.ok(start >= 0 && end > start, 'Run pawl step should be present')
  return action
    .slice(start, end)
    .split('\n')
    .slice(1)
    .map((line) => line.startsWith('        ') ? line.slice(8) : line)
    .join('\n')
}

test('action captures a failing check and keeps its config output', () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-action-'))
  const bin = join(root, 'bin')
  const runnerTemp = join(root, 'runner-temp')
  const output = join(root, 'github-output')
  const pawl = join(bin, 'pawl')
  mkdirSync(bin)
  mkdirSync(runnerTemp)
  writeFileSync(pawl, '#!/usr/bin/env bash\nprintf \'{"exit_code":7}\\n\'\nexit 7\n')
  chmodSync(pawl, 0o755)

  try {
    execFileSync('bash', ['-euo', 'pipefail', '-c', runPawlScript()], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        PAWL_COMMAND: 'check',
        PAWL_ARGS: '-c pawl.smoke.yaml',
        RUNNER_TEMP: runnerTemp,
        GITHUB_OUTPUT: output,
      },
    })
    const outputs = readFileSync(output, 'utf8')
    assert.match(outputs, /code=7/)
    assert.match(outputs, /config=pawl\.smoke\.yaml/)
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
