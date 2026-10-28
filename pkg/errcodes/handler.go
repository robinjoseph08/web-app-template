package errcodes

import (
	"errors"
	"net/http"

	"github.com/iancoleman/strcase"
	"github.com/labstack/echo/v5"
	echologger "github.com/robinjoseph08/golib/echo/v5/middleware/logger"
	"github.com/robinjoseph08/golib/errutils"
	"github.com/robinjoseph08/web-app-template/pkg/errorstack"
)

// Handler converts application and Echo errors into the API error envelope.
type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

// Handle is an Echo HTTP error handler.
func (h *Handler) Handle(c *echo.Context, err error) {
	ctx := c.Request().Context()
	if errutils.IsIgnorableErr(err) || errorstack.IsContextCancellation(ctx, err) {
		return
	}
	if response, unwrapErr := echo.UnwrapResponse(c.Response()); unwrapErr == nil && response.Committed {
		return
	}

	httpCode, payload := h.generatePayload(err)
	// The response hides the cause of a server error, so the log line is the
	// only place it survives.
	if httpCode >= http.StatusInternalServerError {
		echologger.FromEchoContext(c).Err(err).Error("server error")
	}

	if writeErr := c.JSON(httpCode, payload); writeErr != nil && !errutils.IsIgnorableErr(writeErr) && !errorstack.IsContextCancellation(ctx, writeErr) {
		echologger.FromEchoContext(c).Err(errorstack.Capture(writeErr)).Error("error handler json error")
	}
}

func (*Handler) generatePayload(err error) (int, map[string]any) {
	httpCode := http.StatusInternalServerError
	code := "internal_server_error"
	message := http.StatusText(httpCode)

	if statusCode := echo.StatusCode(err); statusCode != 0 {
		httpCode = statusCode
		message = http.StatusText(statusCode)
		code = strcase.ToSnake(message)
		if statusCode == http.StatusNotFound {
			message = "Page not found."
			code = "not_found"
		}
	}

	var echoErr *echo.HTTPError
	if errors.As(err, &echoErr) && echoErr.Message != "" {
		httpCode = echoErr.Code
		message = echoErr.Message
		code = strcase.ToSnake(message)
	}

	if applicationErr, ok := errors.AsType[*Error](err); ok {
		httpCode = applicationErr.HTTPCode
		message = applicationErr.Message
		code = applicationErr.Code
	}

	return httpCode, map[string]any{
		"error": map[string]any{
			"code":        code,
			"message":     message,
			"status_code": httpCode,
		},
	}
}
