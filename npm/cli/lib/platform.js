// Maps Node's process.platform / process.arch to the @pawl-tools platform
// package carrying the Go binary for that target. Go binaries are built
// CGO-free (fully static), so one linux package covers glibc and musl alike.
const PACKAGES = {
  'darwin-arm64': { pkg: '@pawl-tools/cli-darwin-arm64', bin: 'pawl' },
  'darwin-x64': { pkg: '@pawl-tools/cli-darwin-x64', bin: 'pawl' },
  'linux-x64': { pkg: '@pawl-tools/cli-linux-x64', bin: 'pawl' },
  'linux-arm64': { pkg: '@pawl-tools/cli-linux-arm64', bin: 'pawl' },
  'win32-x64': { pkg: '@pawl-tools/cli-win32-x64', bin: 'pawl.exe' },
  'win32-arm64': { pkg: '@pawl-tools/cli-win32-arm64', bin: 'pawl.exe' },
};

export function platformPackage(platform, arch) {
  return PACKAGES[`${platform}-${arch}`] || null;
}
