package pawl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Project-type detection for `pawl init`. Detection reads marker files only: it
// runs no command and reads no manifest content, so it cannot fail on a tool
// that is missing, slow, or unreachable. A stack is detected when any of its
// marker files exists in the config file's directory.
// See spec/commands/init.md.

type projectType struct {
	// Name is the human-readable stack, reported in init's output.
	Name string
	// Markers are the files whose presence identifies the stack.
	Markers []string
	// Include is the source glob list the generated dimensions scan.
	Include []string
	// Exclude is the stack's build output and dependency trees.
	Exclude []string
}

// projectTypes is ordered so a multi-stack directory always generates the same
// config regardless of directory-order from the filesystem.
var projectTypes = []projectType{
	{
		Name:    "Node/TypeScript",
		Markers: []string{"package.json"},
		Include: []string{"**/*.{ts,tsx,js,jsx}"},
		Exclude: []string{"**/node_modules/**", "**/dist/**", "**/build/**", "**/coverage/**"},
	},
	{
		Name:    "Go",
		Markers: []string{"go.mod"},
		Include: []string{"**/*.go"},
		Exclude: []string{"**/vendor/**"},
	},
	{
		Name:    "Rust",
		Markers: []string{"Cargo.toml"},
		Include: []string{"**/*.rs"},
		Exclude: []string{"**/target/**"},
	},
	{
		Name:    "Python",
		Markers: []string{"pyproject.toml", "requirements.txt"},
		Include: []string{"**/*.py"},
		Exclude: []string{"**/.venv/**", "**/venv/**", "**/__pycache__/**"},
	},
	{
		Name:    "Swift",
		Markers: []string{"Package.swift"},
		Include: []string{"**/*.swift"},
		Exclude: []string{"**/.build/**"},
	},
}

// detectProject reports the stacks whose marker files exist in dir, plus the
// merged include and exclude globs for the generated config. The returned lists
// are de-duplicated and keep projectTypes order, so the same directory always
// produces the same config.
func detectProject(dir string) (include, exclude, labels []string) {
	seenInclude := map[string]bool{}
	seenExclude := map[string]bool{}
	for _, project := range projectTypes {
		marker, found := findMarker(dir, project.Markers)
		if !found {
			continue
		}
		labels = append(labels, fmt.Sprintf("%s (%s)", project.Name, marker))
		for _, glob := range project.Include {
			if !seenInclude[glob] {
				seenInclude[glob] = true
				include = append(include, glob)
			}
		}
		for _, glob := range project.Exclude {
			if !seenExclude[glob] {
				seenExclude[glob] = true
				exclude = append(exclude, glob)
			}
		}
	}
	return include, exclude, labels
}

// findMarker returns the first marker present in dir. A directory of the right
// name is not a marker: the generated config must reflect a real project file.
func findMarker(dir string, markers []string) (string, bool) {
	for _, marker := range markers {
		info, err := os.Stat(filepath.Join(dir, marker))
		if err == nil && !info.IsDir() {
			return marker, true
		}
	}
	return "", false
}

// renderDetectedConfig fills the detected-stack starter with the merged globs
// and the detected stack labels. The template is an embedded YAML asset rather
// than a Go string literal for the same reason starter.yaml is: its debt-marker
// example text must not be scanned by pawl's own `**/*.go` dimensions.
func renderDetectedConfig(include, exclude, labels []string) string {
	rendered := strings.NewReplacer(
		"__PAWL_INCLUDE__", yamlFlowList(include),
		"__PAWL_EXCLUDE__", yamlFlowList(exclude),
		"__PAWL_DETECTED__", strings.Join(labels, ", "),
	).Replace(detectedStarterConfig)
	return rendered
}

// yamlFlowList renders a glob list as a YAML flow sequence. Glob entries are
// quoted so a `{a,b}` alternation or a `*` stays literal.
func yamlFlowList(globs []string) string {
	quoted := make([]string, len(globs))
	for i, glob := range globs {
		quoted[i] = fmt.Sprintf("%q", glob)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
