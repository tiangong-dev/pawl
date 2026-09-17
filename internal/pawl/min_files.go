package pawl

import "fmt"

// validateMinFilesOption verifies a completeness floor only where an adapter
// can prove its scanned-file count.
func validateMinFilesOption(options map[string]any) error {
	value, exists := options["min_files"]
	if !exists {
		return nil
	}
	n, ok := numberOption(options, "min_files")
	if !ok || n < 0 || n != float64(int(n)) {
		return fmt.Errorf("min_files must be a non-negative integer, got %v", value)
	}
	return nil
}

// requireMinFiles checks a proven scan count against a configured completeness
// floor. The root is part of the diagnosis: an empty scan can be a bad glob or
// the wrong config directory, and those need different fixes.
func requireMinFiles(options map[string]any, scanned int, root string) error {
	minFiles, ok := numberOption(options, "min_files")
	if !ok || scanned >= int(minFiles) {
		return nil
	}
	return fmt.Errorf("scanned %d file(s) under %s, expected at least %d", scanned, root, int(minFiles))
}
