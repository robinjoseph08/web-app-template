package webapp

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerServesBuiltAsset(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "console.log('app')", recorder.Body.String())
	assert.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))
}

func TestHandlerFallsBackToIndexForClientRoute(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/settings/profile", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "<html>app</html>", recorder.Body.String())
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
}

func TestHandlerFallsBackToIndexForDottedClientRoute(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/users/jane.doe", nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "<html>app</html>", recorder.Body.String())
}

func TestHandlerReturnsNotFoundForMissingAsset(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "<html>app</html>")
}

func TestHandlerSecuresTheAppShell(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, "frame-ancestors 'none'", recorder.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"))
}

func TestHandlerGzipsLargeTextFiles(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/assets/large.js", nil)
	request.Header.Set("Accept-Encoding", "br, gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", recorder.Header().Get("Vary"))
	assert.Equal(t, "text/javascript; charset=utf-8", recorder.Header().Get("Content-Type"))
	reader, err := gzip.NewReader(recorder.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, largeScript, string(body))

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/assets/large.js", nil))
	assert.Empty(t, plain.Header().Get("Content-Encoding"))
	assert.Equal(t, largeScript, plain.Body.String())
	assert.NotEqual(t, recorder.Header().Get("ETag"), plain.Header().Get("ETag"))
}

func TestHandlerHonorsRefusedGzip(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/assets/large.js", nil)
	request.Header.Set("Accept-Encoding", "gzip;q=0")
	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, request)

	assert.Empty(t, recorder.Header().Get("Content-Encoding"))
}

func TestHandlerRevalidatesWithETag(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)

	request := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	assert.Equal(t, http.StatusNotModified, second.Code)
}

func TestHandlerCachesUnhashedFilesBriefly(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=3600", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "image/svg+xml", recorder.Header().Get("Content-Type"))
}

var largeScript = strings.Repeat("console.log('app');\n", 200)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := newHandler(fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"favicon.svg":     {Data: []byte("<svg></svg>")},
		"assets/app.js":   {Data: []byte("console.log('app')")},
		"assets/large.js": {Data: []byte(largeScript)},
	})
	require.NoError(t, err)
	return handler
}
