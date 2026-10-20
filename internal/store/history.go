package store

import (
	"context"
	"encoding/json"
	"slices"

	"footprint-serverful/ent"
	"footprint-serverful/ent/location"
	"footprint-serverful/ent/locationquality"
	"footprint-serverful/ent/predicate"
	"footprint-serverful/internal/trajectory"
)

type historicalSearch struct {
	Complete           bool   `json:"complete"`
	Reason             string `json:"reason,omitempty"`
	CandidateLimit     *int   `json:"candidateLimit,omitempty"`
	ReturnedCandidates *int   `json:"returnedCandidates,omitempty"`
}

type excursionEvidence struct {
	trajectory.Excursion
	HistoricalSearch *historicalSearch `json:"historicalSearch,omitempty"`
}

func isCandidate(q *ent.LocationQuality) bool {
	return q != nil && (q.Status == locationquality.StatusOutlier || q.Status == locationquality.StatusReview)
}

func setHistory(q *ent.LocationQuality, matches []trajectory.HistoricalMatch, search *historicalSearch) error {
	var evidence excursionEvidence
	if err := json.Unmarshal(q.Evidence, &evidence); err != nil {
		return err
	}
	evidence.HistoricalMatches, evidence.HistoricalSearch = matches, search
	raw, err := json.Marshal(evidence)
	if err == nil {
		q.Evidence = raw
	}
	return err
}

func reuseHistory(current, previous *ent.LocationQuality, excluded map[int64]bool) bool {
	if !isCandidate(previous) || previous.AlgorithmVersion != trajectory.Version || previous.Reason != current.Reason {
		return false
	}
	var old, next excursionEvidence
	if json.Unmarshal(previous.Evidence, &old) != nil || json.Unmarshal(current.Evidence, &next) != nil {
		return false
	}
	if old.BeforeID != next.BeforeID || old.AfterID != next.AfterID ||
		!slices.Equal(old.PointIDs, next.PointIDs) || !slices.Equal(old.KeptInteriorIDs, next.KeptInteriorIDs) ||
		old.HistoricalMatches == nil || (old.HistoricalSearch != nil && !old.HistoricalSearch.Complete) {
		return false
	}
	for _, match := range old.HistoricalMatches {
		if !match.SearchComplete || excluded[match.PreviousID] {
			return false
		}
	}
	return setHistory(current, old.HistoricalMatches, old.HistoricalSearch) == nil
}

func (s *Store) addHistory(ctx context.Context, snapshot windowSnapshot, analysis trajectory.Analysis, labels []*ent.LocationQuality) error {
	excluded := trajectory.ExcludedIDs(analysis)
	current := map[int64]*ent.LocationQuality{}
	rows, indices := map[int64]*ent.Location{}, map[int64]int{}
	for _, q := range labels {
		current[q.LocationID] = q
		if q.Status != locationquality.StatusRetained {
			excluded[q.LocationID] = true
		}
	}
	for i, row := range snapshot.rows {
		rows[row.ID], indices[row.ID] = row, i
	}
	lateReport := snapshot.rows[len(snapshot.rows)-1].ID != snapshot.rows[snapshot.targetIndex].ID
	candidates := []*ent.LocationQuality{}
	for _, q := range labels {
		if isCandidate(q) && (lateReport || !reuseHistory(q, rows[q.LocationID].Edges.Quality, excluded)) {
			candidates = append(candidates, q)
		}
	}
	distance := func(q *ent.LocationQuality) int {
		n := indices[q.LocationID] - snapshot.targetIndex
		return max(n, -n)
	}
	slices.SortStableFunc(candidates, func(a, b *ent.LocationQuality) int {
		if distance(a) != distance(b) {
			return distance(a) - distance(b)
		}
		if a.LocationID < b.LocationID {
			return -1
		}
		if a.LocationID > b.LocationID {
			return 1
		}
		return 0
	})
	for i, q := range candidates {
		if i >= 2 {
			if err := setHistory(q, []trajectory.HistoricalMatch{}, &historicalSearch{Reason: "candidate_limit"}); err != nil {
				return err
			}
			continue
		}
		point := trackPoint(rows[q.LocationID])
		bounds := trajectory.HistoryBounds(point)
		longitudes := []predicate.Location{}
		for _, r := range bounds.LongitudeRanges {
			longitudes = append(longitudes, location.And(location.LongitudeGTE(r[0]), location.LongitudeLTE(r[1])))
		}
		// Limit raw candidates before examining their quality; dense unreliable
		// history must not turn into an unbounded scan for retained observations.
		history, err := s.client.Location.Query().Where(location.DeviceEQ(point.Device),
			location.LatitudeGTE(bounds.MinLatitude), location.LatitudeLTE(bounds.MaxLatitude),
			location.Or(longitudes...), location.TimestampLTE(point.Timestamp-trajectory.HistoryAgeSeconds*1000)).
			Order(ent.Asc(location.FieldLatitude, location.FieldLongitude, location.FieldTimestamp, location.FieldID)).
			Limit(trajectory.HistoryLimit + 1).WithQuality().All(ctx)
		if err != nil {
			return err
		}
		complete := len(history) <= trajectory.HistoryLimit
		history = history[:min(len(history), trajectory.HistoryLimit)]
		references := []trajectory.Point{}
		for _, row := range history {
			label := row.Edges.Quality
			if local := current[row.ID]; local != nil {
				label = local
			}
			if label != nil && label.Status == locationquality.StatusRetained && !excluded[row.ID] {
				references = append(references, trackPoint(row))
			}
		}
		matches := []trajectory.HistoricalMatch{}
		if match := trajectory.MatchHistory(point, references, complete); match != nil {
			matches = append(matches, *match)
		}
		limit, returned := trajectory.HistoryLimit, len(history)
		if err := setHistory(q, matches, &historicalSearch{Complete: complete, CandidateLimit: &limit, ReturnedCandidates: &returned}); err != nil {
			return err
		}
	}
	return nil
}
