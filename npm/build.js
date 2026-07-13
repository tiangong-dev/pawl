#!/usr/bin/env node
// Cross-compiles the pawl binary for every supported target and generates
// the per-platform npm packages under npm/dist/. The version single source
// of truth is npm/cli/package.json — this script stamps it into the Go
// binary (ldflags) and rewrites cli's optionalDependencies to match, so the
// launcher and its binaries can never drift apart.
//
//   node npm/build.js           # build all platform packages into npm/dist/
//   node npm/publish.js         # publish platform packages, then the cli
import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const npmDir = dirname(fileURLToPath(import.meta.url));
const repoDir = dirname(npmDir);
const distDir = join(npmDir, 'dist');
const archiveDir = join(distDir, 'archives');
const makeArchives = process.env.PAWL_ARCHIVES === '1';

const cliPkgPath = join(npmDir, 'cli', 'package.json');
const cliPkg = JSON.parse(readFileSync(cliPkgPath, 'utf8'));
// The version single source of truth is cli/package.json, but CI overrides it
// via PAWL_VERSION (e.g. a dev timestamp) — persisted back so the launcher and
// its stamped binaries share one version.
if (process.env.PAWL_VERSION) {
  cliPkg.version = process.env.PAWL_VERSION;
  writeFileSync(cliPkgPath, JSON.stringify(cliPkg, null, 2) + '\n');
}
const version = cliPkg.version;

// node platform/arch → Go GOOS/GOARCH. Must stay in sync with
// npm/cli/lib/platform.js (the launcher-side resolver).
const TARGETS = [
  { platform: 'darwin', arch: 'arm64', goos: 'darwin', goarch: 'arm64', bin: 'pawl' },
  { platform: 'darwin', arch: 'x64', goos: 'darwin', goarch: 'amd64', bin: 'pawl' },
  { platform: 'linux', arch: 'arm64', goos: 'linux', goarch: 'arm64', bin: 'pawl' },
  { platform: 'linux', arch: 'x64', goos: 'linux', goarch: 'amd64', bin: 'pawl' },
  { platform: 'win32', arch: 'arm64', goos: 'windows', goarch: 'arm64', bin: 'pawl.exe' },
  { platform: 'win32', arch: 'x64', goos: 'windows', goarch: 'amd64', bin: 'pawl.exe' },
];

rmSync(distDir, { recursive: true, force: true });

const optionalDeps = {};
for (const t of TARGETS) {
  const name = `@pawl-tools/cli-${t.platform}-${t.arch}`;
  const pkgDir = join(distDir, `cli-${t.platform}-${t.arch}`);
  mkdirSync(pkgDir, { recursive: true });

  console.log(`building ${name}@${version} (${t.goos}/${t.goarch})`);
  execFileSync(
    'go',
    [
      'build',
      '-trimpath',
      '-ldflags',
      `-s -w -X github.com/tiangong-dev/pawl/internal/pawl.Version=${version}`,
      '-o',
      join(pkgDir, t.bin),
      './cmd/pawl',
    ],
    {
      cwd: repoDir,
      stdio: 'inherit',
      env: { ...process.env, CGO_ENABLED: '0', GOOS: t.goos, GOARCH: t.goarch },
    }
  );

  writeFileSync(
    join(pkgDir, 'package.json'),
    JSON.stringify(
      {
        name,
        version,
        description: `pawl binary for ${t.platform}-${t.arch}`,
        license: cliPkg.license,
        repository: cliPkg.repository,
        // Yarn PnP must extract the binary to disk to exec it.
        preferUnplugged: true,
        os: [t.platform],
        cpu: [t.arch],
      },
      null,
      2
    ) + '\n'
  );
  optionalDeps[name] = version;

  // Standalone archive for direct download / GitHub Release (npm-independent).
  if (makeArchives) {
    mkdirSync(archiveDir, { recursive: true });
    const stem = `pawl-${version}-${t.platform}-${t.arch}`;
    if (t.platform === 'win32') {
      execFileSync('zip', ['-j', join(archiveDir, `${stem}.zip`), join(pkgDir, t.bin)], {
        stdio: 'inherit',
      });
    } else {
      execFileSync('tar', ['-czf', join(archiveDir, `${stem}.tar.gz`), '-C', pkgDir, t.bin], {
        stdio: 'inherit',
      });
    }
  }
}

cliPkg.optionalDependencies = optionalDeps;
writeFileSync(cliPkgPath, JSON.stringify(cliPkg, null, 2) + '\n');
console.log(`done: ${TARGETS.length} platform packages in npm/dist/, cli optionalDependencies pinned to ${version}`);
