package version

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// RegisterRoutes adds GET /api/version, which tells the frontend which build
// of the server it's talking to.
func RegisterRoutes(e *echo.Echo) {
	e.GET("/api/version", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, Info{Version: Version})
	})
}
