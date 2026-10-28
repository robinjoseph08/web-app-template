package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/robinjoseph08/web-app-template/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfiguresServer(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:0", srv.Addr)
	assert.Equal(t, 3*time.Second, srv.ReadHeaderTimeout)
	assert.IsType(t, &echo.Echo{}, srv.Handler)
}

func TestHealthRoute(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"healthy":true}`, recorder.Body.String())
}

func TestNotFoundResponse(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "not_found", response.Error.Code)
}

func TestEmbeddedFrontendDoesNotHandleUnknownAPIRoutes(t *testing.T) {
	t.Parallel()

	frontend := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("frontend"))
	})
	srv, err := newServer(config.NewForTest(), frontend)
	require.NoError(t, err)

	apiRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(apiRecorder, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	assert.Equal(t, http.StatusNotFound, apiRecorder.Code)
	assert.Contains(t, apiRecorder.Body.String(), "not_found")

	frontendRecorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(frontendRecorder, httptest.NewRequest(http.MethodGet, "/settings", nil))
	assert.Equal(t, http.StatusOK, frontendRecorder.Code)
	assert.Equal(t, "frontend", frontendRecorder.Body.String())
}

func TestMethodNotAllowedResponse(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/health", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Contains(t, recorder.Header().Get(echo.HeaderAllow), http.MethodGet)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "method_not_allowed", response.Error.Code)
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
}

func TestAPIRejectsCrossOriginMutations(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		headers map[string]string
		status  int
	}{
		"cross-site fetch":     {map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "application/json"}, http.StatusForbidden},
		"foreign origin":       {map[string]string{"Origin": "https://attacker.example", "Content-Type": "application/json"}, http.StatusForbidden},
		"same-origin fetch":    {map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/json"}, http.StatusNotFound},
		"matching origin":      {map[string]string{"Origin": "http://example.com", "Content-Type": "application/json"}, http.StatusNotFound},
		"non-browser client":   {map[string]string{"Content-Type": "application/json"}, http.StatusNotFound},
		"form body":            {map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		"JSON with a charset":  {map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/json; charset=utf-8"}, http.StatusNotFound},
		"missing content type": {map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusUnsupportedMediaType},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv, err := newServer(config.NewForTest(), nil)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "http://example.com/api/missing", strings.NewReader("{}"))
			for key, value := range test.headers {
				request.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			srv.Handler.ServeHTTP(recorder, request)

			assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
		})
	}
}

func TestAPIAllowsMutationsWithoutABody(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/missing", nil)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestAPIRejectsLargeBodies(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	e := srv.Handler.(*echo.Echo)
	e.POST("/api/echo", func(c *echo.Context) error {
		var body map[string]string
		if err := c.Bind(&body); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, body)
	})

	large := `{"value":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(large))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "request_too_large")
}

func TestAPIResponsesAreCompressed(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)

	assert.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
}

func TestFrontendIsNotCompressedTwice(t *testing.T) {
	t.Parallel()

	frontend := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("frontend"))
	})
	srv, err := newServer(config.NewForTest(), frontend)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)

	assert.Empty(t, recorder.Header().Get("Content-Encoding"))
	assert.Equal(t, "frontend", recorder.Body.String())
}

func TestPanicKeepsItsStack(t *testing.T) {
	t.Parallel()

	var captured error
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					captured, _ = recovered.(error)
				}
			}()
			return next(c)
		}
	})
	e.Use(capturePanicErrorStack())
	e.GET("/panic", func(_ *echo.Context) error {
		panic(errors.New("boom"))
	})
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))

	require.Error(t, captured)
	assert.Contains(t, fmt.Sprintf("%+v", captured), "server_test.go")
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Parallel()

	srv, err := newServer(config.NewForTest(), nil)
	require.NoError(t, err)
	e := srv.Handler.(*echo.Echo)
	e.GET("/panic", func(_ *echo.Context) error {
		panic("boom")
	})

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}
