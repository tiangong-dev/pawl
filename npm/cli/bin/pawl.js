#!/usr/bin/env node

// Thin launcher: resolves the platform binary package installed as an
// optionalDependency and execs it with inherited stdio, forwarding the
// exit code untouched (pawl's 0/1/2 contract must survive the shell).
// Launcher-level failures exit 2 — "cannot run" must never read as a pass.
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { platformPackage } from '../lib/platform.js';

// require.resolve has no direct ESM equivalent across node>=18; createRequire
// gives the same resolver anchored at this module.
const require = createRequire(import.meta.url);

const target = platformPackage(process.platform, process.arch);
if (!target) {
  console.error(
    `pawl: unsupported platform ${process.platform}-${process.arch}. ` +
      'Prebuilt binaries cover darwin/linux/win32 on x64/arm64; ' +
      'build from source with `go install github.com/tiangong-dev/pawl/cmd/pawl@latest`.'
  );
  process.exit(2);
}

let binPath;
try {
  binPath = require.resolve(`${target.pkg}/${target.bin}`);
} catch {
  console.error(
    `pawl: platform package ${target.pkg} is not installed. ` +
      'Package managers skip optionalDependencies under --no-optional / omit=optional — ' +
      'reinstall with optional dependencies enabled.'
  );
  process.exit(2);
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error(`pawl: failed to run ${binPath}: ${result.error.message}`);
  process.exit(2);
}
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status === null ? 2 : result.status);
