package trajectory

import "slices"

// Analyze ports the bounded excursion-v3 detector. Findings annotate original
// observations; virtual removals only affect this calculation.
func Analyze(input []Point) Analysis {
	a := Analysis{
		InvalidPointIDs: []int64{}, DuplicateGroups: [][]int64{}, ConflictingTimestampGroups: [][]int64{},
		Excursions: []Excursion{}, UnresolvedJumps: []Edge{}, Gaps: []Edge{},
	}
	groups := map[string][]Point{}
	devices := []string{}
	for _, p := range input {
		if !valid(p) {
			a.InvalidPointIDs = append(a.InvalidPointIDs, p.ID)
			continue
		}
		if _, exists := groups[p.Device]; !exists {
			devices = append(devices, p.Device)
		}
		groups[p.Device] = append(groups[p.Device], p)
	}
	for _, device := range devices {
		group := groups[device]
		slices.SortFunc(group, func(a, b Point) int {
			if a.Timestamp != b.Timestamp {
				return compareID(a.Timestamp, b.Timestamp)
			}
			return compareID(a.ID, b.ID)
		})
		tracks := [][]Point{{}}
		for i := 0; i < len(group); {
			end := i + 1
			for end < len(group) && group[end].Timestamp == group[i].Timestamp {
				end++
			}
			same := group[i:end]
			duplicate := true
			for _, p := range same {
				duplicate = duplicate && p.Longitude == same[0].Longitude && p.Latitude == same[0].Latitude
			}
			switch {
			case len(same) == 1:
				tracks[len(tracks)-1] = append(tracks[len(tracks)-1], same[0])
			case duplicate:
				a.DuplicateGroups = append(a.DuplicateGroups, IDs(same))
				tracks[len(tracks)-1] = append(tracks[len(tracks)-1], same[0])
			default:
				a.ConflictingTimestampGroups = append(a.ConflictingTimestampGroups, IDs(same))
				tracks = append(tracks, []Point{})
			}
			i = end
		}
		for _, points := range tracks {
			analyzeTrack(&a, points, device)
		}
	}
	excluded := ExcludedIDs(a)
	history, byID := []Point{}, map[int64]Point{}
	for _, p := range input {
		byID[p.ID] = p
		if !excluded[p.ID] {
			history = append(history, p)
		}
	}
	for i := range a.Excursions {
		c := &a.Excursions[i]
		ids := slices.Clone(c.PointIDs)
		set := map[int64]bool{}
		for _, id := range ids {
			set[id] = true
		}
		for _, group := range a.DuplicateGroups {
			if intersects(group, set) {
				for _, id := range group {
					if !set[id] {
						ids = append(ids, id)
						set[id] = true
					}
				}
			}
		}
		for _, id := range ids {
			if match := MatchHistory(byID[id], history, true); match != nil {
				c.HistoricalMatches = append(c.HistoricalMatches, *match)
			}
		}
	}
	return a
}

func analyzeTrack(a *Analysis, points []Point, device string) {
	for i := 1; i < len(points); i++ {
		e := makeEdge(points[i-1], points[i])
		if e.Seconds > maxGapSeconds {
			a.Gaps = append(a.Gaps, e)
		}
	}
	working := points
	protected := map[int64]bool{}
	for pass := 1; pass <= MaxPasses; pass++ {
		high := []Excursion{}
		for _, c := range candidates(working, pass) {
			if c.Confidence == "high" {
				high = append(high, c)
			}
		}
		high = selectCandidates(high, protected)
		if len(high) == 0 {
			break
		}
		a.Excursions = append(a.Excursions, high...)
		flagged := map[int64]bool{}
		for _, c := range high {
			for _, id := range append([]int64{c.BeforeID, c.AfterID}, c.KeptInteriorIDs...) {
				protected[id] = true
			}
			for _, id := range c.PointIDs {
				flagged[id] = true
			}
		}
		next := []Point{}
		for _, p := range working {
			if !flagged[p.ID] {
				next = append(next, p)
			}
		}
		working = next
	}
	a.Excursions = append(a.Excursions, selectCandidates(candidates(working, MaxPasses+1), protected)...)
	known := map[int64]bool{}
	for _, c := range a.Excursions {
		if c.Device == device {
			for _, id := range c.PointIDs {
				known[id] = true
			}
		}
	}
	for i := 1; i < len(working); i++ {
		e := makeEdge(working[i-1], working[i])
		if e.SpeedKmh != nil && *e.SpeedKmh > hardSpeed && !known[e.BeforeID] && !known[e.AfterID] {
			a.UnresolvedJumps = append(a.UnresolvedJumps, e)
		}
	}
}
