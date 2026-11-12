Part of the pawl engine contract. See [spec/README.md](README.md).

## Public Go API (package `pawl`)

The pure comparison core is exported so tests (and future embedders) hit the same
judgment the CLI uses — one source of truth for "did this get worse":

```go
type Direction string   // "lower-is-better" | "higher-is-better"
type GateMode string    // "total" | "per-file-count" | "per-key-value"

type Metric struct {
    Direction  Direction          `json:"direction"`
    Definition string             `json:"definition,omitempty"`
    Value      float64            `json:"value"`
    Unit      string             `json:"unit"`
    Breakdown map[string]float64 `json:"breakdown"`          // nil ⇔ JSON null
    Tolerance *float64           `json:"tolerance,omitempty"` // nil ⇔ undeclared
}

type Snapshot struct {
    Metrics map[string]Metric `json:"metrics"`
}

type MetricSample struct {          // narrow input for regression checks
    Value     float64
    Breakdown map[string]float64
}

type GateSpec struct {              // how one dimension gates
    Direction Direction
    Gate      GateMode              // "" ⇒ total
    Tolerance float64
}

type FileGrowth struct {           // one file whose offender count rose
    File        string
    Base        float64
    Current     float64
    RenamedFrom string              // base path git says this file was renamed from, "" otherwise
}

type GuardViolation struct {        // one metric that worsened between two snapshots
    ID        string
    Direction Direction
    Base      float64
    Current   float64
    Tolerance float64
    Files     []FileGrowth          // per-file rises behind a per-file-count violation
}                                   // String() renders "<id>: <base> → <cur>" with the
                                    // file detail appended in parentheses when present

func Worse(d Direction, base, cur, tolerance float64) bool
func Better(d Direction, base, cur float64) bool
func OffenderCountsByFile(breakdown map[string]float64) map[string]float64
func RegressionsOf(spec GateSpec, base, cur MetricSample) []string
func OrphanedMetrics(dimensionIDs []string, baseline map[string]Metric) []string
func GuardViolations(base, pr map[string]Metric) (violations []GuardViolation, removed []string)
func GuardViolationsWithGates(base, pr map[string]Metric, gates map[string]GateMode) (violations []GuardViolation, removed []string)
func GuardViolationsWithGatesAndRenames(base, pr map[string]Metric, gates map[string]GateMode, renames map[string]string) (violations []GuardViolation, removed []string)
func RedefinedMetrics(base, current map[string]Metric) []string
func SnapshotShapeErrors(parsed any) []string   // parsed = json.Unmarshal into any
func ImprovementNotice(improvedIDs []string, onCI bool) string // "" when not applicable
func FormatNumber(v float64) string             // minimal decimal notation
```

`GuardViolations` treats a metric with empty `Direction` as
`lower-is-better` (the conservative default for hand-crafted snapshots) and honors
the metric's own recorded `Tolerance`. Violations are reported in sorted id order;
`removed` is sorted. `OrphanedMetrics` returns sorted ids.

`gates` is the configured gate mode per metric id (the caller reads the working
tree's config; a snapshot records no gate). On a `per-file-count` gate a metric
whose scalar did not worsen is still a violation when any file's summed offender
count rose — the same per-file predicate `check`, `record` and `--since` read,
with no tolerance — and `Files` carries those rises, capped at 10 entries with
the rest summarized as `and N more`. An absent id or any other gate mode
compares the scalar alone. `GuardViolations` is `GuardViolationsWithGates` with
no gate map: the same scalar-only comparison it has always been.

`GuardViolationsWithGatesAndRenames` adds `renames`: git's detected renames for
the compared range, as base path → the path the same file has in pr, both
relative to the config directory (the form breakdown keys use). A per-file-count
count recorded against a rename source is compared as the count for its
destination, and `Files` names the source in `RenamedFrom`. The mapping is
conservative: a count is moved only when the pair is 1:1, the destination has no
count of its own, and the source is present — any other pair is skipped and the
paths are compared as they are. Total and per-key-value gates ignore `renames`
entirely. `GuardViolationsWithGates` is this function with no rename map.
