package version

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

func TestDefaultVersion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "dev", Version)
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	e := echo.New()
	RegisterRoutes(e)
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"version":"dev"}`, recorder.Body.String())
}
