package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"time"

	"footprint-serverful/ent"
	"footprint-serverful/ent/deviceanalysis"
	"footprint-serverful/ent/location"
	"footprint-serverful/ent/locationquality"
	"footprint-serverful/internal/model"
	"footprint-serverful/internal/trajectory"
)

const (
	windowSide = 64
	updateSide = trajectory.MaxBurstPoints*trajectory.MaxPasses + 3
)

type windowSnapshot struct {
	revision        int64
	rows            []*ent.Location
	targetIndex     int
	truncatedBefore bool
	truncatedAfter  bool
}

func trackPoint(row *ent.Location) trajectory.Point {
	return trajectory.Point{ID: row.ID, Device: row.Device, Timestamp: row.Timestamp,
		Longitude: row.Longitude, Latitude: row.Latitude}
}

func (s *Store) loadWindow(ctx context.Context, target *ent.Location) (windowSnapshot, error) {
	return withTx(ctx, s.client, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true},
		func(tx *ent.Tx) (windowSnapshot, error) {
			state, err := tx.DeviceAnalysis.Get(ctx, target.Device)
			if err != nil {
				return windowSnapshot{}, err
			}
			left, err := tx.Location.Query().Where(location.DeviceEQ(target.Device),
				location.TimestampLTE(target.Timestamp),
				location.Or(location.TimestampLT(target.Timestamp), location.IDLTE(target.ID))).
				Order(ent.Desc(location.FieldTimestamp, location.FieldID)).Limit(windowSide + 2).WithQuality().All(ctx)
			if err != nil {
				return windowSnapshot{}, err
			}
			right, err := tx.Location.Query().Where(location.DeviceEQ(target.Device),
				location.TimestampGTE(target.Timestamp),
				location.Or(location.TimestampGT(target.Timestamp), location.IDGT(target.ID))).
				Order(ent.Asc(location.FieldTimestamp, location.FieldID)).Limit(windowSide + 1).WithQuality().All(ctx)
			if err != nil {
				return windowSnapshot{}, err
			}
			if len(left) == 0 || left[0].ID != target.ID {
				return windowSnapshot{}, errors.New("analysis snapshot is incomplete")
			}
			snapshot := windowSnapshot{revision: state.Revision,
				truncatedBefore: len(left) > windowSide+1, truncatedAfter: len(right) > windowSide}
			left, right = left[:min(len(left), windowSide+1)], right[:min(len(right), windowSide)]
			slices.Reverse(left)
			snapshot.rows = append(left, right...)
			snapshot.targetIndex = len(left) - 1
			return snapshot, nil
		})
}

func annotate(snapshot windowSnapshot) (trajectory.Analysis, []*ent.LocationQuality, error) {
	points := make([]trajectory.Point, len(snapshot.rows))
	timestamps := []int64{}
	for i, row := range snapshot.rows {
		points[i] = trackPoint(row)
		if len(timestamps) == 0 || timestamps[len(timestamps)-1] != row.Timestamp {
			timestamps = append(timestamps, row.Timestamp)
		}
	}
	analysis := trajectory.Analyze(points)
	checkedAt := time.Now().UnixMilli()
	byID := make(map[int64]*ent.LocationQuality, len(points))
	pendingSince := timestamps[max(0, len(timestamps)-2)]
	for _, p := range points {
		q := &ent.LocationQuality{LocationID: p.ID, Status: locationquality.StatusRetained,
			Reason: "no_excursion_detected", AlgorithmVersion: trajectory.Version,
			Revision: snapshot.revision, CheckedAt: checkedAt, Evidence: json.RawMessage(`{}`)}
		if p.Timestamp >= pendingSince {
			q.Status, q.Reason = locationquality.StatusPending, "awaiting_context"
		}
		byID[p.ID] = q
	}
	var encodingErr error
	set := func(ids []int64, status locationquality.Status, reason string, evidence any) {
		raw, err := json.Marshal(evidence)
		if err != nil {
			encodingErr = err
			return
		}
		for _, id := range ids {
			if q := byID[id]; q != nil {
				q.Status, q.Reason, q.Evidence = status, reason, raw
			}
		}
	}
	for _, e := range analysis.Gaps {
		set([]int64{e.BeforeID, e.AfterID}, locationquality.StatusUncertain, "sampling_gap", e)
	}
	for _, e := range analysis.UnresolvedJumps {
		set([]int64{e.BeforeID, e.AfterID}, locationquality.StatusUncertain, "unresolved_jump", e)
	}
	for _, ids := range analysis.ConflictingTimestampGroups {
		set(ids, locationquality.StatusUncertain, "conflicting_timestamp", map[string]any{"pointIds": ids})
	}
	set(analysis.InvalidPointIDs, locationquality.StatusUncertain, "invalid_observation", struct{}{})
	for _, c := range analysis.Excursions {
		status := locationquality.StatusReview
		if c.Confidence == "high" {
			status = locationquality.StatusOutlier
		}
		set(c.PointIDs, status, c.Reason, c)
	}
	for _, ids := range analysis.DuplicateGroups {
		for _, id := range ids[1:] {
			copy := *byID[ids[0]]
			copy.LocationID = id
			byID[id] = &copy
		}
	}
	first, end := max(0, snapshot.targetIndex-updateSide), min(len(points), snapshot.targetIndex+updateSide+1)
	affected := map[int64]bool{}
	for _, p := range points[first:end] {
		affected[p.ID] = true
	}
	for _, groups := range [][][]int64{analysis.DuplicateGroups, analysis.ConflictingTimestampGroups} {
		for _, ids := range groups {
			include := false
			for _, id := range ids {
				include = include || affected[id]
			}
			if include {
				for _, id := range ids {
					affected[id] = true
				}
			}
		}
	}
	for i := first; i < end; i++ {
		if (snapshot.truncatedBefore && i < updateSide) || (snapshot.truncatedAfter && i >= len(points)-updateSide) {
			set([]int64{points[i].ID}, locationquality.StatusUncertain, "truncated_context", struct{}{})
		}
	}
	labels := []*ent.LocationQuality{}
	for _, p := range points {
		if affected[p.ID] {
			labels = append(labels, byID[p.ID])
		}
	}
	return analysis, labels, encodingErr
}

func labelChanged(current, previous *ent.LocationQuality) bool {
	if previous == nil || current.Status != previous.Status || current.Reason != previous.Reason ||
		current.AlgorithmVersion != previous.AlgorithmVersion {
		return true
	}
	var a, b any
	if json.Unmarshal(current.Evidence, &a) != nil || json.Unmarshal(previous.Evidence, &b) != nil {
		return true
	}
	return !reflect.DeepEqual(a, b)
}

func (s *Store) saveLabels(ctx context.Context, device string, revision int64, labels []*ent.LocationQuality) (bool, error) {
	return withTx(ctx, s.client, nil, func(tx *ent.Tx) (bool, error) {
		state, err := tx.DeviceAnalysis.Query().Where(deviceanalysis.IDEQ(device)).ForUpdate().Only(ctx)
		if err != nil || state.Revision != revision {
			return false, err
		}
		if len(labels) == 0 {
			return true, nil
		}
		builders := make([]*ent.LocationQualityCreate, len(labels))
		for i, q := range labels {
			builders[i] = tx.LocationQuality.Create().SetLocationID(q.LocationID).SetStatus(q.Status).
				SetReason(q.Reason).SetAlgorithmVersion(q.AlgorithmVersion).SetRevision(q.Revision).
				SetCheckedAt(q.CheckedAt).SetEvidence(q.Evidence)
		}
		err = tx.LocationQuality.CreateBulk(builders...).OnConflictColumns(locationquality.FieldLocationID).
			UpdateNewValues().Exec(ctx)
		return err == nil, err
	})
}

func (s *Store) analyzeRecent(ctx context.Context, target *ent.Location) (model.Quality, model.Analysis) {
	result := model.Analysis{State: "deferred"}
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := s.loadWindow(ctx, target)
		if err != nil {
			break
		}
		result.WindowPoints = len(snapshot.rows)
		analysis, labels, err := annotate(snapshot)
		if err != nil {
			break
		}
		if err := s.addHistory(ctx, snapshot, analysis, labels); err != nil {
			for _, q := range labels {
				if isCandidate(q) {
					// Keep primary geometry even when supplementary history fails.
					_ = setHistory(q, []trajectory.HistoricalMatch{}, &historicalSearch{Reason: "unavailable"})
				}
			}
		}
		previous := map[int64]*ent.LocationQuality{}
		for _, row := range snapshot.rows {
			previous[row.ID] = row.Edges.Quality
		}
		changed := []*ent.LocationQuality{}
		for _, q := range labels {
			if labelChanged(q, previous[q.LocationID]) {
				changed = append(changed, q)
			}
		}
		accepted, err := s.saveLabels(ctx, target.Device, snapshot.revision, changed)
		if err != nil {
			break
		}
		if !accepted {
			continue
		}
		for _, q := range labels {
			if q.LocationID == target.ID {
				if !labelChanged(q, previous[q.LocationID]) {
					q = previous[q.LocationID]
				}
				return qualityJSON(q), model.Analysis{State: "updated", WindowPoints: len(snapshot.rows), UpdatedPoints: len(changed)}
			}
		}
	}
	slog.WarnContext(ctx, "location analysis deferred", "location_id", target.ID)
	q, err := s.client.LocationQuality.Query().Where(locationquality.LocationIDEQ(target.ID)).Only(ctx)
	if err == nil {
		return qualityJSON(q), result
	}
	// The ingest transaction already committed. Do not invite duplicate retries.
	version := trajectory.Version
	return model.Quality{Status: "pending", Reason: "awaiting_context", Version: &version}, result
}
