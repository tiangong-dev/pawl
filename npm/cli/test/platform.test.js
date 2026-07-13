import test from 'node:test';
import assert from 'node:assert/strict';

import { platformPackage } from '../lib/platform.js';

const SUPPORTED = [
  { platform: 'darwin', arch: 'arm64', pkg: '@pawl-tools/cli-darwin-arm64', bin: 'pawl' },
  { platform: 'darwin', arch: 'x64', pkg: '@pawl-tools/cli-darwin-x64', bin: 'pawl' },
  { platform: 'linux', arch: 'x64', pkg: '@pawl-tools/cli-linux-x64', bin: 'pawl' },
  { platform: 'linux', arch: 'arm64', pkg: '@pawl-tools/cli-linux-arm64', bin: 'pawl' },
  { platform: 'win32', arch: 'x64', pkg: '@pawl-tools/cli-win32-x64', bin: 'pawl.exe' },
  { platform: 'win32', arch: 'arm64', pkg: '@pawl-tools/cli-win32-arm64', bin: 'pawl.exe' },
];

const UNSUPPORTED = [
  ['freebsd', 'x64'],
  ['linux', 'ia32'],
  ['darwin', 'ppc64'],
  [undefined, undefined],
];

test('platformPackage is a function', () => {
  assert.equal(typeof platformPackage, 'function');
});

for (const { platform, arch, pkg, bin } of SUPPORTED) {
  test(`platformPackage resolves ${platform}/${arch}`, () => {
    assert.deepEqual(platformPackage(platform, arch), { pkg, bin });
  });
}

for (const [platform, arch] of UNSUPPORTED) {
  test(`platformPackage returns null for unsupported ${String(platform)}/${String(arch)}`, () => {
    assert.equal(platformPackage(platform, arch), null);
  });
}
