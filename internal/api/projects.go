package api

import (
	"net/url"
	"strconv"

	"github.com/labstack/echo/v5"

	"footprint-serverful/internal/model"
)

func (s *server) listProjects(c *echo.Context) error {
	limit, err := integerParam(c.QueryParams(), "limit", 100, 1, 500)
	if err != nil {
		return err
	}
	offset, err := integerParam(c.QueryParams(), "offset", 0, 0, maxSafeInt)
	if err != nil {
		return err
	}
	result, err := s.store.ListProjects(c.Request().Context(), limit, offset)
	if err != nil {
		return err
	}
	return c.JSON(200, result)
}

func (s *server) findProject(c *echo.Context) error {
	// Echo routes on the escaped path, so encoded slashes remain one parameter.
	decoded, err := url.PathUnescape(c.Param("value"))
	if err != nil {
		return badRequest("project", "Invalid project name encoding")
	}
	name, err := text(decoded, "project")
	if err != nil {
		return err
	}
	p, err := s.store.FindProject(c.Request().Context(), name)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"project": p})
}

func (s *server) createProject(c *echo.Context) error {
	body, err := readBody(c.Request())
	if err != nil {
		return err
	}
	var input model.ProjectInput
	if input.Name, err = text(body["name"], "name"); err != nil {
		return err
	}
	if input.StartTime, err = timestamp(body["startTime"], "startTime"); err != nil {
		return err
	}
	if input.EndTime, err = timestamp(body["endTime"], "endTime"); err != nil {
		return err
	}
	p, err := s.store.CreateProject(c.Request().Context(), input)
	if err != nil {
		return err
	}
	return c.JSON(201, map[string]any{"project": p})
}

func (s *server) updateProject(c *echo.Context) error {
	value := c.Param("value")
	id, err := strconv.ParseInt(value, 10, 64)
	if !idPattern.MatchString(value) || err != nil || id > maxSafeInt {
		return badRequest("id", "id must be a positive integer")
	}
	body, err := readBody(c.Request())
	if err != nil {
		return err
	}
	var input model.ProjectPatch
	if value, ok := body["name"]; ok {
		name, err := text(value, "name")
		if err != nil {
			return err
		}
		input.Name = &name
	}
	if value, ok := body["startTime"]; ok {
		start, err := timestamp(value, "startTime")
		if err != nil {
			return err
		}
		input.StartTime = &start
	}
	if value, ok := body["endTime"]; ok {
		end, err := timestamp(value, "endTime")
		if err != nil {
			return err
		}
		input.EndTime = &end
	}
	if input.Name == nil && input.StartTime == nil && input.EndTime == nil {
		return badRequest("body", "Provide name, startTime or endTime")
	}
	p, err := s.store.UpdateProject(c.Request().Context(), id, input)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"project": p})
}
