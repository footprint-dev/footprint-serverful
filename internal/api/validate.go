package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	maxTimestamp = int64(8_640_000_000_000_000)
	maxSafeInt   = int64(9_007_199_254_740_991)
)

var (
	digitsPattern = regexp.MustCompile(`^[0-9]+$`)
	idPattern     = regexp.MustCompile(`^[1-9][0-9]*$`)
)

func readBody(r *http.Request) (map[string]any, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, &apiError{Status: 415, Code: "unsupported_media_type", Message: "Use JSON or form data"}
	}
	body := map[string]any{}
	switch strings.ToLower(contentType) {
	case "application/json":
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, badRequest("body", "Malformed request body")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, badRequest("body", "Malformed request body")
		}
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return nil, badRequest("body", "body must be a JSON object")
		}
		return object, nil
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return nil, badRequest("body", "Malformed request body")
		}
		for key, values := range r.PostForm {
			body[key] = values[len(values)-1]
		}
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
			return nil, badRequest("body", "Malformed request body")
		}
		defer r.MultipartForm.RemoveAll()
		for key, values := range r.MultipartForm.Value {
			body[key] = values[len(values)-1]
		}
		for key, files := range r.MultipartForm.File {
			// A file cannot satisfy a scalar coordinate or text field.
			body[key] = files[len(files)-1]
		}
	default:
		return nil, &apiError{Status: 415, Code: "unsupported_media_type", Message: "Use JSON or form data"}
	}
	return body, nil
}

func number(value any, field string) (float64, error) {
	var text string
	switch v := value.(type) {
	case json.Number:
		text = string(v)
	case string:
		text = strings.TrimSpace(v)
	default:
		return 0, badRequest(field, field+" is required and must be a number")
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, badRequest(field, field+" must be a finite number")
	}
	return n, nil
}

func boundedNumber(value any, field string, minValue, maxValue float64) (float64, error) {
	n, err := number(value, field)
	if err != nil {
		return 0, err
	}
	if n < minValue || n > maxValue {
		return 0, badRequest(field, fmt.Sprintf("%s must be between %g and %g", field, minValue, maxValue))
	}
	return n, nil
}

func timestamp(value any, field string) (int64, error) {
	if n, ok := value.(json.Number); ok {
		f, err := n.Float64()
		if err != nil || math.IsNaN(f) || f < 0 || f > float64(maxTimestamp) || math.Trunc(f) != f {
			return 0, badRequest(field, field+" must be a valid Unix timestamp in milliseconds")
		}
		return int64(f), nil
	}
	text, ok := value.(string)
	if !ok || !digitsPattern.MatchString(text) {
		return 0, badRequest(field, field+" must be a valid Unix timestamp in milliseconds")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n > maxTimestamp {
		return 0, badRequest(field, field+" must be a valid Unix timestamp in milliseconds")
	}
	return n, nil
}

func text(value any, field string) (string, error) {
	v, ok := value.(string)
	if !ok || strings.TrimSpace(v) == "" || textLength(v) > 200 {
		return "", badRequest(field, field+" must be a nonempty string of at most 200 characters")
	}
	return strings.TrimSpace(v), nil
}

func textLength(v string) int {
	// The current API measures strings in UTF-16 code units.
	return len(utf16.Encode([]rune(v)))
}

func timezone(value any) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	hours, err := boundedNumber(value, "timezone", -12, 14)
	if err != nil {
		return nil, err
	}
	minutes := hours * 60
	rounded := math.Round(minutes)
	if math.Abs(minutes-rounded) > 1e-7 {
		return nil, badRequest("timezone", "timezone must be a UTC hour offset with whole-minute precision")
	}
	result := rounded / 60
	return &result, nil
}

func integerParam(query url.Values, field string, fallback, minValue, maxValue int64) (int, error) {
	if !query.Has(field) {
		return int(fallback), nil
	}
	value := query.Get(field)
	n, err := strconv.ParseInt(value, 10, 64)
	if !digitsPattern.MatchString(value) || err != nil || n < minValue || n > maxValue {
		return 0, badRequest(field, fmt.Sprintf("%s must be an integer between %d and %d", field, minValue, maxValue))
	}
	return int(n), nil
}

func coalesce(body map[string]any, primary, alias string) any {
	if value := body[primary]; value != nil {
		return value
	}
	return body[alias]
}
