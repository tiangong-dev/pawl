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

// npmOperation keeps dry-run validation local. npm 11 asks the registry to
// accept a version even under `npm publish --dry-run`, so every PR after a
// release fails merely because that released version already exists. `npm pack
// --dry-run` exercises manifest parsing, lifecycle scripts and package contents
// without consulting version uniqueness; the real path remains npm publish.
export function npmOperation({ dryRun, tag, provenance }) {
  if (dryRun) {
    return { command: 'npm', args: ['pack', '--dry-run'] };
  }
  return {
    command: 'npm',
    args: [
      'publish', '--access', 'public', '--tag', tag,
      ...(provenance ? ['--provenance'] : []),
    ],
  };
}

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

  // PAWL_DRY_RUN validates that every package can be packed without uploading
  // or asking the registry to accept this version. CI runs it on PRs so broken
  // manifests, lifecycle scripts and package contents surface before release.
  const dryRun = process.env.PAWL_DRY_RUN === '1';

  // --provenance signs the tarball with the CI runner's OIDC identity (needs
  // `id-token: write`); it is a no-op-erroring flag off-CI, and a dry run
  // uploads nothing to attest, so only opt in for a real CI publish.
  const provenance = process.env.GITHUB_ACTIONS === 'true' && !dryRun;

  const publish = (dir) => {
    const operation = npmOperation({ dryRun, tag, provenance });
    return execFileSync(operation.command, operation.args, { cwd: dir, stdio: 'inherit' });
  };

  for (const dir of selectPackageDirs(distDir)) {
    publish(dir);
  }
  publish(join(npmDir, 'cli'));
  const verb = dryRun ? 'dry-run: packages validated for publish' : 'published';
  console.log(`${verb} @pawl-tools/cli + platform packages under dist-tag "${tag}"`);
}

// Run the publish only when invoked directly, so tests can import
// selectPackageDirs without triggering a real npm publish.
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main();
}
