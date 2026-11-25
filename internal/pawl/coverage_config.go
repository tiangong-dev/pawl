package pawl

import "fmt"

func validateCoverageBuiltinOptions(options map[string]any) error {
	if file, _ := options["file"].(string); file == "" {
		return fmt.Errorf("builtin %q requires a file option (the coverage report)", builtinCoverage)
	}
	format, _ := options["format"].(string)
	switch format {
	case "lcov", "cobertura":
	default:
		return fmt.Errorf("builtin %q requires a format option of lcov or cobertura, got %q", builtinCoverage, format)
	}
	metric, _ := options["metric"].(string)
	switch metric {
	case "", "lines", "branches", "functions":
	default:
		return fmt.Errorf("builtin %q metric must be lines, branches or functions, got %q", builtinCoverage, metric)
	}
	if metric == "functions" && format == "cobertura" {
		return fmt.Errorf("builtin %q metric functions is lcov-only (cobertura has no functions rate)", builtinCoverage)
	}
	for _, key := range []string{"min_files", "min_found"} {
		if err := validateCoverageFloorOption(options, key); err != nil {
			return fmt.Errorf("builtin %q %v", builtinCoverage, err)
		}
	}
	return nil
}
