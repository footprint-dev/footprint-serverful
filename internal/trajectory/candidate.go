package trajectory

import (
	"math"
	"slices"
)

// The longest speed-compatible path keeps both anchors and as many original
// observations as possible. Distance breaks ties, as in the upstream detector.
func compatiblePath(points []Point, threshold, gap float64) []Point {
	lengths := make([]int, len(points))
	distances := make([]float64, len(points))
	previous := make([]int, len(points))
	for i := range points {
		distances[i], previous[i] = math.Inf(1), -1
	}
	lengths[0], distances[0] = 1, 0
	for i := 1; i < len(points); i++ {
		for j := 0; j < i; j++ {
			if lengths[j] == 0 {
				continue
			}
			e := makeEdge(points[j], points[i])
			if e.Seconds <= 0 || e.Seconds > gap || *e.SpeedKmh > threshold {
				continue
			}
			length, distance := lengths[j]+1, distances[j]+e.DistanceMeters
			if length > lengths[i] || (length == lengths[i] && distance < distances[i]) {
				lengths[i], distances[i], previous[i] = length, distance, j
			}
		}
	}
	if lengths[len(points)-1] == 0 {
		return nil
	}
	path := []Point{}
	for i := len(points) - 1; i >= 0; i = previous[i] {
		path = append(path, points[i])
	}
	slices.Reverse(path)
	return path
}

func candidate(points []Point, start, end, pass int) *Excursion {
	a, b := points[start-1], points[end+1]
	span := float64(b.Timestamp-a.Timestamp) / 1000
	window := points[start-1 : end+2]
	longReturn := span > maxGapSeconds
	for i := 1; i < len(window); i++ {
		longReturn = longReturn || float64(window[i].Timestamp-window[i-1].Timestamp)/1000 > maxGapSeconds
	}
	gap := float64(maxGapSeconds)
	if longReturn {
		gap = maxReturnSeconds
	}
	if span <= 0 || span > gap {
		return nil
	}
	entry, exit, bypass := makeEdge(a, points[start]), makeEdge(points[end], b), makeEdge(a, b)
	if entry.SpeedKmh == nil || exit.SpeedKmh == nil || bypass.SpeedKmh == nil {
		return nil
	}
	if longReturn && (bypass.DistanceMeters > maxReturnDistance || max(*entry.SpeedKmh, *exit.SpeedKmh) <= hardSpeed) {
		return nil
	}
	bridgeThreshold := max(minSpeed, *bypass.SpeedKmh*contextFactor+30)
	if min(*entry.SpeedKmh, *exit.SpeedKmh) <= bridgeThreshold {
		return nil
	}
	contexts := []Edge{}
	if start >= 2 {
		contexts = append(contexts, makeEdge(points[start-2], a))
	}
	if end+2 < len(points) {
		contexts = append(contexts, makeEdge(b, points[end+2]))
	}
	contextCap := max(350, *bypass.SpeedKmh*contextFactor+30)
	trusted, localSpeed := 0, 0.0
	for _, e := range contexts {
		if e.Seconds <= 0 || e.Seconds > gap {
			continue
		}
		if *e.SpeedKmh > contextCap {
			return nil
		}
		if !longReturn || e.DistanceMeters <= maxReturnDistance {
			trusted++
			localSpeed += *e.SpeedKmh
		}
	}
	if longReturn && trusted == 0 {
		return nil
	}
	if trusted > 0 {
		localSpeed /= float64(trusted)
	}
	threshold := max(bridgeThreshold, localSpeed*contextFactor+30)
	if min(*entry.SpeedKmh, *exit.SpeedKmh) <= threshold {
		return nil
	}
	kept := compatiblePath(window, threshold, gap)
	if len(kept) == 0 || len(kept) == len(window) {
		return nil
	}
	retained := make(map[int64]bool, len(kept))
	for _, p := range kept {
		retained[p.ID] = true
	}
	rawDistance, maxDeviation := 0.0, 0.0
	pathIndex := 0
	bad := []Point{}
	for i := start; i <= end+1; i++ {
		segment := makeEdge(points[i-1], points[i])
		if segment.Seconds <= 0 || segment.Seconds > gap {
			return nil
		}
		rawDistance += segment.DistanceMeters
		if i > end {
			break
		}
		if retained[points[i].ID] {
			continue
		}
		for kept[pathIndex+1].Timestamp < points[i].Timestamp {
			pathIndex++
		}
		predicted := expectedPosition(kept[pathIndex], kept[pathIndex+1], points[i].Timestamp)
		if predicted == nil {
			return nil
		}
		deviation := Distance(points[i], *predicted)
		maxDeviation = max(maxDeviation, deviation)
		if deviation < minDeviation {
			return nil
		}
		bad = append(bad, points[i])
	}
	if len(bad) == 0 || bad[0].ID != points[start].ID || bad[len(bad)-1].ID != points[end].ID {
		return nil
	}
	ratio := rawDistance / max(bypass.DistanceMeters, 100)
	if ratio < minDetourRatio {
		return nil
	}
	c := &Excursion{
		PointIDs: IDs(bad), KeptInteriorIDs: IDs(kept[1 : len(kept)-1]), Device: a.Device,
		BeforeID: a.ID, AfterID: b.ID, Confidence: "review", Reason: "return_spike",
		EntrySpeedKmh: *entry.SpeedKmh, ExitSpeedKmh: *exit.SpeedKmh, BypassSpeedKmh: *bypass.SpeedKmh,
		ThresholdKmh: threshold, MaxDeviationMeters: maxDeviation, DetourRatio: ratio,
		SpanSeconds: span, TrustedContextSides: trusted, Pass: pass, HistoricalMatches: []HistoricalMatch{},
	}
	if min(*entry.SpeedKmh, *exit.SpeedKmh) >= max(highSpeed, threshold) && trusted == 2 {
		c.Confidence = "high"
	}
	if len(bad) > 1 {
		c.Reason = "excursion_burst"
	}
	if longReturn {
		c.Reason = "long_" + c.Reason
	}
	return c
}

func candidates(points []Point, pass int) []Excursion {
	result := []Excursion{}
	for start := 1; start < len(points)-1; start++ {
		for end := start; end < min(len(points)-1, start+MaxBurstPoints); end++ {
			if c := candidate(points, start, end, pass); c != nil {
				result = append(result, *c)
			}
		}
	}
	score := func(c Excursion) float64 {
		return min(c.EntrySpeedKmh, c.ExitSpeedKmh) / c.ThresholdKmh *
			math.Log1p(c.MaxDeviationMeters/minDeviation) * math.Log1p(c.DetourRatio) / math.Sqrt(float64(len(c.PointIDs)))
	}
	slices.SortStableFunc(result, func(a, b Excursion) int {
		if score(a) != score(b) {
			if score(a) > score(b) {
				return -1
			}
			return 1
		}
		if a.BeforeID != b.BeforeID {
			return compareID(a.BeforeID, b.BeforeID)
		}
		return compareID(a.AfterID, b.AfterID)
	})
	return result
}

func selectCandidates(candidates []Excursion, protected map[int64]bool) []Excursion {
	selected := []Excursion{}
	flagged, anchors := map[int64]bool{}, map[int64]bool{}
	for _, c := range candidates {
		support := append([]int64{c.BeforeID, c.AfterID}, c.KeptInteriorIDs...)
		if intersects(support, flagged) || intersects(c.PointIDs, flagged) ||
			intersects(c.PointIDs, anchors) || intersects(c.PointIDs, protected) {
			continue
		}
		selected = append(selected, c)
		for _, id := range c.PointIDs {
			flagged[id] = true
		}
		for _, id := range support {
			anchors[id] = true
		}
	}
	return selected
}

func compareID(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
