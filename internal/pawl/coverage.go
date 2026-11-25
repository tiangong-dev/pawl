package pawl

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// --- Coverage ------------------------------------------------------------

// measureCoverage reads a coverage report (lcov's text, cobertura's XML) and
// computes a coverage percentage for the requested metric — the two formats
// json-value cannot read. A report with zero of the requested unit found is a
// measurement failure, never a silent 0 or 100.
func measureCoverage(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	fileRel, _ := dim.Options["file"].(string)
	format, _ := dim.Options["format"].(string)
	command, _ := dim.Options["command"].(string)
	metric := "lines"
	if m, ok := dim.Options["metric"].(string); ok && m != "" {
		metric = m
	}

	data, artifact, err := readIngestReport(cfg, dim, stderr, command, fileRel)
	if err != nil {
		return MeasureResult{}, err
	}

	var pct float64
	var found float64
	var files map[string]struct{}
	_, minFilesRequired := dim.Options["min_files"]
	_, minFoundRequired := dim.Options["min_found"]
	switch format {
	case "lcov":
		pct, found, files, err = lcovCoverage(data, metric, cfg.Dir, minFilesRequired, minFoundRequired)
	case "cobertura":
		pct, found, files, err = coberturaCoverage(data, metric, cfg.Dir, minFilesRequired, minFoundRequired)
	default:
		return MeasureResult{}, fmt.Errorf("unknown coverage format %q", format)
	}
	if err != nil {
		return MeasureResult{}, err
	}
	if _, required := dim.Options["min_files"]; required {
		if len(files) == 0 {
			return MeasureResult{}, fmt.Errorf("coverage report has no valid source file evidence for min_files")
		}
		if minFiles, _ := numberOption(dim.Options, "min_files"); float64(len(files)) < minFiles {
			return MeasureResult{}, fmt.Errorf("coverage report contains %d distinct valid source file(s), expected at least %s", len(files), FormatNumber(minFiles))
		}
	}
	if _, required := dim.Options["min_found"]; required {
		if math.Trunc(found) != found {
			return MeasureResult{}, fmt.Errorf("coverage report %s found units is not a non-negative integer: %s", metric, FormatNumber(found))
		}
		if found > 9007199254740991 {
			return MeasureResult{}, fmt.Errorf("coverage report %s found units exceed the exactly representable integer range", metric)
		}
		if minFound, _ := numberOption(dim.Options, "min_found"); found < minFound {
			return MeasureResult{}, fmt.Errorf("coverage report has %s found units, expected at least %s", FormatNumber(found), FormatNumber(minFound))
		}
	}
	return MeasureResult{Value: pct, Unit: "%", Artifact: artifact}, nil
}

// lcovPercent sums the found/hit counters for the metric across every record in
// an lcov .info file. LF/LH = lines, FNF/FNH = functions, BRF/BRH = branches.
func lcovPercent(data []byte, metric string) (float64, error) {
	pct, _, _, err := lcovCoverage(data, metric, ".", false, false)
	return pct, err
}

func lcovCoverage(data []byte, metric, root string, collectFiles, requireFound bool) (float64, float64, map[string]struct{}, error) {
	var foundKey, hitKey string
	switch metric {
	case "lines":
		foundKey, hitKey = "LF", "LH"
	case "functions":
		foundKey, hitKey = "FNF", "FNH"
	case "branches":
		foundKey, hitKey = "BRF", "BRH"
	default:
		return 0, 0, nil, fmt.Errorf("unknown metric %q", metric)
	}

	// Counters must be non-negative finite numbers — ParseFloat happily accepts
	// "NaN", "Inf", and negatives, any of which would otherwise yield a bogus
	// percentage (e.g. LF:-1 LH:-1 → 100%) instead of a measurement failure.
	readCounter := func(key, raw string) (float64, error) {
		n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return 0, fmt.Errorf("lcov %s record is not a number: %q", key, raw)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, fmt.Errorf("lcov %s record is not a non-negative finite number: %q", key, raw)
		}
		if requireFound && math.Trunc(n) != n {
			return 0, fmt.Errorf("lcov %s record is not a non-negative integer: %q", key, raw)
		}
		return n, nil
	}

	// Well-formed lcov emits the found/hit summary counters in pairs inside
	// each SF:…end_of_record record — never outside one. Both structure rules
	// are enforced: a counter outside any record is malformed even as a
	// balanced pair (stray counters would silently skew the total), and
	// within a record the selected counters must pair (a merely
	// globally-balanced report — LF in one record, LH in another — is a
	// truncated or mangled report whose surviving counters would fabricate a
	// percentage). A record with neither counter of the selected metric is
	// fine.
	var found, hit float64
	var foundEvidence float64
	files := map[string]struct{}{}
	seenEvidenceSources := map[string]struct{}{}
	var pathResolver *coveragePathResolver
	inRecord := false
	recSource := ""
	recFound, recHit := 0, 0
	recFoundVal, recHitVal := 0.0, 0.0
	flushRecord := func() error {
		if recFound != recHit {
			return fmt.Errorf("lcov report is malformed: a record has %d %s counter(s) but %d %s counter(s) — they must pair within the same record",
				recFound, foundKey, recHit, hitKey)
		}
		// One summary pair per record: duplicate summaries in one record are
		// not independent records to be summed — that would dilute the
		// percentage with whichever duplicate the mangling left behind.
		if recFound > 1 {
			return fmt.Errorf("lcov report is malformed: a record has %d %s/%s summary pairs — a record carries at most one",
				recFound, foundKey, hitKey)
		}
		// hit ≤ found must hold per record — an impossible record (2 lines
		// hit out of 1 found) must not be masked by another record's slack
		// in the global sum, where it would fabricate a clean percentage.
		if recHitVal > recFoundVal {
			return fmt.Errorf("lcov report is malformed: a record has more %s hit (%v) than %s found (%v)",
				metric, recHitVal, metric, recFoundVal)
		}
		if (collectFiles || requireFound) && recFound == 1 && recSource != "" {
			if pathResolver == nil {
				pathResolver = newCoveragePathResolver()
			}
			paths, err := coverageSourcePathsWithResolver(root, nil, recSource, pathResolver)
			if err != nil {
				return fmt.Errorf("lcov SF path %q: %w", recSource, err)
			}
			if len(paths) > 0 {
				path := paths[0]
				if collectFiles {
					files[path] = struct{}{}
				}
				if requireFound {
					if _, duplicate := seenEvidenceSources[path]; duplicate {
						return fmt.Errorf("lcov report has duplicate source evidence for %q; min_found requires distinct source records", recSource)
					}
					seenEvidenceSources[path] = struct{}{}
					foundEvidence += recFoundVal
				}
			}
		}
		recFound, recHit = 0, 0
		recFoundVal, recHitVal = 0, 0
		recSource = ""
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SF:"):
			// A missing end_of_record before the next SF must not let an
			// unpaired counter escape validation.
			if err := flushRecord(); err != nil {
				return 0, 0, nil, err
			}
			inRecord = true
			recSource = strings.TrimSpace(strings.TrimPrefix(line, "SF:"))
		case line == "end_of_record":
			if err := flushRecord(); err != nil {
				return 0, 0, nil, err
			}
			inRecord = false
		default:
			if v, ok := strings.CutPrefix(line, foundKey+":"); ok {
				if !inRecord {
					return 0, 0, nil, fmt.Errorf("lcov report is malformed: %s counter outside any SF record", foundKey)
				}
				n, err := readCounter(foundKey, v)
				if err != nil {
					return 0, 0, nil, err
				}
				found += n
				recFound++
				recFoundVal += n
			} else if v, ok := strings.CutPrefix(line, hitKey+":"); ok {
				if !inRecord {
					return 0, 0, nil, fmt.Errorf("lcov report is malformed: %s counter outside any SF record", hitKey)
				}
				n, err := readCounter(hitKey, v)
				if err != nil {
					return 0, 0, nil, err
				}
				hit += n
				recHit++
				recHitVal += n
			}
		}
	}
	// A truncated trailing record (EOF without end_of_record) validates too.
	if err := flushRecord(); err != nil {
		return 0, 0, nil, err
	}
	if requireFound && len(seenEvidenceSources) == 0 {
		return 0, 0, nil, fmt.Errorf("lcov report has no valid in-directory SF source evidence for min_found")
	}
	// Each counter is finite, but the cross-record sum can still overflow
	// float64 — an overflowed found reads hit/Inf as a fabricated 0%.
	if math.IsInf(found, 0) || math.IsInf(hit, 0) {
		return 0, 0, nil, fmt.Errorf("lcov report is malformed: summed %s counters overflow", metric)
	}
	if found == 0 {
		return 0, 0, nil, fmt.Errorf("no %s coverage data in lcov report", metric)
	}
	if hit > found {
		return 0, 0, nil, fmt.Errorf("lcov report has more %s hit (%v) than found (%v)", metric, hit, found)
	}
	percentage := hit / found * 100
	if requireFound {
		found = foundEvidence
	}
	return percentage, found, files, nil
}

// coberturaPercent reads the root <coverage> element's line-rate / branch-rate
// (a 0–1 fraction) and scales it to a percentage. cobertura carries no
// functions rate, so metric "functions" never reaches here (config rejects it).
func coberturaPercent(data []byte, metric string) (float64, error) {
	pct, _, _, err := coberturaCoverage(data, metric, ".", false, false)
	return pct, err
}

func coberturaCoverage(data []byte, metric, root string, collectFiles, requireFound bool) (float64, float64, map[string]struct{}, error) {
	var pathResolver *coveragePathResolver
	var cov struct {
		XMLName       xml.Name
		LineRate      *float64 `xml:"line-rate,attr"`
		BranchRate    *float64 `xml:"branch-rate,attr"`
		LinesValid    string   `xml:"lines-valid,attr"`
		BranchesValid string   `xml:"branches-valid,attr"`
		Sources       []string `xml:"sources>source"`
		Packages      []struct {
			Sources []string `xml:"sources>source"`
			Classes []struct {
				Filename string `xml:"filename,attr"`
			} `xml:"classes>class"`
		} `xml:"packages>package"`
	}
	if err := xml.Unmarshal(data, &cov); err != nil {
		return 0, 0, nil, fmt.Errorf("not cobertura XML: %v", err)
	}
	// The rate attributes exist on the root <coverage> element; some other XML
	// that merely carries a line-rate attribute must not read as coverage.
	if cov.XMLName.Local != "coverage" {
		return 0, 0, nil, fmt.Errorf("not a cobertura report: root element is <%s>, want <coverage>", cov.XMLName.Local)
	}
	var rate *float64
	var attr string
	var foundRaw string
	switch metric {
	case "lines":
		rate, attr = cov.LineRate, "line-rate"
		foundRaw = cov.LinesValid
	case "branches":
		rate, attr = cov.BranchRate, "branch-rate"
		foundRaw = cov.BranchesValid
	default:
		return 0, 0, nil, fmt.Errorf("unknown metric %q", metric)
	}
	if rate == nil {
		return 0, 0, nil, fmt.Errorf("no %s coverage data: cobertura report has no %s attribute", metric, attr)
	}
	if math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0 || *rate > 1 {
		return 0, 0, nil, fmt.Errorf("cobertura %s is not a fraction in [0,1]: %v", attr, *rate)
	}
	files := map[string]struct{}{}
	if collectFiles {
		pathResolver = newCoveragePathResolver()
		for _, pkg := range cov.Packages {
			sourceRoots := append(append([]string(nil), cov.Sources...), pkg.Sources...)
			for _, class := range pkg.Classes {
				if strings.TrimSpace(class.Filename) == "" {
					continue
				}
				paths, err := coverageSourcePathsWithResolver(root, sourceRoots, class.Filename, pathResolver)
				if err != nil {
					return 0, 0, nil, fmt.Errorf("cobertura class filename %q: %w", class.Filename, err)
				}
				if len(paths) > 1 {
					paths, err = existingCoverageCandidatesWithResolver(paths, pathResolver)
					if err != nil {
						return 0, 0, nil, fmt.Errorf("cobertura class filename %q: %w", class.Filename, err)
					}
					if len(paths) == 0 {
						return 0, 0, nil, fmt.Errorf("cobertura class filename %q has multiple source roots but no existing in-root source file", class.Filename)
					}
					if len(paths) > 1 {
						return 0, 0, nil, fmt.Errorf("cobertura class filename %q resolves to multiple existing source files under <sources>", class.Filename)
					}
				}
				for _, path := range paths {
					files[path] = struct{}{}
				}
			}
		}
	}
	var found float64
	if requireFound && foundRaw != "" {
		n, err := strconv.ParseUint(strings.TrimSpace(foundRaw), 10, 64)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("cobertura %s-valid is not a non-negative integer: %q", metric, foundRaw)
		}
		if n > 9007199254740991 {
			return 0, 0, nil, fmt.Errorf("cobertura %s-valid exceeds the exactly representable integer range", metric)
		}
		found = float64(n)
	} else if requireFound {
		return 0, 0, nil, fmt.Errorf("no %s coverage data: cobertura report has no %s-valid denominator", metric, metric)
	}
	return *rate * 100, found, files, nil
}
