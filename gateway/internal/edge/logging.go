package edge

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const requestIDHeader = "X-Request-ID"

func NewRequestLoggingMiddleware(logger *logrus.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			startedAt := time.Now()

			ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
			request := r.Clone(ctx)
			request.Header.Set(requestIDHeader, requestID)

			w.Header().Set(requestIDHeader, requestID)

			response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(response, request)

			status := response.Status()
			if status == 0 {
				status = http.StatusOK
			}

			logger.WithFields(logrus.Fields{
				"request_id":  requestID,
				"method":      r.Method,
				"status":      status,
				"duration_ms": time.Since(startedAt).Milliseconds(),
			}).Info("http_request")
		})
	}
}
