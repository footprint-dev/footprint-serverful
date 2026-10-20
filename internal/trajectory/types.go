package trajectory

const (
	Version           = "excursion-v3-experimental"
	MaxBurstPoints    = 8
	MaxPasses         = 3
	HistoryLimit      = 64
	HistoryAgeSeconds = 86_400
	historyRadius     = 100
	maxGapSeconds     = 1800
	maxReturnSeconds  = 10_800
	maxReturnDistance = 1000
	minDeviation      = 1000
	minDetourRatio    = 4
	minSpeed          = 150
	highSpeed         = 500
	hardSpeed         = 1500
	contextFactor     = 3
)

type Point struct {
	ID        int64   `json:"id"`
	Device    string  `json:"device"`
	Timestamp int64   `json:"timestamp"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type HistoricalMatch struct {
	PointID           int64   `json:"pointId"`
	PreviousID        int64   `json:"previousId"`
	PreviousTimestamp int64   `json:"previousTimestamp"`
	DistanceMeters    float64 `json:"distanceMeters"`
	AgeSeconds        float64 `json:"ageSeconds"`
	SearchComplete    bool    `json:"searchComplete"`
}

type Excursion struct {
	PointIDs            []int64           `json:"pointIds"`
	KeptInteriorIDs     []int64           `json:"keptInteriorIds"`
	Device              string            `json:"device"`
	BeforeID            int64             `json:"beforeId"`
	AfterID             int64             `json:"afterId"`
	Confidence          string            `json:"confidence"`
	Reason              string            `json:"reason"`
	EntrySpeedKmh       float64           `json:"entrySpeedKmh"`
	ExitSpeedKmh        float64           `json:"exitSpeedKmh"`
	BypassSpeedKmh      float64           `json:"bypassSpeedKmh"`
	ThresholdKmh        float64           `json:"thresholdKmh"`
	MaxDeviationMeters  float64           `json:"maxDeviationMeters"`
	DetourRatio         float64           `json:"detourRatio"`
	SpanSeconds         float64           `json:"spanSeconds"`
	TrustedContextSides int               `json:"trustedContextSides"`
	Pass                int               `json:"pass"`
	HistoricalMatches   []HistoricalMatch `json:"historicalMatches"`
}

type Edge struct {
	Device         string   `json:"device"`
	BeforeID       int64    `json:"beforeId"`
	AfterID        int64    `json:"afterId"`
	Seconds        float64  `json:"seconds"`
	DistanceMeters float64  `json:"distanceMeters"`
	SpeedKmh       *float64 `json:"speedKmh"`
}

type Analysis struct {
	InvalidPointIDs            []int64
	DuplicateGroups            [][]int64
	ConflictingTimestampGroups [][]int64
	Excursions                 []Excursion
	UnresolvedJumps            []Edge
	Gaps                       []Edge
}

func IDs(points []Point) []int64 {
	ids := make([]int64, len(points))
	for i, p := range points {
		ids[i] = p.ID
	}
	return ids
}

func ExcludedIDs(a Analysis) map[int64]bool {
	ids := make(map[int64]bool)
	for _, id := range a.InvalidPointIDs {
		ids[id] = true
	}
	for _, group := range a.ConflictingTimestampGroups {
		for _, id := range group {
			ids[id] = true
		}
	}
	for _, c := range a.Excursions {
		for _, id := range c.PointIDs {
			ids[id] = true
		}
	}
	for _, e := range a.UnresolvedJumps {
		ids[e.BeforeID], ids[e.AfterID] = true, true
	}
	for _, group := range a.DuplicateGroups {
		if intersects(group, ids) {
			for _, id := range group {
				ids[id] = true
			}
		}
	}
	return ids
}

func intersects(ids []int64, set map[int64]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}
