import { test } from 'node:test'
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdirSync, symlinkSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'
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

function installPawlScript() {
  const start = action.indexOf('      run: |', action.indexOf('    - name: Install pawl'))
  const end = action.indexOf('\n    # Run the gate but capture its exit code', start)
  assert.ok(start >= 0 && end > start, 'Install pawl step should be present')
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

test('install step prefers gh release download and unpacks binary', () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-install-gh-'))
  const bin = join(root, 'bin')
  const runnerTemp = join(root, 'runner-temp')
  const githubPath = join(root, 'github-path')
  const logFile = join(root, 'call.log')
  mkdirSync(bin)
  mkdirSync(runnerTemp)

  const mockGh = join(bin, 'gh')
  writeFileSync(mockGh, `#!/usr/bin/env bash
echo "gh $@" >> "${logFile}"
if [ "$1" = "release" ] && [ "$2" = "download" ]; then
  targetDir=""
  while [ $# -gt 0 ]; do
    if [ "$1" = "--dir" ]; then
      targetDir="$2"
      shift 2
    else
      shift
    fi
  done
  touch "${bin}/pawl"
  chmod +x "${bin}/pawl"
  tar -czf "\${targetDir}/pawl-0.8.2-linux-x64.tar.gz" -C "${bin}" pawl
fi
`)
  chmodSync(mockGh, 0o755)

  try {
    execFileSync('bash', ['-euo', 'pipefail', '-c', installPawlScript()], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        RUNNER_OS: 'Linux',
        RUNNER_ARCH: 'X64',
        PAWL_VERSION: 'v0.8.2',
        RUNNER_TEMP: runnerTemp,
        GITHUB_PATH: githubPath,
      },
    })
    const log = readFileSync(logFile, 'utf8')
    assert.match(log, /gh release download v0\.8\.2 --repo tiangong-dev\/pawl --pattern pawl-0\.8\.2-linux-x64\.tar\.gz/)
    assert.ok(existsSync(join(runnerTemp, 'pawl-bin', 'pawl')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('install step falls back to curl with retry flags when gh fails', () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-install-curl-'))
  const bin = join(root, 'bin')
  const runnerTemp = join(root, 'runner-temp')
  const githubPath = join(root, 'github-path')
  const logFile = join(root, 'call.log')
  mkdirSync(bin)
  mkdirSync(runnerTemp)

  const mockGh = join(bin, 'gh')
  writeFileSync(mockGh, `#!/usr/bin/env bash
echo "gh $@" >> "${logFile}"
exit 1
`)
  chmodSync(mockGh, 0o755)

  const mockCurl = join(bin, 'curl')
  writeFileSync(mockCurl, `#!/usr/bin/env bash
echo "curl $@" >> "${logFile}"
if [ "$1" = "--retry-all-errors" ] && [ "$2" = "-V" ]; then
  exit 0
fi
for arg in "$@"; do
  outfile="$arg"
done
touch "${bin}/pawl"
chmod +x "${bin}/pawl"
tar -czf "$outfile" -C "${bin}" pawl
`)
  chmodSync(mockCurl, 0o755)

  try {
    execFileSync('bash', ['-euo', 'pipefail', '-c', installPawlScript()], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        RUNNER_OS: 'Linux',
        RUNNER_ARCH: 'X64',
        PAWL_VERSION: 'v0.8.2',
        RUNNER_TEMP: runnerTemp,
        GITHUB_PATH: githubPath,
      },
    })
    const log = readFileSync(logFile, 'utf8')
    assert.match(log, /gh release download/)
    assert.match(log, /curl -fsSL --retry 5 --retry-connrefused --retry-all-errors \S+pawl-0\.8\.2-linux-x64\.tar\.gz/)
    assert.ok(existsSync(join(runnerTemp, 'pawl-bin', 'pawl')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

// Mock curl: answers the --retry-all-errors probe, serves a release JSON for
// the API URL, and writes a tarball to whatever follows -o.
function mockCurl(bin, logFile, { failDownload = false } = {}) {
  const path = join(bin, 'curl')
  writeFileSync(path, `#!/bin/sh
echo "curl $*" >> "${logFile}"
if [ "$1" = "--retry-all-errors" ] && [ "$2" = "-V" ]; then exit 0; fi
case "$*" in
  *api.github.com*) printf '{\\n  "tag_name": "v0.8.2",\\n  "name": "v0.8.2"\\n}\\n'; exit 0 ;;
  *.sigstore.json*) exit 22 ;;
esac
${failDownload ? 'echo "curl: (56) The requested URL returned error: 504" >&2; exit 56' : ''}
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift 2; else shift; fi
done
src="$(mktemp -d)"
printf '#!/bin/sh\\n' > "$src/pawl"
chmod +x "$src/pawl"
tar -czf "$out" -C "$src" pawl
rm -rf "$src"
`)
  chmodSync(path, 0o755)
}

function failingGh(bin, logFile) {
  const path = join(bin, 'gh')
  writeFileSync(path, `#!/bin/sh
echo "gh $*" >> "${logFile}"
echo "gh: HTTP 504" >&2
exit 1
`)
  chmodSync(path, 0o755)
}

function actionEnv(bin, root, version) {
  return {
    ...process.env,
    PATH: `${bin}${delimiter}${process.env.PATH}`,
    RUNNER_OS: 'Linux',
    RUNNER_ARCH: 'X64',
    PAWL_VERSION: version,
    RUNNER_TEMP: join(root, 'runner-temp'),
    GITHUB_PATH: join(root, 'github-path'),
  }
}

test('install step resolves latest through curl when gh cannot', () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-install-latest-'))
  const bin = join(root, 'bin')
  const logFile = join(root, 'call.log')
  mkdirSync(bin)
  mkdirSync(join(root, 'runner-temp'))
  failingGh(bin, logFile)
  mockCurl(bin, logFile)
  try {
    const out = execFileSync('bash', ['-euo', 'pipefail', '-c', installPawlScript()], {
      env: actionEnv(bin, root, 'latest'),
      encoding: 'utf8',
    })
    assert.match(out, /downloading pawl-0\.8\.2-linux-x64\.tar\.gz \(v0\.8\.2\)/)
    const log = readFileSync(logFile, 'utf8')
    assert.match(log, /gh release view --repo tiangong-dev\/pawl/)
    assert.match(log, /curl -fsSL --retry 5 --retry-connrefused --retry-all-errors https:\/\/api\.github\.com\/repos\/tiangong-dev\/pawl\/releases\/latest/)
    assert.ok(existsSync(join(root, 'runner-temp', 'pawl-bin', 'pawl')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('install step fails when both gh and curl cannot download', () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-install-fail-'))
  const bin = join(root, 'bin')
  const logFile = join(root, 'call.log')
  mkdirSync(bin)
  mkdirSync(join(root, 'runner-temp'))
  failingGh(bin, logFile)
  mockCurl(bin, logFile, { failDownload: true })
  try {
    assert.throws(
      () => execFileSync('bash', ['-euo', 'pipefail', '-c', installPawlScript()], {
        env: actionEnv(bin, root, 'v0.8.2'),
        stdio: 'pipe',
      }),
      (err) => err.status === 56,
    )
    assert.ok(!existsSync(join(root, 'runner-temp', 'pawl-bin', 'pawl')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

// install.sh takes the binary-download path only when go and npm are absent,
// so run it with a PATH holding just the tools it needs plus the mocks.
test('install.sh falls back to curl with retries when gh download fails', { skip: process.platform === 'win32' }, () => {
  const root = mkdtempSync(join(tmpdir(), 'pawl-install-sh-'))
  const bin = join(root, 'bin')
  const dest = join(root, 'dest')
  const logFile = join(root, 'call.log')
  mkdirSync(bin)
  mkdirSync(dest)
  for (const tool of ['uname', 'grep', 'head', 'cut', 'mktemp', 'rm', 'tar', 'gzip', 'install', 'chmod', 'printf', 'cat']) {
    const found = process.env.PATH.split(delimiter).map((d) => join(d, tool)).find((p) => existsSync(p))
    if (found) symlinkSync(found, join(bin, tool))
  }
  failingGh(bin, logFile)
  mockCurl(bin, logFile)
  try {
    const out = execFileSync('/bin/sh', [new URL('../install.sh', import.meta.url).pathname], {
      env: { PATH: bin, HOME: root, PAWL_INSTALL_DIR: dest, PAWL_VERSION: 'v0.8.2' },
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    assert.match(out, /installed to /)
    const log = readFileSync(logFile, 'utf8')
    assert.match(log, /gh release download v0\.8\.2 --repo tiangong-dev\/pawl --pattern pawl-0\.8\.2-/)
    assert.match(log, /curl -fsSL --retry 5 --retry-connrefused --retry-all-errors https:\/\/github\.com\/tiangong-dev\/pawl\/releases\/download\/v0\.8\.2\/pawl-0\.8\.2-/)
    assert.ok(existsSync(join(dest, 'pawl')))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
