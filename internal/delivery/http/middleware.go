package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

type contextKey string

const (
	ctxKeyUserID contextKey = "auth.user_id"
	ctxKeyRole   contextKey = "auth.role"
)

// Auth validates the JWT and injects the caller's identity into the request
// context. Credentials are accepted from the Authorization header or, for
// media elements that cannot set headers (<video>, <img>, HLS segment
// requests), from a "token" query parameter.
func Auth(auth *usecase.AuthUsecase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			if token == "" {
				writeError(w, r, fmt.Errorf("%w: missing credentials", domain.ErrUnauthorized))
				return
			}

			claims, err := auth.ValidateToken(token)
			if err != nil {
				writeError(w, r, err)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxKeyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// callerFromContext returns the authenticated identity set by Auth.
func callerFromContext(ctx context.Context) (uuid.UUID, domain.Role, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	if !ok {
		return uuid.Nil, "", false
	}
	role, ok := ctx.Value(ctxKeyRole).(domain.Role)
	if !ok {
		return uuid.Nil, "", false
	}
	return id, role, true
}

// RequestLogger emits one structured log line per request.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// CORS is a permissive CORS policy suitable for a personal server whose API
// and web client may live on different origins during development.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
