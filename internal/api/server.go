package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"footprint-serverful/internal/store"
)

type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *apiError) Error() string {
	return e.Message
}

func badRequest(field, message string) error {
	return &apiError{Status: 400, Code: "bad_request", Message: message, Field: field}
}

type server struct {
	store *store.Store
	token string
}

var bearerPattern = regexp.MustCompile(`(?i)^Bearer (\S+)$`)

func New(db *store.Store, token string) *echo.Echo {
	s := &server{store: db, token: token}
	e := echo.New()
	e.HTTPErrorHandler = errorHandler
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// RawPath is optional in Go. Keep routing consistently encoded so a
			// project name containing "/" or a literal "%2F" is decoded once.
			c.Request().URL.RawPath = c.Request().URL.EscapedPath()
			return next(c)
		}
	})
	e.Use(requestLog)
	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{DisablePrintStack: true}))
	e.Use(middleware.ContextTimeout(30 * time.Second))
	e.Use(s.authenticate)
	e.GET("/health", func(c *echo.Context) error {
		return c.JSON(200, map[string]string{"status": "ok", "name": "footprint-serverful-api"})
	})
	e.GET("/api/me", func(c *echo.Context) error {
		return c.JSON(200, map[string]any{"authenticated": true, "method": "bearer"})
	})
	e.PUT("/api/location", s.reportLocation)
	e.GET("/api/location", s.latestLocation)
	e.GET("/api/locations", s.listLocations)
	e.GET("/api/projects", s.listProjects)
	e.POST("/api/projects", s.createProject)
	e.GET("/api/projects/:value", s.findProject)
	e.PATCH("/api/projects/:value", s.updateProject)
	return e
}

func (s *server) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			if s.token == "" {
				return &apiError{Status: 503, Code: "auth_not_configured", Message: "Ingest token is not configured"}
			}
			match := bearerPattern.FindStringSubmatch(c.Request().Header.Get("Authorization"))
			if len(match) != 2 || subtle.ConstantTimeCompare([]byte(match[1]), []byte(s.token)) != 1 {
				return &apiError{Status: 401, Code: "unauthorized", Message: "A valid bearer token is required"}
			}
		}
		return next(c)
	}
}

func errorHandler(c *echo.Context, err error) {
	if response, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil && response.Committed {
		return
	}
	var result *apiError
	if !errors.As(err, &result) {
		switch {
		case errors.Is(err, store.ErrProjectNotFound):
			result = &apiError{Status: 404, Code: "project_not_found", Message: "Project does not exist"}
		case errors.Is(err, store.ErrProjectRange):
			result = &apiError{Status: 400, Code: "bad_request", Message: err.Error(), Field: "endTime"}
		default:
			status := http.StatusInternalServerError
			var frameworkError echo.HTTPStatusCoder
			if errors.As(err, &frameworkError) {
				status = frameworkError.StatusCode()
			}
			code, message := "internal_error", "The request could not be completed"
			switch status {
			case 404:
				code, message = "not_found", "Unknown route"
			case 405:
				code, message = "method_not_allowed", "Method is not allowed"
			case 400:
				code, message = "bad_request", "Malformed request"
			case 415:
				code, message = "unsupported_media_type", "Use JSON or form data"
			}
			result = &apiError{Status: status, Code: code, Message: message}
		}
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	if result.Status == 401 {
		c.Response().Header().Set("WWW-Authenticate", `Bearer realm="footprint-api"`)
	}
	if result.Status >= 500 {
		// Database errors can contain credentials or observations.
		slog.ErrorContext(c.Request().Context(), "API request failed", "route", c.Path(), "error_type", fmt.Sprintf("%T", err))
	}
	_ = c.JSON(result.Status, map[string]any{"error": result})
}

func requestLog(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		start := time.Now()
		err := next(c)
		if err != nil {
			errorHandler(c, err)
		}
		status := 200
		if r, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil {
			status = r.Status
		}
		slog.InfoContext(c.Request().Context(), "HTTP request", "method", c.Request().Method,
			"route", c.Path(), "status", status, "duration", time.Since(start))
		return nil
	}
}
