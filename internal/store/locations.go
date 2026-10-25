package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"footprint-serverful/ent"
	"footprint-serverful/ent/deviceanalysis"
	"footprint-serverful/ent/location"
	"footprint-serverful/ent/locationquality"
	"footprint-serverful/ent/predicate"
	"footprint-serverful/internal/model"
	"footprint-serverful/internal/trajectory"
)

func locationQuery(ctx context.Context, client *ent.Client, filter model.LocationFilter) (*ent.LocationQuery, error) {
	conditions := []predicate.Location{}
	if filter.Since != nil {
		conditions = append(conditions, location.TimestampGTE(*filter.Since))
	}
	if filter.Until != nil {
		conditions = append(conditions, location.TimestampLTE(*filter.Until))
	}
	if filter.Device != nil {
		conditions = append(conditions, location.DeviceEQ(*filter.Device))
	}
	if filter.Project != nil {
		p, err := findProject(ctx, client, *filter.Project)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, location.TimestampGTE(p.StartTime), location.TimestampLTE(p.EndTime))
	}
	if filter.Filtered {
		conditions = append(conditions, location.Not(location.HasQualityWith(locationquality.StatusEQ(locationquality.StatusOutlier))))
	}
	return client.Location.Query().Where(conditions...).WithQuality(), nil
}

func (s *Store) LatestLocation(ctx context.Context, filter model.LocationFilter) (*model.Location, error) {
	// Eager loading uses separate queries. Freeze filtering and annotations in
	// one snapshot so a concurrent analysis cannot contradict the chosen view.
	return withTx(ctx, s.client, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true},
		func(tx *ent.Tx) (*model.Location, error) {
			query, err := locationQuery(ctx, tx.Client(), filter)
			if err != nil {
				return nil, err
			}
			row, err := query.Order(ent.Desc(location.FieldTimestamp, location.FieldID)).First(ctx)
			if ent.IsNotFound(err) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			result := locationJSON(row)
			return &result, nil
		})
}

func (s *Store) ListLocations(ctx context.Context, filter model.LocationFilter, limit, offset int, descending bool) (model.LocationPage, error) {
	return withTx(ctx, s.client, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true},
		func(tx *ent.Tx) (model.LocationPage, error) {
			query, err := locationQuery(ctx, tx.Client(), filter)
			if err != nil {
				return model.LocationPage{}, err
			}
			if descending {
				query.Order(ent.Desc(location.FieldTimestamp, location.FieldID))
			} else {
				query.Order(ent.Asc(location.FieldTimestamp, location.FieldID))
			}
			rows, err := query.Limit(limit + 1).Offset(offset).All(ctx)
			if err != nil {
				return model.LocationPage{}, err
			}
			result := model.LocationPage{Locations: []model.Location{}, Pagination: model.Pagination{
				Limit: limit, Offset: offset, HasMore: len(rows) > limit,
			}}
			for _, row := range rows[:min(len(rows), limit)] {
				result.Locations = append(result.Locations, locationJSON(row))
			}
			return result, nil
		})
}

func (s *Store) ReportLocation(ctx context.Context, input model.LocationInput) (model.IngestResult, error) {
	row, err := s.ingest(ctx, input)
	if err != nil {
		return model.IngestResult{}, err
	}
	quality, analysis := s.analyzeRecent(ctx, row)
	result := locationJSON(row)
	result.Quality = quality
	return model.IngestResult{Location: result, Analysis: analysis}, nil
}

func (s *Store) ingest(ctx context.Context, input model.LocationInput) (*ent.Location, error) {
	return withTx(ctx, s.client, nil, func(tx *ent.Tx) (*ent.Location, error) {
		if err := tx.DeviceAnalysis.Create().SetID(input.Device).SetRevision(0).
			OnConflictColumns(deviceanalysis.FieldID).Ignore().Exec(ctx); err != nil {
			return nil, err
		}
		state, err := tx.DeviceAnalysis.Query().Where(deviceanalysis.IDEQ(input.Device)).ForUpdate().Only(ctx)
		if err != nil {
			return nil, err
		}
		revision := state.Revision + 1
		if err := tx.DeviceAnalysis.UpdateOneID(input.Device).SetRevision(revision).Exec(ctx); err != nil {
			return nil, err
		}
		row, err := tx.Location.Create().SetLongitude(input.Longitude).SetLatitude(input.Latitude).
			SetNillableAltitude(input.Altitude).
			SetDevice(input.Device).SetCity(input.City).SetTimestamp(input.Timestamp).SetNillableTimezone(input.Timezone).Save(ctx)
		if err != nil {
			return nil, err
		}
		q, err := tx.LocationQuality.Create().SetLocationID(row.ID).SetStatus(locationquality.StatusPending).
			SetReason("awaiting_context").SetAlgorithmVersion(trajectory.Version).SetRevision(revision).
			SetCheckedAt(time.Now().UnixMilli()).SetEvidence(json.RawMessage(`{}`)).Save(ctx)
		if err != nil {
			return nil, err
		}
		row.Edges.Quality = q
		return row, nil
	})
}
