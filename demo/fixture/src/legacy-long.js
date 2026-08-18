// Configuration plumbing, extracted from the original single-file CLI during
// the 0.4 refactor. Most callers only need parseConfig and mergeConfigs; the
// path and alias helpers are still here because two downstream packages
// import them directly and we have not finished migrating those.
//
// The rough edges below predate the extraction and were carried over as-is,
// since that refactor was supposed to be behavior-preserving. They have been
// on the cleanup list for a while.
//
// Do not add new exports here — new config code goes in config/ instead.

function parseConfig(raw) {
  // TODO: validate schema before returning; currently trusts the caller.
  return JSON.parse(raw);
}

function mergeConfigs(base, override) {
  const result = { ...base };
  for (const key of Object.keys(override)) {
    if (override[key] !== undefined) {
      result[key] = override[key];
    }
  }
  return result;
}

function normalizePath(path) {
  return path.replace(/\\/g, "/").replace(/\/+$/, "");
}

function resolveAlias(aliases, name) {
  let seen = new Set();
  let current = name;
  while (aliases[current] && !seen.has(current)) {
    seen.add(current);
    current = aliases[current];
  }
  return current;
}

function deepFreeze(obj) {
  // FIXME: does not handle circular references — will stack-overflow.
  Object.getOwnPropertyNames(obj).forEach((key) => {
    const value = obj[key];
    if (value && typeof value === "object") {
      deepFreeze(value);
    }
  });
  return Object.freeze(obj);
}

module.exports = {
  parseConfig,
  mergeConfigs,
  normalizePath,
  resolveAlias,
  deepFreeze,
};
