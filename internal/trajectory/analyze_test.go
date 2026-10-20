package trajectory

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

func point(id int64, longitude, seconds float64) Point {
	return Point{ID: id, Device: "phone", Timestamp: int64(seconds * 1000), Longitude: longitude, Latitude: 30}
}

func flagged(a Analysis) []int64 {
	ids := []int64{}
	for _, c := range a.Excursions {
		ids = append(ids, c.PointIDs...)
	}
	slices.Sort(ids)
	return ids
}

func TestExcursions(t *testing.T) {
	tests := []struct {
		name   string
		points []Point
		ids    []int64
		reason string
	}{
		{"return spike", []Point{point(1, 120, 0), point(2, 120, 30), point(3, 121, 60), point(4, 120, 90), point(5, 120, 120)}, []int64{3}, "return_spike"},
		{"long return", []Point{point(1, 120, 0), point(2, 120, 30), point(3, 112, 3530), point(4, 120, 3560), point(5, 120, 3590)}, []int64{3}, "long_return_spike"},
		{"plausible flight return", []Point{point(1, 120, 0), point(2, 120, 30), point(3, 112, 3530), point(4, 120, 7030), point(5, 120, 7060)}, []int64{}, ""},
		{"slow return", []Point{point(1, 120, 0), point(2, 120, 300), point(3, 120.02, 600), point(4, 120, 900), point(5, 120, 1200)}, []int64{}, ""},
		{"unsupported long return", []Point{point(1, 120, 0), point(2, 120, 30), point(3, 112, 3530), point(4, 120, 11000), point(5, 120, 11030)}, []int64{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.points)
			result := Analyze(tt.points)
			if !slices.Equal(flagged(result), tt.ids) {
				t.Fatalf("flagged %v, want %v", flagged(result), tt.ids)
			}
			if tt.reason != "" && (result.Excursions[0].Reason != tt.reason || result.Excursions[0].Confidence != "high") {
				t.Fatalf("unexpected evidence: %+v", result.Excursions[0])
			}
			if !reflect.DeepEqual(before, tt.points) {
				t.Fatal("detector modified observations")
			}
		})
	}
}

func TestRetainsInteriorPointInBurst(t *testing.T) {
	points := []Point{point(1, 120, 0), point(2, 120, 30), point(3, 120, 60), point(4, 120, 90),
		point(5, 120, 120), point(6, 120, 150), point(7, 120, 180), point(8, 120, 210)}
	for _, i := range []int{2, 3, 5} {
		points[i].Latitude = 26
	}
	if got := flagged(Analyze(points)); !slices.Equal(got, []int64{3, 4, 6}) {
		t.Fatalf("flagged %v, want [3 4 6]", got)
	}
}

func TestFlightNoiseAndDeviceIsolation(t *testing.T) {
	for _, scenario := range []string{"flight", "noise", "separate devices"} {
		t.Run(scenario, func(t *testing.T) {
			points := []Point{}
			for i := range 10 {
				p := point(int64(i+1), 100+float64(i)*0.1, float64(i)*40)
				if scenario == "noise" {
					p.Longitude = 120 + float64(i%2)*0.0005
				}
				if scenario == "separate devices" {
					if i%2 == 0 {
						p.Device, p.Longitude = "watch", 0
					} else {
						p.Longitude = 120
					}
				}
				points = append(points, p)
			}
			result := Analyze(points)
			if len(result.Excursions) != 0 || len(result.UnresolvedJumps) != 0 {
				t.Fatalf("plausible motion flagged: %+v", result)
			}
		})
	}
}

func TestDuplicateAndConflictingObservations(t *testing.T) {
	points := []Point{point(1, 120, 0), point(2, 120, 30), point(3, 121, 60), point(6, 121, 60), point(4, 120, 90), point(5, 120, 120)}
	result := Analyze(points)
	if !reflect.DeepEqual(result.DuplicateGroups, [][]int64{{3, 6}}) || !slices.Equal(flagged(result), []int64{3}) {
		t.Fatalf("unexpected duplicate result: %+v", result)
	}
	points[3].Longitude = 120
	result = Analyze(points)
	if len(result.Excursions) != 0 || !reflect.DeepEqual(result.ConflictingTimestampGroups, [][]int64{{3, 6}}) {
		t.Fatalf("conflicting observations must split inference: %+v", result)
	}
}

func TestHistoricalEvidence(t *testing.T) {
	const day = 86_400
	recent := []Point{point(3, 120, day*2), point(4, 120, day*2+30), point(5, 120.05, day*2+90),
		point(6, 120, day*2+150), point(7, 120, day*2+180)}
	result := Analyze(append([]Point{point(1, 120.05, 0), point(2, 120.05, 30)}, recent...))
	if len(result.Excursions) != 1 {
		t.Fatalf("expected one review candidate: %+v", result)
	}
	c := result.Excursions[0]
	if c.Confidence != "review" || len(c.HistoricalMatches) != 1 || c.HistoricalMatches[0].PreviousID != 2 {
		t.Fatalf("history must not upgrade confidence: %+v", c)
	}
	target := point(20, 120, day*2)
	if MatchHistory(target, []Point{point(1, 120, day*2-1)}, true) != nil {
		t.Fatal("recent observations must not count as historical references")
	}
	other := point(1, 120, 0)
	other.Device = "watch"
	if MatchHistory(target, []Point{other}, true) != nil {
		t.Fatal("reference must belong to the same device")
	}
}

func TestSphericalHistoryBounds(t *testing.T) {
	a, b := point(1, -179.9999, 172800), point(2, 179.9999, 0)
	a.Latitude, b.Latitude = 0, 0
	match := MatchHistory(a, []Point{b}, false)
	if match == nil || match.DistanceMeters >= 30 || match.SearchComplete || len(HistoryBounds(a).LongitudeRanges) != 2 {
		t.Fatal("date-line history match or truncation metadata was lost")
	}
	a.Latitude, b.Latitude, a.Longitude, b.Longitude = 90, 90, 150, -30
	if Distance(a, b) > 0.01 || !reflect.DeepEqual(HistoryBounds(a).LongitudeRanges, [][2]float64{{-180, 180}}) {
		t.Fatal("polar bounds must cover every longitude")
	}
	b.Latitude = math.NaN()
	if MatchHistory(a, []Point{b}, true) != nil {
		t.Fatal("invalid observations cannot support history")
	}
}
