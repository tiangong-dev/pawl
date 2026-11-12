package pawl

// Top-level `exclude` is the shared exclusion list. It is appended to the
// exclusion list of every builtin dimension that scans the filesystem, so a
// repository can declare `node_modules`, `dist`, or `vendor` once instead of
// repeating the same globs in each dimension. Dimensions with no include/exclude
// concept (command adapters, named analyzers) are untouched.
// See spec/commands/config.md.

// sharedExcludeBuiltins names the dimensions the top-level list reaches: the
// ones whose options carry include/exclude globs. Every other builtin reads a
// command, a report file, or a tool-generated artifact, and has nothing to
// exclude from a filesystem walk.
var sharedExcludeBuiltins = map[string]bool{
	builtinFileLength:   true,
	builtinFileBytes:    true,
	builtinPatternCount: true,
}

// bindDimensionDefinitions folds the shared exclusion globs into each
// filesystem-scanning dimension and fills in every dimension's definition
// fingerprint. It runs after validation, before any measurement: the fingerprint
// must describe the effective exclude set the measured number came from, so a
// config that spells the same exclusions differently still records the same
// definition.
func bindDimensionDefinitions(cfg *Config, shared []string) error {
	for i := range cfg.Dimensions {
		cfg.Dimensions[i].Options = applySharedExclude(cfg.Dimensions[i].Builtin, cfg.Dimensions[i].Options, shared)
		definition, err := definitionFingerprint(cfg, cfg.Dimensions[i])
		if err != nil {
			return err
		}
		cfg.Dimensions[i].Definition = definition
	}
	return nil
}

// applySharedExclude returns options with the top-level globs appended after the
// dimension's own, duplicates removed. The result is the dimension's *effective*
// exclusion set, which is what both the filesystem walk and the definition
// fingerprint read, so lifting a glob repeated in every dimension up to the top
// level leaves the fingerprint unchanged.
//
// De-duplication is what makes that migration invisible: appending verbatim
// would turn the repeated `node_modules` glob into two entries and change the
// fingerprint even though the set of excluded paths is identical.
func applySharedExclude(builtin string, options map[string]any, shared []string) map[string]any {
	if len(shared) == 0 || !sharedExcludeBuiltins[builtin] {
		return options
	}
	if options == nil {
		options = map[string]any{}
	}
	own := stringList(options["exclude"])
	seen := make(map[string]bool, len(own)+len(shared))
	effective := make([]any, 0, len(own)+len(shared))
	add := func(glob string) {
		if seen[glob] {
			return
		}
		seen[glob] = true
		effective = append(effective, glob)
	}
	for _, glob := range own {
		add(glob)
	}
	for _, glob := range shared {
		add(glob)
	}
	options["exclude"] = effective
	return options
}
