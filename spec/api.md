Part of the pawl engine contract. See [spec/README.md](README.md).

## Public Go API (package `pawl`)

The pure comparison core is exported so tests (and future embedders) hit the same
judgment the CLI uses — one source of truth for "did this get worse":

```go
type Direction string   // "lower-is-better" | "higher-is-better"
type GateMode string    // "total" | "per-file-count" | "per-key-value"

type Metric struct {
    Direction Direction          `json:"direction"`
    Value     float64            `json:"value"`
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

type GuardViolation struct {        // one metric that worsened between two snapshots
    ID        string
    Direction Direction
    Base      float64
    Current   float64
    Tolerance float64
}                                   // String() renders "<id>: <base> → <cur>"

func Worse(d Direction, base, cur, tolerance float64) bool
func Better(d Direction, base, cur float64) bool
func OffenderCountsByFile(breakdown map[string]float64) map[string]float64
func RegressionsOf(spec GateSpec, base, cur MetricSample) []string
func OrphanedMetrics(dimensionIDs []string, baseline map[string]Metric) []string
func GuardViolations(base, pr map[string]Metric) (violations []GuardViolation, removed []string)
func SnapshotShapeErrors(parsed any) []string   // parsed = json.Unmarshal into any
func ImprovementNotice(improvedIDs []string, onCI bool) string // "" when not applicable
func FormatNumber(v float64) string             // minimal decimal notation
```

`GuardViolations` treats a metric with empty `Direction` as
`lower-is-better` (the conservative default for hand-crafted snapshots) and honors
the metric's own recorded `Tolerance`. Violations are reported in sorted id order;
`removed` is sorted. `OrphanedMetrics` returns sorted ids.
