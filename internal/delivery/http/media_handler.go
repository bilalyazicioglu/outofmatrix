package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

// MediaHandler exposes upload and library CRUD endpoints.
type MediaHandler struct {
	media          *usecase.MediaUsecase
	maxUploadBytes int64
}

func NewMediaHandler(media *usecase.MediaUsecase, maxUploadBytes int64) *MediaHandler {
	return &MediaHandler{media: media, maxUploadBytes: maxUploadBytes}
}

// Upload handles POST /api/v1/media/upload.
//
// The multipart body is consumed as a stream (r.MultipartReader), so a 4 GB
// video never touches RAM: bytes flow from the socket straight to disk.
// Clients should send the optional "title" field BEFORE the "file" field —
// field order in a multipart body is the order they were appended.
func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, r, fmt.Errorf("%w: expected multipart/form-data: %v", domain.ErrInvalidInput, err))
		return
	}

	var (
		title   string
		created *domain.MediaItem
	)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, r, fmt.Errorf("%w: read multipart body: %v", domain.ErrInvalidInput, err))
			return
		}

		switch part.FormName() {
		case "title":
			raw, err := io.ReadAll(io.LimitReader(part, 512))
			part.Close()
			if err != nil {
				writeError(w, r, fmt.Errorf("%w: read title field: %v", domain.ErrInvalidInput, err))
				return
			}
			title = strings.TrimSpace(string(raw))

		case "file":
			if created != nil {
				part.Close()
				continue // one file per request; ignore extras
			}
			filename := part.FileName()

			// Sniff the first 512 bytes so the MIME type does not depend on
			// the client being honest, then stitch the stream back together.
			head := make([]byte, 512)
			n, err := io.ReadFull(part, head)
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
				part.Close()
				writeError(w, r, fmt.Errorf("media: read upload: %w", err))
				return
			}
			head = head[:n]
			mimeType := detectMime(part.Header.Get("Content-Type"), filename, head)
			body := io.MultiReader(bytes.NewReader(head), part)

			created, err = h.media.CreateFromUpload(r.Context(), userID, title, filename, mimeType, body)
			part.Close()
			if err != nil {
				writeError(w, r, err)
				return
			}

		default:
			part.Close()
		}
	}

	if created == nil {
		writeError(w, r, fmt.Errorf(`%w: missing "file" field`, domain.ErrInvalidInput))
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// detectMime picks the most trustworthy MIME type available: an explicit,
// specific client header first, then the filename extension, then content
// sniffing as the last resort.
func detectMime(header, filename string, head []byte) string {
	if header != "" && header != "application/octet-stream" {
		return header
	}
	if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); byExt != "" {
		return byExt
	}
	return http.DetectContentType(head)
}

type listResponse struct {
	Items  []*domain.MediaItem `json:"items"`
	Total  int64               `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

// List handles GET /api/v1/media?type=video&limit=50&offset=0.
func (h *MediaHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}

	q := r.URL.Query()
	limit := parseIntDefault(q.Get("limit"), 50)
	offset := parseIntDefault(q.Get("offset"), 0)
	mediaType := domain.MediaType(q.Get("type"))

	items, total, err := h.media.List(r.Context(), userID, mediaType, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: items, Total: total, Limit: limit, Offset: offset})
}

// Get handles GET /api/v1/media/{id}.
func (h *MediaHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.media.Get(r.Context(), userID, role, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Delete handles DELETE /api/v1/media/{id}.
func (h *MediaHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.media.Delete(r.Context(), userID, role, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid id", domain.ErrInvalidInput)
	}
	return id, nil
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
