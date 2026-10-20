package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"footprint-serverful/ent"
	"footprint-serverful/internal/model"
)

var (
	ErrProjectNotFound = errors.New("project not found")
	ErrProjectRange    = errors.New("endTime must be at or after startTime")
)

type Store struct {
	client *ent.Client
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("cannot connect to PostgreSQL")
	}
	return &Store{client: ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.client.Schema.Create(ctx)
}

func (s *Store) Close() error {
	return s.client.Close()
}

func withTx[T any](ctx context.Context, client *ent.Client, options *sql.TxOptions, fn func(*ent.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := client.BeginTx(ctx, options)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	result, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit transaction: %w", err)
	}
	return result, nil
}

func locationJSON(row *ent.Location) model.Location {
	return model.Location{
		ID: row.ID, Longitude: row.Longitude, Latitude: row.Latitude,
		City: row.City, Device: row.Device, Timestamp: row.Timestamp,
		Timezone: row.Timezone, Quality: qualityJSON(row.Edges.Quality),
	}
}

func qualityJSON(q *ent.LocationQuality) model.Quality {
	if q == nil {
		return model.Quality{Status: "unassessed", Reason: "not_analyzed"}
	}
	return model.Quality{Status: string(q.Status), Reason: q.Reason, Version: &q.AlgorithmVersion,
		CheckedAt: &q.CheckedAt, Evidence: q.Evidence}
}

func projectJSON(p *ent.Project) model.Project {
	return model.Project{ID: p.ID, Name: p.Name, StartTime: p.StartTime, EndTime: p.EndTime}
}
