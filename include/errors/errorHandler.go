package errors

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ErrorHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Next()

		if len(ctx.Errors) > 0 {
			err := ctx.Errors.Last()

			var httpError *HttpAgentError
			if errors.As(err, &httpError) {
				slog.Error(err.Error(),
					"path", ctx.Request.URL.Path,
					"userId", httpError.UserID,
					"agentId", httpError.AgentID,
					"error", httpError.Unwrap().Error())
				ctx.JSON(httpError.Status, httpError)
				return
			}

			slog.Error("error", "error", err.Error(), "path", ctx.Request.URL.Path)
			ctx.JSON(http.StatusInternalServerError, HttpAgentError{
				Status:  http.StatusInternalServerError,
				Message: "Something went wrong.",
			})
		}
	}
}
