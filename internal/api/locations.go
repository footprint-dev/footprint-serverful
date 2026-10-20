package api

import (
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"

	"footprint-serverful/internal/model"
)

func locationInput(body map[string]any) (model.LocationInput, error) {
	var input model.LocationInput
	var err error
	if input.Longitude, err = number(coalesce(body, "longitude", "lng"), "longitude", -180, 180); err != nil {
		return input, err
	}
	if input.Latitude, err = number(coalesce(body, "latitude", "lat"), "latitude", -90, 90); err != nil {
		return input, err
	}
	if input.Timestamp, err = timestamp(coalesce(body, "timestamp", "datetime"), "timestamp"); err != nil {
		return input, err
	}
	if input.Timezone, err = timezone(body["timezone"]); err != nil {
		return input, err
	}
	if input.Device, err = text(body["device"], "device"); err != nil {
		return input, err
	}
	if city := body["city"]; city != nil {
		value, ok := city.(string)
		if !ok || textLength(value) > 200 {
			return input, badRequest("city", "city must be a string of at most 200 characters")
		}
		input.City = strings.TrimSpace(value)
	}
	return input, nil
}

func locationFilter(query url.Values) (model.LocationFilter, error) {
	filter := model.LocationFilter{}
	for _, field := range []string{"since", "until"} {
		if query.Has(field) {
			value, err := timestamp(query.Get(field), field)
			if err != nil {
				return filter, err
			}
			if field == "since" {
				filter.Since = &value
			} else {
				filter.Until = &value
			}
		}
	}
	if filter.Since != nil && filter.Until != nil && *filter.Since > *filter.Until {
		return filter, badRequest("since", "since must not be after until")
	}
	for _, field := range []string{"device", "project"} {
		if query.Has(field) {
			value, err := text(query.Get(field), field)
			if err != nil {
				return filter, err
			}
			if field == "device" {
				filter.Device = &value
			} else {
				filter.Project = &value
			}
		}
	}
	if query.Has("view") {
		switch query.Get("view") {
		case "raw":
		case "filtered":
			filter.Filtered = true
		default:
			return filter, badRequest("view", "view must be raw or filtered")
		}
	}
	return filter, nil
}

func (s *server) reportLocation(c *echo.Context) error {
	body, err := readBody(c.Request())
	if err != nil {
		return err
	}
	input, err := locationInput(body)
	if err != nil {
		return err
	}
	result, err := s.store.ReportLocation(c.Request().Context(), input)
	if err != nil {
		return err
	}
	return c.JSON(201, result)
}

func (s *server) latestLocation(c *echo.Context) error {
	filter, err := locationFilter(c.QueryParams())
	if err != nil {
		return err
	}
	row, err := s.store.LatestLocation(c.Request().Context(), filter)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"location": row})
}

func (s *server) listLocations(c *echo.Context) error {
	query := c.QueryParams()
	filter, err := locationFilter(query)
	if err != nil {
		return err
	}
	limit, err := integerParam(query, "limit", 500, 1, 5000)
	if err != nil {
		return err
	}
	offset, err := integerParam(query, "offset", 0, 0, maxSafeInt)
	if err != nil {
		return err
	}
	order := "asc"
	if query.Has("order") {
		order = query.Get("order")
	}
	if order != "asc" && order != "desc" {
		return badRequest("order", "order must be asc or desc")
	}
	result, err := s.store.ListLocations(c.Request().Context(), filter, limit, offset, order == "desc")
	if err != nil {
		return err
	}
	return c.JSON(200, result)
}
