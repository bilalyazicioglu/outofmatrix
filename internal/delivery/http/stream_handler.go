package http

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/usecase"
)

// StreamHandler serves media bytes: adaptive HLS (master playlist, media
// playlists, segments) for video, range-request originals for audio/photos,
// and thumbnails.
type StreamHandler struct {
	media *usecase.MediaUsecase
}

func NewStreamHandler(media *usecase.MediaUsecase) *StreamHandler {
	return &StreamHandler{media: media}
}

// The HLS output is a flat directory produced by our own transcoder, so the
// only names ever served are these three shapes. Anything else — traversal
// attempts included — is rejected before touching the filesystem.
var (
	// master.m3u8, index_1080p.m3u8, plus legacy single-variant index.m3u8
	hlsPlaylistPattern = regexp.MustCompile(`^(master\.m3u8|index(_[A-Za-z0-9]+)?\.m3u8)$`)
	// segment_1080p_00042.ts, plus legacy segment_00042.ts
	hlsSegmentPattern = regexp.MustCompile(`^segment_(?:[A-Za-z0-9]+_)?[0-9]+\.ts$`)
)

// loadReady fetches the item with ownership enforcement.
func (h *StreamHandler) loadReady(w http.ResponseWriter, r *http.Request) (*domain.MediaItem, bool) {
	userID, role, ok := callerFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return nil, false
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return nil, false
	}
	item, err := h.media.Get(r.Context(), userID, role, id)
	if err != nil {
		writeError(w, r, err)
		return nil, false
	}
	return item, true
}

// HLSFile handles GET /api/v1/media/stream/{id}/{file} for every HLS asset.
func (h *StreamHandler) HLSFile(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadReady(w, r)
	if !ok {
		return
	}
	if item.HLSPath == "" {
		writeJSON(w, http.StatusConflict, errorBody{
			Error: fmt.Sprintf("HLS rendition not available (status: %s)", item.Status),
		})
		return
	}

	name := chi.URLParam(r, "file")
	if name != filepath.Base(name) {
		writeError(w, r, fmt.Errorf("%w: invalid file name", domain.ErrInvalidInput))
		return
	}

	switch {
	case hlsPlaylistPattern.MatchString(name):
		h.servePlaylist(w, r, item, name)
	case hlsSegmentPattern.MatchString(name):
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		http.ServeFile(w, r, h.media.AbsPath(filepath.Join(item.HLSPath, name)))
	default:
		writeError(w, r, fmt.Errorf("%w: invalid file name", domain.ErrInvalidInput))
	}
}

// servePlaylist streams an .m3u8 file, rewriting every playlist/segment URI
// to carry the caller's ?token= — HLS players fetch those URIs themselves
// and cannot attach an Authorization header.
func (h *StreamHandler) servePlaylist(w http.ResponseWriter, r *http.Request, item *domain.MediaItem, name string) {
	if item.Status != domain.MediaStatusReady {
		writeJSON(w, http.StatusConflict, errorBody{
			Error: fmt.Sprintf("HLS rendition not available (status: %s)", item.Status),
		})
		return
	}

	path := h.media.AbsPath(filepath.Join(item.HLSPath, name))
	if name == "master.m3u8" {
		// Items processed before the multi-bitrate upgrade only have the old
		// single-variant playlist; serve that transparently.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = h.media.AbsPath(filepath.Join(item.HLSPath, "index.m3u8"))
		}
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, r, fmt.Errorf("open playlist: %w", err))
		return
	}
	defer f.Close()

	token := r.URL.Query().Get("token")
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if token != "" && (strings.HasSuffix(line, ".ts") || strings.HasSuffix(line, ".m3u8")) {
			line += "?token=" + url.QueryEscape(token)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return // client went away
		}
	}
	if err := scanner.Err(); err != nil {
		writeError(w, r, fmt.Errorf("read playlist: %w", err))
	}
}

// Raw handles GET /api/v1/media/raw/{id}: the untouched original with full
// HTTP range support via http.ServeFile — this is how audio tracks stream
// (seekable <audio> element) and how full-resolution photos are viewed.
func (h *StreamHandler) Raw(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadReady(w, r)
	if !ok {
		return
	}
	if item.MimeType != "" {
		w.Header().Set("Content-Type", item.MimeType)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, h.media.AbsPath(item.FilePath))
}

// Thumbnail handles GET /api/v1/media/thumb/{id}.
func (h *StreamHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadReady(w, r)
	if !ok {
		return
	}
	if item.ThumbnailPath == "" {
		writeError(w, r, fmt.Errorf("%w: no thumbnail", domain.ErrNotFound))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, h.media.AbsPath(item.ThumbnailPath))
}
