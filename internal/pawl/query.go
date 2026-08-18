package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const rankNearRatio = 0.9

type rankFile struct {
	Path   string  `json:"path"`
	Value  float64 `json:"value"`
	Status string  `json:"status"`
}

type rankDimension struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Threshold int        `json:"threshold"`
	Files     []rankFile `json:"files"`
}

// runRank lists every file matching each file-length / file-bytes dimension,
// including those under the threshold, sorted by size descending. Near-threshold
// files (above 90% of the limit, not yet over) are the ones an agent should
// not grow.
func runRank(cfg *Config, format string, stdout, stderr io.Writer) int {
	var dims []rankDimension
	for _, d := range cfg.Dimensions {
		var (
			kind     string
			fallback int
			sizeOf   func([]byte) int
		)
		switch d.Builtin {
		case builtinFileLength:
			kind, fallback, sizeOf = "lines", defaultFileLengthThreshold, lineCount
		case builtinFileBytes:
			kind, fallback, sizeOf = "bytes", defaultFileBytesThreshold, func(data []byte) int { return len(data) }
		default:
			continue
		}
		ranked, err := rankOne(cfg, d, fallback, sizeOf)
		if err != nil {
			fmt.Fprintf(stderr, "rank %s: %v\n", d.ID, err)
			return 2
		}
		dims = append(dims, rankDimension{
			ID:        d.ID,
			Kind:      kind,
			Threshold: intOption(d.Options, "threshold", fallback),
			Files:     ranked,
		})
	}
	if len(dims) == 0 {
		fmt.Fprintln(stderr, "rank needs a file-length or file-bytes dimension in the config.")
		return 2
	}
	if format == "json" {
		payload := map[string]any{
			"schema_version": 2,
			"command":        "rank",
			"dimensions":     dims,
		}
		return writeQueryJSON(stdout, stderr, payload)
	}
	for _, dim := range dims {
		fmt.Fprintf(stdout, "\n%s  (threshold %d %s)\n", dim.ID, dim.Threshold, dim.Kind)
		for _, f := range dim.Files {
			mark := "    "
			switch f.Status {
			case "over":
				mark = "OVER"
			case "near":
				mark = "near"
			}
			fmt.Fprintf(stdout, "  %s  %9s  %s\n", mark, FormatNumber(f.Value), f.Path)
		}
	}
	fmt.Fprintln(stdout, "")
	return 0
}

func rankOne(cfg *Config, dim Dimension, fallback int, sizeOf func([]byte) int) ([]rankFile, error) {
	threshold := intOption(dim.Options, "threshold", fallback)
	include := stringList(dim.Options["include"])
	exclude := stringList(dim.Options["exclude"])
	nearFloor := float64(threshold) * rankNearRatio
	var files []rankFile
	err := walkIncluded(cfg.Dir, include, exclude, func(rel, abs string) error {
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		n := sizeOf(data)
		status := "ok"
		if n > threshold {
			status = "over"
		} else if float64(n) > nearFloor {
			status = "near"
		}
		files = append(files, rankFile{Path: rel, Value: float64(n), Status: status})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Value != files[j].Value {
			return files[i].Value > files[j].Value
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func writeQueryJSON(stdout, stderr io.Writer, payload map[string]any) int {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "%s\n", data)
	return 0
}
