package trajectory

import "math"

const (
	radians     = math.Pi / 180
	earthRadius = 6_371_008.8
)

func valid(p Point) bool {
	return !math.IsNaN(p.Latitude) && !math.IsNaN(p.Longitude) &&
		math.Abs(p.Latitude) <= 90 && math.Abs(p.Longitude) <= 180 &&
		p.Timestamp >= 0 && p.Timestamp <= 8_640_000_000_000_000
}

func Distance(a, b Point) float64 {
	lat1, lat2 := a.Latitude*radians, b.Latitude*radians
	dlat, dlon := lat2-lat1, (b.Longitude-a.Longitude)*radians
	h := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dlon/2), 2)
	return 2 * earthRadius * math.Asin(math.Sqrt(max(0, min(1, h))))
}

func MatchHistory(point Point, history []Point, complete bool) *HistoricalMatch {
	if point.Device == "" || !valid(point) {
		return nil
	}
	var nearest *Point
	distance := math.Inf(1)
	for _, previous := range history {
		if previous.ID == point.ID || previous.Device != point.Device || !valid(previous) ||
			point.Timestamp-previous.Timestamp < HistoryAgeSeconds*1000 {
			continue
		}
		meters := Distance(point, previous)
		if meters > historyRadius {
			continue
		}
		if meters < distance || (meters == distance && (nearest == nil || previous.Timestamp > nearest.Timestamp ||
			(previous.Timestamp == nearest.Timestamp && previous.ID < nearest.ID))) {
			nearest, distance = &previous, meters
		}
	}
	if nearest == nil {
		return nil
	}
	return &HistoricalMatch{
		PointID: point.ID, PreviousID: nearest.ID, PreviousTimestamp: nearest.Timestamp,
		DistanceMeters: distance, AgeSeconds: float64(point.Timestamp-nearest.Timestamp) / 1000,
		SearchComplete: complete,
	}
}

type Bounds struct {
	MinLatitude, MaxLatitude float64
	LongitudeRanges          [][2]float64
}

func HistoryBounds(point Point) Bounds {
	angle := historyRadius / earthRadius
	latitudeDelta := angle / radians
	b := Bounds{MinLatitude: max(-90, point.Latitude-latitudeDelta), MaxLatitude: min(90, point.Latitude+latitudeDelta)}
	delta := 180.0
	if b.MinLatitude != -90 && b.MaxLatitude != 90 {
		delta = math.Asin(math.Sin(angle)/math.Cos(point.Latitude*radians)) / radians
	}
	low, high := point.Longitude-delta, point.Longitude+delta
	switch {
	case delta == 180:
		b.LongitudeRanges = [][2]float64{{-180, 180}}
	case low < -180:
		b.LongitudeRanges = [][2]float64{{-180, high}, {low + 360, 180}}
	case high > 180:
		b.LongitudeRanges = [][2]float64{{low, 180}, {-180, high - 360}}
	default:
		b.LongitudeRanges = [][2]float64{{low, high}}
	}
	return b
}

// Interpolate on the sphere so date-line crossings follow the short arc.
func expectedPosition(a, b Point, timestamp int64) *Point {
	vector := func(p Point) [3]float64 {
		return [3]float64{math.Cos(p.Latitude*radians) * math.Cos(p.Longitude*radians),
			math.Cos(p.Latitude*radians) * math.Sin(p.Longitude*radians), math.Sin(p.Latitude * radians)}
	}
	x, y := vector(a), vector(b)
	angle := math.Acos(max(-1, min(1, x[0]*y[0]+x[1]*y[1]+x[2]*y[2])))
	if angle > math.Pi-1e-6 {
		return nil
	}
	fraction := float64(timestamp-a.Timestamp) / float64(b.Timestamp-a.Timestamp)
	u, v := 1-fraction, fraction
	if angle >= 1e-8 {
		u = math.Sin((1-fraction)*angle) / math.Sin(angle)
		v = math.Sin(fraction*angle) / math.Sin(angle)
	}
	z := [3]float64{}
	for i := range z {
		z[i] = u*x[i] + v*y[i]
	}
	return &Point{Longitude: math.Atan2(z[1], z[0]) / radians, Latitude: math.Atan2(z[2], math.Hypot(z[0], z[1])) / radians}
}

func makeEdge(a, b Point) Edge {
	e := Edge{Device: a.Device, BeforeID: a.ID, AfterID: b.ID,
		Seconds: float64(b.Timestamp-a.Timestamp) / 1000, DistanceMeters: Distance(a, b)}
	if e.Seconds > 0 {
		speed := e.DistanceMeters / e.Seconds * 3.6
		e.SpeedKmh = &speed
	}
	return e
}
