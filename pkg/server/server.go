package server

import (
	"fmt"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/robinjoseph08/golib/echo/v5/health"
	echologger "github.com/robinjoseph08/golib/echo/v5/middleware/logger"
	"github.com/robinjoseph08/golib/echo/v5/middleware/recovery"
	"github.com/robinjoseph08/web-app-template/internal/webapp"
	"github.com/robinjoseph08/web-app-template/pkg/binder"
	"github.com/robinjoseph08/web-app-template/pkg/config"
	"github.com/robinjoseph08/web-app-template/pkg/errcodes"
	"github.com/robinjoseph08/web-app-template/pkg/errorstack"
	"github.com/robinjoseph08/web-app-template/pkg/version"
)

// maxRequestBodyBytes caps API request bodies. JSON requests are small; raise
// it, or exempt a route, if one needs to accept uploads.
const maxRequestBodyBytes = 64 * 1024

// New constructs the HTTP server and registers the application's routes and
// middleware.
func New(cfg *config.Config) (*http.Server, error) {
	frontend, available, err := webapp.Handler()
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}
	if !available {
		frontend = nil
	}
	return newServer(cfg, frontend)
}

func newServer(cfg *config.Config, frontend http.Handler) (*http.Server, error) {
	e := echo.New()

	requestBinder, err := binder.New()
	if err != nil {
		return nil, fmt.Errorf("create request binder: %w", err)
	}
	e.Binder = requestBinder

	e.Use(echologger.Middleware())
	e.Use(recovery.Middleware())
	e.Use(capturePanicErrorStack())
	// The embedded frontend compresses its own files once at startup, so only
	// API responses are compressed per request.
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c *echo.Context) bool { return !isAPI(c.Request()) },
	}))
	e.Use(browserAPI())

	health.RegisterRoutes(e)
	version.RegisterRoutes(e)
	apiNotFound := func(_ *echo.Context) error { return echo.ErrNotFound }
	e.Any("/api", apiNotFound)
	e.Any("/api/*", apiNotFound)

	if frontend != nil {
		e.GET("/*", echo.WrapHandler(frontend))
		e.HEAD("/*", echo.WrapHandler(frontend))
	}

	e.HTTPErrorHandler = errcodes.NewHandler().Handle

	return &http.Server{
		Addr:              net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort)),
		Handler:           e,
		ReadHeaderTimeout: 3 * time.Second,
	}, nil
}

func isAPI(request *http.Request) bool {
	return request.URL.Path == "/api" || strings.HasPrefix(request.URL.Path, "/api/")
}

// capturePanicErrorStack preserves the panic site before Golib's recovery
// middleware converts an error panic into a returned error.
func capturePanicErrorStack() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				recoveredErr, ok := recovered.(error)
				if !ok {
					panic(recovered)
				}
				if recoveredErr == http.ErrAbortHandler { //nolint:errorlint // net/http requires the exact sentinel.
					panic(recoveredErr)
				}
				panic(errorstack.Capture(recoveredErr))
			}()
			return next(c)
		}
	}
}

// browserAPI keeps API responses out of caches and rejects cross-site
// mutations. The frontend is served from the same origin as the API (Vite
// proxies /api in development), so it's never cross-origin. Mutations must
// send JSON, which a plain HTML form can't, and are capped in size.
func browserAPI() echo.MiddlewareFunc {
	crossOrigin := http.NewCrossOriginProtection()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			request := c.Request()
			if !isAPI(request) {
				return next(c)
			}
			header := c.Response().Header()
			header.Set("Cache-Control", "no-store")
			header.Set("X-Content-Type-Options", "nosniff")
			switch request.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return next(c)
			}
			if err := crossOrigin.Check(request); err != nil {
				return &errcodes.Error{HTTPCode: http.StatusForbidden, Message: "Cross-origin requests are not allowed.", Code: "invalid_origin"}
			}
			if request.ContentLength != 0 || request.Header.Get(echo.HeaderContentType) != "" {
				contentType, _, err := mime.ParseMediaType(request.Header.Get(echo.HeaderContentType))
				if err != nil || contentType != echo.MIMEApplicationJSON {
					return errcodes.UnsupportedMediaType()
				}
			}
			request.Body = http.MaxBytesReader(c.Response(), request.Body, maxRequestBodyBytes)
			return next(c)
		}
	}
}
