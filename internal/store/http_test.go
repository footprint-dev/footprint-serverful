package store_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"footprint-serverful/internal/api"
	"footprint-serverful/internal/config"
	"footprint-serverful/internal/model"
	"footprint-serverful/internal/store"
)

func exchange(t *testing.T, app http.Handler, method, path, contentType string, body io.Reader, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer test-token")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	app.ServeHTTP(response, req)
	if response.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, response.Code, status, response.Body.String())
	}
	return response
}

func requestJSON[T any](t *testing.T, app http.Handler, method, path string, body any, status int) T {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(t, app, method, path, "application/json", bytes.NewReader(raw), status)
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	db, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	app := api.New(db, config.Config{Server: config.Server{IngestToken: "test-token"}})
	device := fmt.Sprintf("http-%d", time.Now().UnixNano())
	query := "/api/locations?device=" + url.QueryEscape(device)
	type latest struct {
		Location *model.Location `json:"location"`
	}
	type projectResponse struct {
		Project model.Project `json:"project"`
	}
	t.Run("location formats and pagination", func(t *testing.T) {
		empty := requestJSON[latest](t, app, "GET", "/api/location?device="+device, nil, 200)
		if empty.Location != nil {
			t.Fatal("empty latest must be null")
		}
		input := map[string]any{"longitude": 0, "latitude": 0, "timestamp": 3000, "timezone": 0, "device": device}
		first := requestJSON[model.IngestResult](t, app, "PUT", "/api/location", input, 201)
		if first.Location.Longitude != 0 || first.Location.Timezone == nil || *first.Location.Timezone != 0 {
			t.Fatalf("zero values lost: %+v", first)
		}
		form := url.Values{"lng": {"0"}, "lat": {"0"}, "datetime": {"1000"}, "device": {device}}
		exchange(t, app, "PUT", "/api/location?device=wrong", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), 201)
		var multipartBody bytes.Buffer
		writer := multipart.NewWriter(&multipartBody)
		for key, value := range map[string]string{"longitude": "0", "latitude": "0", "timestamp": "3000", "timezone": "5.75", "device": device} {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		response := exchange(t, app, "PUT", "/api/location", writer.FormDataContentType(), &multipartBody, 201)
		var third model.IngestResult
		if err := json.Unmarshal(response.Body.Bytes(), &third); err != nil {
			t.Fatal(err)
		}
		newest := requestJSON[latest](t, app, "GET", "/api/location?device="+device, nil, 200)
		if newest.Location == nil || newest.Location.ID != third.Location.ID {
			t.Fatal("latest must order by observation time then ID")
		}
		page := requestJSON[model.LocationPage](t, app, "GET", query+"&limit=2", nil, 200)
		if len(page.Locations) != 2 || page.Locations[0].Timestamp != 1000 ||
			page.Locations[0].Timezone != nil || page.Locations[1].ID != first.Location.ID || !page.Pagination.HasMore {
			t.Fatalf("unexpected page: %+v", page)
		}
		page = requestJSON[model.LocationPage](t, app, "GET", query+"&limit=2&offset=2", nil, 200)
		if len(page.Locations) != 1 || page.Pagination.HasMore {
			t.Fatal("incorrect final page")
		}
		input["timestamp"] = "2024-01-01T08:00:00+08:00"
		dated := requestJSON[model.IngestResult](t, app, "PUT", "/api/location", input, 201)
		if dated.Location.Timestamp != 1704067200000 {
			t.Fatal("datetime was not normalized")
		}
	})
	t.Run("projects and encoded names", func(t *testing.T) {
		for _, name := range []string{device + " / %2F + 地点", strconv.FormatInt(time.Now().UnixNano(), 10)} {
			p := requestJSON[projectResponse](t, app, "POST", "/api/projects",
				map[string]any{"name": name, "startTime": 1000, "endTime": 3000}, 201).Project
			got := requestJSON[projectResponse](t, app, "GET", "/api/projects/"+url.PathEscape(name), nil, 200).Project
			if got.ID != p.ID || got.Name != name {
				t.Fatalf("encoded name changed: %+v", got)
			}
			duplicate := requestJSON[projectResponse](t, app, "POST", "/api/projects",
				map[string]any{"name": name, "startTime": 2000, "endTime": 4000}, 201).Project
			got = requestJSON[projectResponse](t, app, "GET", "/api/projects/"+url.PathEscape(name), nil, 200).Project
			if got.ID != p.ID {
				t.Fatal("name lookup did not choose the first project")
			}
			updated := requestJSON[projectResponse](t, app, "PATCH", "/api/projects/"+strconv.FormatInt(duplicate.ID, 10),
				map[string]any{"startTime": 0}, 200).Project
			if updated.ID != duplicate.ID || updated.StartTime != 0 || updated.EndTime != 4000 {
				t.Fatalf("partial update lost fields: %+v", updated)
			}
			filtered := requestJSON[model.LocationPage](t, app, "GET",
				query+"&project="+url.QueryEscape(name)+"&since=2000&until=3000", nil, 200)
			if len(filtered.Locations) != 2 {
				t.Fatalf("inclusive filters did not intersect: %+v", filtered)
			}
			path := "/api/projects/" + strconv.FormatInt(p.ID, 10)
			for _, body := range []any{map[string]any{}, map[string]any{"name": nil}, map[string]any{"endTime": nil}, map[string]any{"endTime": 999}} {
				requestJSON[map[string]any](t, app, "PATCH", path, body, 400)
			}
			exchange(t, app, "DELETE", path, "", nil, 405)
		}
		requestJSON[map[string]any](t, app, "GET", "/api/projects/missing-"+device, nil, 404)
	})
	t.Run("invalid input never creates observations", func(t *testing.T) {
		before := requestJSON[model.LocationPage](t, app, "GET", query, nil, 200)
		for _, body := range []any{
			map[string]any{"longitude": 181, "latitude": 0, "timestamp": 0, "device": device},
			map[string]any{"longitude": 0, "latitude": nil, "timestamp": 0, "device": device},
			map[string]any{"longitude": 0, "latitude": 0, "timestamp": 0, "device": device, "timezone": 8.001},
		} {
			requestJSON[map[string]any](t, app, "PUT", "/api/location", body, 400)
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("longitude", "coordinate.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, "0"); err != nil {
			t.Fatal(err)
		}
		_ = writer.WriteField("latitude", "0")
		_ = writer.WriteField("timestamp", "0")
		_ = writer.WriteField("device", device)
		_ = writer.Close()
		exchange(t, app, "PUT", "/api/location", writer.FormDataContentType(), &body, 400)
		after := requestJSON[model.LocationPage](t, app, "GET", query, nil, 200)
		if len(before.Locations) != len(after.Locations) {
			t.Fatal("invalid request wrote a record")
		}
		for _, suffix := range []string{"&view=unknown", "&since=3000&until=1000", "&limit=5001", "&offset=-1"} {
			requestJSON[map[string]any](t, app, "GET", query+suffix, nil, 400)
		}
	})
}
