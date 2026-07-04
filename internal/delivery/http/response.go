package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/worker"
)

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already flushed; all we can do is log.
		slog.Error("write json response", "error", err)
	}
}

// writeError maps domain sentinel errors to HTTP status codes. Internal
// details are logged, never sent to the client.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	message := "internal server error"

	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrAlreadyExists):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrInvalidInput):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		status, message = http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, domain.ErrForbidden):
		status, message = http.StatusForbidden, "forbidden"
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, worker.ErrQueueFull):
		status, message = http.StatusServiceUnavailable, "server busy, try again later"
	case errors.As(err, &maxBytes):
		status, message = http.StatusRequestEntityTooLarge, "uploaded file exceeds the size limit"
	}

	if status >= 500 {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeJSON(w, status, errorBody{Error: message})
}
