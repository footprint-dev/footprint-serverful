package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/labstack/echo/v5"
)

//go:embed dist
var files embed.FS

func Register(e *echo.Echo) {
	assets := echo.MustSubFS(files, "dist")
	register := func(path string, handler echo.HandlerFunc, cache string) {
		serve := func(c *echo.Context) error {
			c.Response().Header().Set("Cache-Control", cache)
			return handler(c)
		}
		e.Add(http.MethodGet, path, serve)
		e.Add(http.MethodHead, path, serve)
	}
	register("/", echo.StaticFileHandler("index.html", assets), "no-cache")
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		path := "/" + entry.Name()
		cache := "no-cache"
		handler := echo.StaticFileHandler(entry.Name(), assets)
		if entry.IsDir() {
			path += "/*"
			handler = echo.StaticDirectoryHandler(echo.MustSubFS(assets, entry.Name()), true)
			if entry.Name() == "assets" {
				cache = "public, max-age=31536000, immutable"
			}
		}
		register(path, handler, cache)
	}
}
