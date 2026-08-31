#!/usr/bin/env node
// docs-version-check — keep the version in the docs equal to the version we ship.
//
// The READMEs tell people to write `uses: tiangong-dev/pawl@vX.Y.Z` and to run
// `npx @pawl-tools/cli@X.Y.Z`. Those lines are instructions, not prose —
// someone copies them verbatim. A release bumps npm/cli/package.json; the docs
// stay behind; the copy-paste silently installs a release that is one or two
// versions old, and nothing anywhere says so. That is exactly what happened
// between v0.8.0 and v0.8.2.
//
// R1  read the version actually shipped, from npm/cli/package.json
// R2  scan the READMEs for pinned references to pawl's own artifacts
// R3  any reference naming a different version exits 1, printing the file,
//     the line, what it says, and what it should say
//
// Only pins to pawl's own artifacts are checked. `actions/checkout@v4` belongs
// to someone else, and a floating major alias like `pawl@v0` is a deliberate
// choice to track the latest — neither is this script's business.
//
// Run: node scripts/docs-version-check.mjs

import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

// The file the release workflow reads to decide what version to publish, so
// it is the same source of truth the docs have to agree with.
const VERSION_SOURCE = 'npm/cli/package.json'

const DOCS = ['README.md', 'README.zh-CN.md']

// Each pattern captures the version in group 1. Only fully-qualified x.y.z
// pins match; a major alias (`@v0`) has nothing to drift out of sync with.
export const REFERENCE_PATTERNS = [
  { kind: 'GitHub Action', re: /tiangong-dev\/pawl@v(\d+\.\d+\.\d+)/g },
  { kind: 'npm package', re: /@pawl-tools\/cli@(\d+\.\d+\.\d+)/g },
]

// --- R2/R3: detection -----------------------------------------------------

export function findStaleReferences(text, shipped) {
  const stale = []

  text.split('\n').forEach((line, index) => {
    for (const { kind, re } of REFERENCE_PATTERNS) {
      for (const match of line.matchAll(re)) {
        if (match[1] === shipped) continue
        stale.push({
          line: index + 1,
          kind,
          found: match[0],
          want: match[0].replace(match[1], shipped),
        })
      }
    }
  })

  return stale
}

// --- R1 + reporting -------------------------------------------------------

export function readShippedVersion(root = REPO_ROOT) {
  const pkg = JSON.parse(readFileSync(path.join(root, VERSION_SOURCE), 'utf8'))
  if (typeof pkg.version !== 'string' || !/^\d+\.\d+\.\d+/.test(pkg.version)) {
    throw new Error(`${VERSION_SOURCE} has no usable "version" field`)
  }
  return pkg.version
}

function main() {
  const shipped = readShippedVersion()
  let failed = false

  for (const doc of DOCS) {
    const stale = findStaleReferences(readFileSync(path.join(REPO_ROOT, doc), 'utf8'), shipped)
    for (const ref of stale) {
      failed = true
      console.error(`${doc}:${ref.line}  ${ref.kind} pinned to ${ref.found}, want ${ref.want}`)
    }
  }

  if (failed) {
    console.error(`\nThe docs point at a version we do not ship. ${VERSION_SOURCE} says ${shipped}.`)
    console.error('Update the lines above, or drop the pin to a major alias if the example should track the latest.')
    process.exit(1)
  }

  console.log(`docs pin ${shipped}, matching ${VERSION_SOURCE}`)
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main()
