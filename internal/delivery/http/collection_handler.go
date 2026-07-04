package http

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

// CollectionHandler exposes playlist/album endpoints.
type CollectionHandler struct {
	collections *usecase.CollectionUsecase
}

func NewCollectionHandler(collections *usecase.CollectionUsecase) *CollectionHandler {
	return &CollectionHandler{collections: collections}
}

type createCollectionRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type addItemRequest struct {
	MediaID  uuid.UUID `json:"media_id"`
	Position int       `json:"position"`
}

type collectionResponse struct {
	*domain.Collection
	Items []*domain.MediaItem `json:"items"`
}

// Create handles POST /api/v1/collections.
func (h *CollectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	var req createCollectionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	col, err := h.collections.Create(r.Context(), userID, req.Name, domain.CollectionType(req.Type))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, col)
}

// List handles GET /api/v1/collections.
func (h *CollectionHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	cols, err := h.collections.List(r.Context(), userID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if cols == nil {
		cols = []*domain.Collection{}
	}
	writeJSON(w, http.StatusOK, cols)
}

// Get handles GET /api/v1/collections/{id} and includes the ordered items.
func (h *CollectionHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	col, items, err := h.collections.Get(r.Context(), userID, role, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if items == nil {
		items = []*domain.MediaItem{}
	}
	writeJSON(w, http.StatusOK, collectionResponse{Collection: col, Items: items})
}

// AddItem handles POST /api/v1/collections/{id}/items.
func (h *CollectionHandler) AddItem(w http.ResponseWriter, r *http.Request) {
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
	var req addItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.MediaID == uuid.Nil {
		writeError(w, r, fmt.Errorf("%w: media_id is required", domain.ErrInvalidInput))
		return
	}
	if err := h.collections.AddItem(r.Context(), userID, role, id, req.MediaID, req.Position); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveItem handles DELETE /api/v1/collections/{id}/items/{mediaID}.
func (h *CollectionHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
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
	mediaID, err := uuid.Parse(chi.URLParam(r, "mediaID"))
	if err != nil {
		writeError(w, r, fmt.Errorf("%w: invalid media id", domain.ErrInvalidInput))
		return
	}
	if err := h.collections.RemoveItem(r.Context(), userID, role, id, mediaID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Delete handles DELETE /api/v1/collections/{id}.
func (h *CollectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.collections.Delete(r.Context(), userID, role, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
