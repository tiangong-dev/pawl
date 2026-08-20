import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

import { npmOperation, selectPackageDirs } from '../../publish.js';

test('dry-run validates the package locally without asking the registry to accept an already-published version', () => {
  assert.deepEqual(npmOperation({ dryRun: true, tag: 'latest', provenance: true }), {
    command: 'npm',
    args: ['pack', '--dry-run'],
  });
});

test('real publish keeps tag and provenance behavior', () => {
  assert.deepEqual(npmOperation({ dryRun: false, tag: 'dev', provenance: true }), {
    command: 'npm',
    args: ['publish', '--access', 'public', '--tag', 'dev', '--provenance'],
  });
  assert.deepEqual(npmOperation({ dryRun: false, tag: 'latest', provenance: false }), {
    command: 'npm',
    args: ['publish', '--access', 'public', '--tag', 'latest'],
  });
});

test('selectPackageDirs returns only directories containing package.json, sorted, excluding archives dir and stray files', (t) => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pawl-publish-select-'));
  t.after(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  const platformDirs = ['cli-win32-x64', 'cli-linux-x64', 'cli-darwin-arm64'];
  for (const dir of platformDirs) {
    const dirPath = path.join(tmp, dir);
    fs.mkdirSync(dirPath);
    fs.writeFileSync(
      path.join(dirPath, 'package.json'),
      JSON.stringify({ name: `@pawl-tools/${dir}`, version: '0.1.0' }),
    );
  }

  // archives dir: a directory that holds only release tarballs, no package.json.
  // This is the exact fixture that made the old naive "publish every entry" loop crash with ENOENT.
  const archivesDir = path.join(tmp, 'archives');
  fs.mkdirSync(archivesDir);
  fs.writeFileSync(path.join(archivesDir, 'pawl-0.1.0-linux-x64.tar.gz'), 'fake tarball contents');

  // stray plain file directly under distDir — must be skipped since it is not a directory.
  fs.writeFileSync(path.join(tmp, 'README.md'), '# not a package');

  const result = selectPackageDirs(tmp);

  const expected = platformDirs.map((dir) => path.join(tmp, dir)).sort();

  assert.deepEqual(result, expected, 'should return exactly the platform package dirs, sorted');

  for (const dirPath of result) {
    assert.notEqual(
      path.basename(dirPath),
      'archives',
      'archives dir (no package.json) must never be selected for publish',
    );
  }

  assert.ok(
    !result.includes(archivesDir),
    'archives dir must be excluded even though it is a directory',
  );
  assert.ok(
    !result.includes(path.join(tmp, 'README.md')),
    'plain files must be excluded',
  );

  for (const dirPath of result) {
    assert.ok(path.isAbsolute(dirPath), `${dirPath} must be an absolute path`);
  }
});

test('selectPackageDirs returns an empty array when distDir has no publishable packages', (t) => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'pawl-publish-select-empty-'));
  t.after(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  fs.mkdirSync(path.join(tmp, 'archives'));
  fs.writeFileSync(path.join(tmp, 'archives', 'pawl-0.1.0.tar.gz'), 'fake tarball contents');
  fs.writeFileSync(path.join(tmp, 'CHANGELOG.md'), '# changelog');

  const result = selectPackageDirs(tmp);

  assert.deepEqual(result, []);
});
