package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTimestamp(t *testing.T) {
	for _, input := range []any{json.Number("1704067200000"), "1704067200000", "2024-01-01T00:00:00Z", "2024-01-01T08:00:00+08:00"} {
		got, err := timestamp(input, "timestamp")
		if err != nil || got != 1704067200000 {
			t.Fatalf("%v: got %d, %v", input, got, err)
		}
	}
	for _, input := range []any{nil, true, json.Number("1.5"), "-1", "2024-02-30T00:00:00Z",
		"2024-01-01T00:00:00", "2024-01-01T00:00:00+24:00", "2024-01-01T00:00:00+08:60", "8640000000000001"} {
		if _, err := timestamp(input, "timestamp"); err == nil {
			t.Fatalf("accepted invalid timestamp: %v", input)
		}
	}
}

func TestLocationInputPresenceAndTimezone(t *testing.T) {
	input, err := locationInput(map[string]any{
		"longitude": json.Number("0"), "lng": "120", "lat": "0", "datetime": "0",
		"device": " phone ", "timezone": json.Number("5.75"),
	})
	if err != nil || input.Longitude != 0 || input.Latitude != 0 || input.Timestamp != 0 ||
		input.Device != "phone" || input.Timezone == nil || *input.Timezone != 5.75 {
		t.Fatalf("unexpected input: %+v, %v", input, err)
	}
	for _, value := range []any{json.Number("8.001"), json.Number("15"), "", true, "UTC+8"} {
		if _, err := timezone(value); err == nil {
			t.Fatalf("accepted invalid timezone: %v", value)
		}
	}
	if got, err := timezone(nil); err != nil || got != nil {
		t.Fatal("absent timezone must remain null")
	}
}

func TestBodyParsing(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"object", "application/json", `{"timestamp":8640000000000000}`, 0},
		{"null", "application/json", `null`, 400},
		{"array", "application/json", `[]`, 400},
		{"malformed", "application/json", `{`, 400},
		{"trailing value", "application/json", `{} {}`, 400},
		{"unsupported", "text/plain", `{}`, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/api/location", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			body, err := readBody(r)
			if tc.status == 0 {
				if err != nil || body["timestamp"] != json.Number("8640000000000000") {
					t.Fatalf("precision lost: %v, %v", body, err)
				}
				return
			}
			var apiErr *apiError
			if !errors.As(err, &apiErr) || apiErr.Status != tc.status {
				t.Fatalf("got %v, expected status %d", err, tc.status)
			}
		})
	}
	r := httptest.NewRequest("PUT", "/api/location?device=query", strings.NewReader("device=body&latitude=0"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := readBody(r)
	if err != nil || body["device"] != "body" {
		t.Fatalf("query parameters leaked into body: %v, %v", body, err)
	}
}
