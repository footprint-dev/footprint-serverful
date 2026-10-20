package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	entgo "entgo.io/ent"

	"footprint-serverful/ent"
	"footprint-serverful/ent/deviceanalysis"
	"footprint-serverful/ent/location"
	"footprint-serverful/ent/locationquality"
	"footprint-serverful/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	s, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	// This suite uses an explicitly supplied disposable database.
	for _, remove := range []func(context.Context) (int, error){
		s.client.Location.Delete().Exec, s.client.Project.Delete().Exec, s.client.DeviceAnalysis.Delete().Exec,
	} {
		if _, err := remove(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func report(t *testing.T, s *Store, longitude float64, seconds int64) model.IngestResult {
	t.Helper()
	result, err := s.ReportLocation(t.Context(), model.LocationInput{
		Longitude: longitude, Latitude: 30, Device: "phone", Timestamp: seconds * 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPostgresAnnotationsAndBackfills(t *testing.T) {
	s := testStore(t)
	report(t, s, 120, 0)
	report(t, s, 120, 30)
	spike := report(t, s, 121, 60)
	if spike.Location.Quality.Status != "uncertain" {
		t.Fatalf("one-sided jump: %+v", spike)
	}
	report(t, s, 120, 90)
	last := report(t, s, 120, 120)
	q, err := s.client.LocationQuality.Query().Where(locationquality.LocationIDEQ(spike.Location.ID)).Only(t.Context())
	if err != nil || q.Status != locationquality.StatusOutlier {
		t.Fatalf("spike should become an outlier: %+v, %v", q, err)
	}
	filtered, err := s.ListLocations(t.Context(), model.LocationFilter{Filtered: true}, 2, 0, false)
	if err != nil || len(filtered.Locations) != 2 || !filtered.Pagination.HasMore {
		t.Fatalf("unexpected first filtered page: %+v, %v", filtered, err)
	}
	second, err := s.ListLocations(t.Context(), model.LocationFilter{Filtered: true}, 2, 2, false)
	if err != nil || len(second.Locations) != 2 || second.Pagination.HasMore {
		t.Fatalf("filter must precede pagination: %+v, %v", second, err)
	}
	report(t, s, 120, 15)
	latest, err := s.LatestLocation(t.Context(), model.LocationFilter{})
	if err != nil || latest.ID != last.Location.ID {
		t.Fatal("historical backfill changed the latest observation")
	}
	original, err := s.client.Location.Get(t.Context(), spike.Location.ID)
	if err != nil || original.Longitude != 121 {
		t.Fatal("analysis modified the original position")
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	count, err := s.client.Location.Query().Count(t.Context())
	if err != nil || count != 6 {
		t.Fatal("repeated migration changed observations")
	}
}

func TestPostgresFilteredReadSnapshot(t *testing.T) {
	for _, endpoint := range []string{"list", "latest"} {
		t.Run(endpoint, func(t *testing.T) {
			s := testStore(t)
			point := report(t, s, 120, 0)
			var changed atomic.Bool
			s.client.LocationQuality.Intercept(entgo.InterceptFunc(func(next entgo.Querier) entgo.Querier {
				return entgo.QuerierFunc(func(ctx context.Context, query entgo.Query) (entgo.Value, error) {
					if changed.CompareAndSwap(false, true) {
						// Simulate analysis committing after the location query
						// but before Ent loads that location's quality edge.
						if err := s.client.LocationQuality.Update().
							Where(locationquality.LocationIDEQ(point.Location.ID)).
							SetStatus(locationquality.StatusOutlier).Exec(ctx); err != nil {
							return nil, err
						}
					}
					return next.Query(ctx, query)
				})
			}))
			filter := model.LocationFilter{Filtered: true}
			var got *model.Location
			if endpoint == "latest" {
				var err error
				got, err = s.LatestLocation(t.Context(), filter)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				page, err := s.ListLocations(t.Context(), filter, 10, 0, false)
				if err != nil || len(page.Locations) != 1 {
					t.Fatalf("unexpected page: %+v, %v", page, err)
				}
				got = &page.Locations[0]
			}
			if !changed.Load() || got == nil || got.Quality.Status != "pending" {
				t.Fatalf("filtered response mixed database snapshots: %+v", got)
			}
			next, err := s.LatestLocation(t.Context(), filter)
			if err != nil || next != nil {
				t.Fatalf("next request did not see the committed outlier: %+v, %v", next, err)
			}
		})
	}
}

func TestPostgresIngestRollback(t *testing.T) {
	s := testStore(t)
	s.client.LocationQuality.Use(func(next entgo.Mutator) entgo.Mutator {
		return entgo.MutateFunc(func(ctx context.Context, mutation entgo.Mutation) (entgo.Value, error) {
			return nil, errors.New("quality write unavailable")
		})
	})
	_, err := s.ReportLocation(t.Context(), model.LocationInput{Device: "phone", Timestamp: 1000})
	if err == nil {
		t.Fatal("expected failed ingest")
	}
	count, err := s.client.Location.Query().Count(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("location escaped rollback: %d, %v", count, err)
	}
	count, err = s.client.DeviceAnalysis.Query().Count(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("revision escaped rollback: %d, %v", count, err)
	}
}

func TestPostgresAnalysisFailureKeepsIngest(t *testing.T) {
	s := testStore(t)
	var fail atomic.Bool
	fail.Store(true)
	s.client.Location.Intercept(entgo.InterceptFunc(func(next entgo.Querier) entgo.Querier {
		return entgo.QuerierFunc(func(ctx context.Context, query entgo.Query) (entgo.Value, error) {
			if fail.Load() {
				return nil, errors.New("window unavailable")
			}
			return next.Query(ctx, query)
		})
	}))
	result := report(t, s, 120, 0)
	fail.Store(false)
	if result.Analysis.State != "deferred" || result.Location.Quality.Status != "pending" {
		t.Fatalf("accepted ingest failed: %+v", result)
	}
	count, err := s.client.Location.Query().Count(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("raw location was not retained: %d, %v", count, err)
	}
}

func TestPostgresRejectsStaleAnalysis(t *testing.T) {
	s := testStore(t)
	report(t, s, 120, 0)
	report(t, s, 120, 30)
	spike := report(t, s, 121, 60)
	target, err := s.ingest(t.Context(), model.LocationInput{Longitude: 120, Latitude: 30, Device: "phone", Timestamp: 90000})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.loadWindow(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	_, stale, err := annotate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	report(t, s, 120, 120)
	for _, labels := range [][]*ent.LocationQuality{stale, nil} {
		accepted, err := s.saveLabels(t.Context(), "phone", snapshot.revision, labels)
		if err != nil || accepted {
			t.Fatalf("stale snapshot was accepted: %v, %v", accepted, err)
		}
	}
	q, err := s.client.LocationQuality.Query().Where(locationquality.LocationIDEQ(spike.Location.ID)).Only(t.Context())
	if err != nil || q.Status != locationquality.StatusOutlier {
		t.Fatalf("newer evidence overwritten: %+v, %v", q, err)
	}
}

func TestPostgresConcurrentIngestAndNoopAnalysis(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() {
			_, err := s.ReportLocation(t.Context(), model.LocationInput{
				Longitude: 120, Latitude: 30, Device: "phone", Timestamp: int64(i * 30000),
			})
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	last := report(t, s, 120, 360)
	state, err := s.client.DeviceAnalysis.Get(t.Context(), "phone")
	if err != nil || state.Revision != 13 {
		t.Fatalf("lost device revision: %+v, %v", state, err)
	}
	count, err := s.client.Location.Query().Where(location.DeviceEQ("phone")).Count(t.Context())
	if err != nil || count != 13 {
		t.Fatalf("lost concurrent observation: %d, %v", count, err)
	}
	row, err := s.client.Location.Get(t.Context(), last.Location.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.client.LocationQuality.Query().Order(ent.Asc(locationquality.FieldLocationID)).All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, analysis := s.analyzeRecent(t.Context(), row)
	if analysis.State != "updated" || analysis.UpdatedPoints != 0 {
		t.Fatalf("unchanged analysis wrote labels: %+v", analysis)
	}
	after, err := s.client.LocationQuality.Query().Order(ent.Asc(locationquality.FieldLocationID)).All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range before {
		if before[i].CheckedAt != after[i].CheckedAt || before[i].Revision != after[i].Revision {
			t.Fatal("unchanged label timestamp or revision changed")
		}
	}
}

func TestPostgresHistoricalEvidence(t *testing.T) {
	s := testStore(t)
	var reference model.IngestResult
	for _, seconds := range []int64{0, 30, 60, 90} {
		result := report(t, s, 121, seconds)
		if seconds == 60 {
			reference = result
		}
	}
	const base = int64(172800)
	report(t, s, 120, base)
	report(t, s, 120, base+30)
	spike := report(t, s, 121, base+60)
	report(t, s, 120, base+90)
	last := report(t, s, 120, base+120)
	q, err := s.client.LocationQuality.Query().Where(locationquality.LocationIDEQ(spike.Location.ID)).Only(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var evidence excursionEvidence
	if err := json.Unmarshal(q.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if q.Status != locationquality.StatusOutlier || len(evidence.HistoricalMatches) != 1 ||
		evidence.HistoricalMatches[0].PreviousID != reference.Location.ID ||
		evidence.HistoricalSearch == nil || !evidence.HistoricalSearch.Complete {
		t.Fatalf("unexpected history: %s", q.Evidence)
	}
	row, err := s.client.Location.Get(t.Context(), last.Location.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, result := s.analyzeRecent(t.Context(), row)
	if result.UpdatedPoints != 0 {
		t.Fatalf("completed historical evidence was not reused: %+v", result)
	}
}

func TestPostgresBoundedWindowAndDuplicates(t *testing.T) {
	s := testStore(t)
	builders := make([]*ent.LocationCreate, 200)
	for i := range builders {
		builders[i] = s.client.Location.Create().SetDevice("phone").SetTimestamp(int64(i * 60000)).SetLatitude(30).SetLongitude(120)
	}
	if _, err := s.client.Location.CreateBulk(builders...).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := report(t, s, 120, 12000)
	if result.Analysis.WindowPoints != 65 || result.Analysis.UpdatedPoints != 27 {
		t.Fatalf("analysis window was not bounded: %+v", result.Analysis)
	}
	// A separate device exercises duplicate propagation without old context.
	for _, p := range []struct {
		lon float64
		ms  int64
	}{{120, 0}, {120, 30000}, {121, 60000}, {121, 60000}, {120, 90000}, {120, 120000}} {
		if _, err := s.ReportLocation(t.Context(), model.LocationInput{Device: "watch", Longitude: p.lon, Latitude: 30, Timestamp: p.ms}); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.client.Location.Query().Where(location.DeviceEQ("watch"),
		location.HasQualityWith(locationquality.StatusEQ(locationquality.StatusOutlier))).Count(t.Context())
	if err != nil || count != 2 {
		t.Fatalf("duplicate bypassed annotation: %d, %v", count, err)
	}
	state, err := s.client.DeviceAnalysis.Query().Where(deviceanalysis.IDEQ("phone")).Only(t.Context())
	if err != nil || state.Revision != 1 {
		t.Fatal("device states were mixed")
	}
}
