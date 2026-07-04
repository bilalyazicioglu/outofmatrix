package usecase

import (
	"github.com/google/uuid"

	"outofmatrix/internal/domain"
)

// Media lifecycle statuses pushed over WebSocket.
const (
	EventQueued     = "queued"
	EventProcessing = "processing"
	EventCompleted  = "completed"
	EventFailed     = "failed"
)

// MediaEvent is one real-time status update for a media item. Progress is an
// overall 0-100 percentage across the whole pipeline (probe, thumbnail,
// transcode), so the UI can drive a single progress bar with it.
type MediaEvent struct {
	MediaID  uuid.UUID        `json:"media_id"`
	Title    string           `json:"title,omitempty"`
	Type     domain.MediaType `json:"type,omitempty"`
	Status   string           `json:"status"`
	Stage    string           `json:"stage,omitempty"` // probe | thumbnail | transcode
	Progress float64          `json:"progress"`
	Error    string           `json:"error,omitempty"`
}

// Notifier pushes events to a user's connected clients. The WebSocket hub
// implements it; a nil notifier is replaced by a no-op so the pipeline never
// depends on anyone listening.
type Notifier interface {
	NotifyMedia(userID uuid.UUID, evt MediaEvent)
}

type noopNotifier struct{}

func (noopNotifier) NotifyMedia(uuid.UUID, MediaEvent) {}
