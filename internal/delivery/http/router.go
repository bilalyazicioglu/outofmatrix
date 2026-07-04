// Package http is the delivery layer: it translates HTTP requests into
// usecase calls and usecase results (or domain errors) into responses.
package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"outofmatrix/internal/delivery/ws"
	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

// RouterDeps carries everything the router needs, so main.go stays a pure
// composition root.
type RouterDeps struct {
	Log            *slog.Logger
	Auth           *usecase.AuthUsecase
	Media          *usecase.MediaUsecase
	Uploads        *usecase.UploadUsecase
	Collections    *usecase.CollectionUsecase
	Hub            *ws.Hub
	MaxUploadBytes int64
	WebDir         string
	Health         func() error // liveness probe, e.g. database ping
}

// NewRouter assembles the chi router with all middleware and routes.
func NewRouter(d RouterDeps) http.Handler {
	authH := NewAuthHandler(d.Auth)
	mediaH := NewMediaHandler(d.Media, d.MaxUploadBytes)
	uploadH := NewUploadHandler(d.Uploads)
	streamH := NewStreamHandler(d.Media)
	collH := NewCollectionHandler(d.Collections)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(RequestLogger(d.Log))
	r.Use(chimw.Recoverer)
	r.Use(CORS)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		if d.Health != nil {
			if err := d.Health(); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "unhealthy"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/auth/register", authH.Register)
		api.Post("/auth/login", authH.Login)

		api.Group(func(private chi.Router) {
			private.Use(Auth(d.Auth))

			// Real-time processing events.
			private.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
				userID, _, ok := callerFromContext(r.Context())
				if !ok {
					writeError(w, r, domain.ErrUnauthorized)
					return
				}
				d.Hub.ServeWS(w, r, userID)
			})

			// Resumable chunked uploads (preferred path; handles 5 GB+ files).
			private.Post("/uploads", uploadH.Create)
			private.Get("/uploads/{id}", uploadH.Status)
			private.Put("/uploads/{id}/chunks/{index}", uploadH.PutChunk)
			private.Post("/uploads/{id}/complete", uploadH.Complete)
			private.Delete("/uploads/{id}", uploadH.Abort)

			// Legacy single-request multipart upload (fine for small files).
			private.Post("/media/upload", mediaH.Upload)

			private.Get("/media", mediaH.List)
			private.Get("/media/{id}", mediaH.Get)
			private.Delete("/media/{id}", mediaH.Delete)

			private.Get("/media/stream/{id}/{file}", streamH.HLSFile)
			private.Get("/media/raw/{id}", streamH.Raw)
			private.Get("/media/thumb/{id}", streamH.Thumbnail)

			private.Post("/collections", collH.Create)
			private.Get("/collections", collH.List)
			private.Get("/collections/{id}", collH.Get)
			private.Delete("/collections/{id}", collH.Delete)
			private.Post("/collections/{id}/items", collH.AddItem)
			private.Delete("/collections/{id}/items/{mediaID}", collH.RemoveItem)
		})
	})

	// Static frontend (static/index.html) at the root.
	if d.WebDir != "" {
		fs := http.FileServer(http.Dir(d.WebDir))
		r.Handle("/*", fs)
	}

	return r
}
