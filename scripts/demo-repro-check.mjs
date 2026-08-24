#!/usr/bin/env node
// demo-repro-check — the doc lie detector.
//
// The READMEs show a `pawl check` console block. A block like that is a claim:
// "if you run this, you will see exactly this."
//
// The only way to keep that claim honest is to actually run it. Wordlists
// catch text that looks fake; they cannot catch text that looks real but
// never happened. Execution can.
//
// R1  extract every ```console block containing `$ pawl check` from the READMEs
// R2  build the real binary from this checkout (no releases, no PATH pawl)
// R3  generate a throwaway fixture repo that forces the documented scenario
//     (3 -> 4 files over 500 lines, 1 panic, 12 TODO markers), `pawl record`
//     the baseline, add the fourth long file, then run `pawl check` for real
// R4  byte-compare stdout against the doc block
// R5  any drift exits 1 and prints the diff — the doc loses, reality wins
//
// The regressions line `• file-length (<title>)` carries the dimension
// title, which the zh-CN README translates — so the fixture config is
// generated per block with the title parsed out of the block itself. The
// docs literally specify their own reproduction.
//
// Run: node scripts/demo-repro-check.mjs   (requires go, git)

import { execFileSync, spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const DOCS = ['README.md', 'README.zh-CN.md']

// --- R1: extraction -------------------------------------------------------

// Pull the expected stdout out of every ```console block that runs `pawl
// check`. The `$ ` command line is documentation, not output; everything
// after it is the claim under test. Blocks running other commands are
// ignored.
export function extractCheckBlocks(markdown) {
  const blocks = []
  const fence = /```console\n([\s\S]*?)```/g
  let m
  while ((m = fence.exec(markdown)) !== null) {
    const lines = m[1].split('\n')
    const cmdIdx = lines.findIndex((l) => l.trim() === '$ pawl check')
    if (cmdIdx === -1) continue
    let body = lines.slice(cmdIdx + 1).join('\n')
    body = body.replace(/\n+$/, '') + '\n' // exactly one trailing newline
    blocks.push(body)
  }
  return blocks
}

// The regressions detail names the dimension as `• <id> (<title>)`. The
// title is the one part of the output the docs are allowed to choose
// (zh-CN translates it), so the fixture takes it from the block under test.
export function extractRegressionTitle(block) {
  const m = block.match(/^ {2}• file-length \((.+)\)$/m)
  if (!m) throw new Error('doc block has no `• file-length (<title>)` line to reproduce')
  return m[1]
}

// --- R3: fixture ----------------------------------------------------------

const LONG_FILE = '// deliberately over the 500-line threshold\n'.repeat(501)

export function fixtureYaml(title) {
  return `snapshot: "pawl.snapshot.json"

dimensions:
  - id: "file-length"
    title: ${JSON.stringify(title)}
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["src/**/*.go"]

  - id: "panics"
    title: "panic() calls in non-test code"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: 'panic\\('
      include: ["src/**/*.go"]

  - id: "todo-markers"
    title: "TODO / FIXME markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "TODO|FIXME"
      include: ["src/**/*.go"]
`
}

// Build the documented scenario in dir: baseline has 3 long files, 1
// panic, 12 TODO markers; the "PR" adds a fourth long file.
export function writeFixture(dir, title) {
  mkdirSync(path.join(dir, 'src'), { recursive: true })
  writeFileSync(path.join(dir, 'pawl.yaml'), fixtureYaml(title))
  for (const name of ['alpha.go', 'beta.go', 'gamma.go']) {
    writeFileSync(path.join(dir, 'src', name), LONG_FILE)
  }
  writeFileSync(
    path.join(dir, 'src', 'panics.go'),
    'package fixture\n\nfunc boom() { panic("the one legitimate panic") }\n',
  )
  const todos = ['package fixture', '']
  for (let i = 1; i <= 12; i++) todos.push(`// TODO: repay debt item ${i}`)
  writeFileSync(path.join(dir, 'src', 'todos.go'), todos.join('\n') + '\n')
}

// --- R4: comparison -------------------------------------------------------

export function diffLines(expected, actual) {
  const e = expected.split('\n')
  const a = actual.split('\n')
  const out = []
  const width = Math.max(e.length, a.length)
  for (let i = 0; i < width; i++) {
    if (e[i] === a[i]) continue
    if (e[i] !== undefined) out.push(`  doc line ${i + 1}: ${JSON.stringify(e[i])}`)
    if (a[i] !== undefined) out.push(`  real line ${i + 1}: ${JSON.stringify(a[i])}`)
  }
  return out.join('\n')
}

// --- R2 + driver ------------------------------------------------------------

// The READMEs document what a human sees in a terminal. Under GitHub
// Actions, pawl additionally annotates regressions with `::error::`
// workflow commands on stdout (runtime.go keys off GITHUB_ACTIONS) — CI
// chrome, not the documented output. The reproduction scrubs
// GITHUB_ACTIONS from the child's environment so the gate compares the
// same bytes on a laptop and in CI. (This gate's own first cloud run
// failed on exactly that annotation line.)
function plainEnv() {
  const env = { ...process.env }
  delete env.GITHUB_ACTIONS
  return env
}

function run(cmd, args, cwd) {
  return spawnSync(cmd, args, { cwd, encoding: 'utf8', env: plainEnv() })
}

// Reproduce one documented block against the real binary and compare
// bytes. Returns null on byte-faithful match, or a human-readable failure
// string. Exported so the test suite can prove the detector catches lies,
// not only that it passes truthful docs.
export function verifyBlock(binary, expected, workDir) {
  const title = extractRegressionTitle(expected)
  const fixture = mkdtempSync(path.join(workDir, 'fixture-'))
  writeFixture(fixture, title)
  execFileSync('git', ['init', '-q'], { cwd: fixture })
  const record = run(binary, ['record'], fixture)
  if (record.status !== 0) {
    return `pawl record exited ${record.status}\n${record.stderr}`
  }
  writeFileSync(path.join(fixture, 'src', 'delta.go'), LONG_FILE)
  const check = run(binary, ['check'], fixture)
  if (check.status !== 1) {
    return `expected exit 1 on regression, got ${check.status}`
  }
  if (check.stdout === expected) return null
  return `doc block does not match real output\n${diffLines(expected, check.stdout)}`
}

export function main({ log = console.log, repoRoot = REPO_ROOT } = {}) {
  const go = spawnSync('go', ['version'], { encoding: 'utf8' })
  if (go.status !== 0) {
    log('demo-repro: SKIP (go toolchain not available)')
    return 0
  }

  const work = mkdtempSync(path.join(tmpdir(), 'pawl-doc-repro-'))
  try {
    // R2: the binary under test comes from this checkout, never from PATH.
    log('demo-repro: building pawl from this checkout')
    execFileSync('go', ['build', '-o', path.join(work, 'pawl'), './cmd/pawl'], {
      cwd: repoRoot,
      stdio: 'inherit',
    })
    const binary = path.join(work, 'pawl')

    let failures = 0
    for (const doc of DOCS) {
      const markdown = readFileSync(path.join(repoRoot, doc), 'utf8')
      const blocks = extractCheckBlocks(markdown)
      if (blocks.length === 0) {
        log(`demo-repro: ${doc}: no \`\`\`console $ pawl check\` block found — nothing to verify`)
        continue
      }
      blocks.forEach((expected, i) => {
        const label = `${doc} block #${i + 1}`
        // R3+R4: reproduce the scenario for real, byte-compare. On drift
        // the doc loses (R5).
        const failure = verifyBlock(binary, expected, work)
        if (failure === null) {
          log(`demo-repro: ${label}: OK (byte-faithful)`)
        } else {
          log(`demo-repro: ${label}: FAIL (${failure})`)
          failures++
        }
      })
    }

    if (failures > 0) {
      log(`demo-repro: ${failures} block(s) failed — update the docs to what pawl actually prints`)
      return 1
    }
    log('demo-repro: all documented `pawl check` outputs reproduced byte-for-byte')
    return 0
  } finally {
    rmSync(work, { recursive: true, force: true })
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(main())
}
