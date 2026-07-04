package http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

// UploadHandler exposes the resumable chunked upload API:
//
//	POST   /api/v1/uploads                     open a session
//	GET    /api/v1/uploads/{id}                which chunks arrived (resume)
//	PUT    /api/v1/uploads/{id}/chunks/{index} raw chunk bytes
//	POST   /api/v1/uploads/{id}/complete       assemble -> MediaItem -> queue
//	DELETE /api/v1/uploads/{id}                abort
type UploadHandler struct {
	uploads *usecase.UploadUsecase
}

func NewUploadHandler(uploads *usecase.UploadUsecase) *UploadHandler {
	return &UploadHandler{uploads: uploads}
}

type createSessionRequest struct {
	Filename  string `json:"filename"`
	Title     string `json:"title"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunk_size"`
}

type sessionResponse struct {
	*domain.UploadSession
	ReceivedChunks []int `json:"received_chunks"`
}

// Create handles POST /api/v1/uploads.
func (h *UploadHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	var req createSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.uploads.CreateSession(r.Context(), userID, req.Filename, req.Title, req.MimeType, req.Size, req.ChunkSize)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, sessionResponse{UploadSession: session, ReceivedChunks: []int{}})
}

// Status handles GET /api/v1/uploads/{id}: the resume handshake.
func (h *UploadHandler) Status(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	session, received, err := h.uploads.Status(r.Context(), userID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if received == nil {
		received = []int{}
	}
	writeJSON(w, http.StatusOK, sessionResponse{UploadSession: session, ReceivedChunks: received})
}

// PutChunk handles PUT /api/v1/uploads/{id}/chunks/{index}. The body is the
// raw chunk; it streams straight into the staging file at the chunk's offset.
func (h *UploadHandler) PutChunk(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid chunk index", domain.ErrInvalidInput))
		return
	}

	// Hard transport-level cap; the usecase enforces the exact expected size.
	body := http.MaxBytesReader(w, r.Body, usecase.MaxChunkSize+4096)
	if err := h.uploads.SaveChunk(r.Context(), userID, id, index, body); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Complete handles POST /api/v1/uploads/{id}/complete.
func (h *UploadHandler) Complete(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.uploads.Complete(r.Context(), userID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// Abort handles DELETE /api/v1/uploads/{id}.
func (h *UploadHandler) Abort(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.uploads.Abort(r.Context(), userID, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
