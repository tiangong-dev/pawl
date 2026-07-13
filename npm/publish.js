#!/usr/bin/env node
// Publishes the packages produced by npm/build.js: platform packages first,
// then @pawl-tools/cli — so the launcher is never installable while a
// binary it points at is still missing from the registry.
//
//   NPM_TAG=dev node npm/publish.js   # dist-tag (default "latest")
import { execFileSync } from 'node:child_process';
import { readdirSync, existsSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// selectPackageDirs returns the publishable platform-package directories under
// distDir, sorted — each is a subdirectory that actually holds a package.json.
// It deliberately skips dist/archives/, the release tarballs build.js emits
// alongside the packages when PAWL_ARCHIVES=1: those are GitHub Release assets
// with no manifest, and `npm publish`-ing them fails with ENOENT.
export function selectPackageDirs(distDir) {
  return readdirSync(distDir)
    .sort()
    .map((entry) => join(distDir, entry))
    .filter((dir) => statSync(dir).isDirectory() && existsSync(join(dir, 'package.json')));
}

function main() {
  const npmDir = dirname(fileURLToPath(import.meta.url));
  const distDir = join(npmDir, 'dist');
  const tag = process.env.NPM_TAG || 'latest';

  if (!existsSync(distDir)) {
    console.error('npm/dist/ not found — run `node npm/build.js` first.');
    process.exit(1);
  }

  // PAWL_DRY_RUN validates the full publish path (pack every package, check
  // the registry would accept it) without uploading — CI runs it on PRs so a
  // publish-time break surfaces before merge, not on the release after it.
  const dryRun = process.env.PAWL_DRY_RUN === '1';

  // --provenance signs the tarball with the CI runner's OIDC identity (needs
  // `id-token: write`); it is a no-op-erroring flag off-CI, and a dry run
  // uploads nothing to attest, so only opt in for a real CI publish.
  const provenance = process.env.GITHUB_ACTIONS === 'true' && !dryRun ? ['--provenance'] : [];

  const publish = (dir) =>
    execFileSync(
      'npm',
      ['publish', '--access', 'public', '--tag', tag, ...provenance, ...(dryRun ? ['--dry-run'] : [])],
      { cwd: dir, stdio: 'inherit' }
    );

  for (const dir of selectPackageDirs(distDir)) {
    publish(dir);
  }
  publish(join(npmDir, 'cli'));
  const verb = dryRun ? 'dry-run: would publish' : 'published';
  console.log(`${verb} @pawl-tools/cli + platform packages under dist-tag "${tag}"`);
}

// Run the publish only when invoked directly, so tests can import
// selectPackageDirs without triggering a real npm publish.
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main();
}
