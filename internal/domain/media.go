package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MediaType classifies a stored file.
type MediaType string

const (
	MediaTypePhoto MediaType = "photo"
	MediaTypeAudio MediaType = "audio"
	MediaTypeVideo MediaType = "video"
)

func (t MediaType) Valid() bool {
	return t == MediaTypePhoto || t == MediaTypeAudio || t == MediaTypeVideo
}

// MediaTypeFromMime maps a MIME type to a MediaType. Unknown or unsupported
// MIME types return ErrInvalidInput.
func MediaTypeFromMime(mimeType string) (MediaType, error) {
	base := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.Index(base, ";"); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	switch {
	case strings.HasPrefix(base, "image/"):
		return MediaTypePhoto, nil
	case strings.HasPrefix(base, "audio/"):
		return MediaTypeAudio, nil
	case strings.HasPrefix(base, "video/"):
		return MediaTypeVideo, nil
	default:
		return "", fmt.Errorf("%w: unsupported mime type %q", ErrInvalidInput, mimeType)
	}
}

// MediaStatus tracks the background processing lifecycle of a MediaItem.
type MediaStatus string

const (
	MediaStatusPending    MediaStatus = "pending"    // uploaded, waiting for a worker
	MediaStatusProcessing MediaStatus = "processing" // a worker is on it
	MediaStatusReady      MediaStatus = "ready"      // metadata + derivatives generated
	MediaStatusFailed     MediaStatus = "failed"     // processing failed; see Metadata.ProcessingError
)

// MediaMetadata is the technical metadata extracted by ffprobe. It is stored
// as JSONB so new fields can be added without migrations.
type MediaMetadata struct {
	DurationSeconds float64           `json:"duration_seconds,omitempty"`
	Width           int               `json:"width,omitempty"`
	Height          int               `json:"height,omitempty"`
	VideoCodec      string            `json:"video_codec,omitempty"`
	AudioCodec      string            `json:"audio_codec,omitempty"`
	BitrateBps      int64             `json:"bitrate_bps,omitempty"`
	SampleRate      int               `json:"sample_rate,omitempty"`
	Channels        int               `json:"channels,omitempty"`
	FrameRate       float64           `json:"frame_rate,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	ProcessingError string            `json:"processing_error,omitempty"`
}

// MediaItem is a single stored photo, audio track or video.
//
// FilePath, ThumbnailPath and HLSPath are stored relative to the storage root
// so the whole library can be moved to another disk or host without a data
// migration.
type MediaItem struct {
	ID            uuid.UUID     `json:"id"`
	UserID        uuid.UUID     `json:"user_id"`
	Title         string        `json:"title"`
	FilePath      string        `json:"file_path"`
	Type          MediaType     `json:"type"`
	Status        MediaStatus   `json:"status"`
	FileSize      int64         `json:"file_size"`
	MimeType      string        `json:"mime_type"`
	BlurHash      string        `json:"blur_hash,omitempty"`
	ThumbnailPath string        `json:"thumbnail_path,omitempty"`
	HLSPath       string        `json:"hls_path,omitempty"`
	Metadata      MediaMetadata `json:"metadata"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// MediaRepository is the persistence port for media items.
type MediaRepository interface {
	Save(ctx context.Context, m *MediaItem) error
	Update(ctx context.Context, m *MediaItem) error
	FindByID(ctx context.Context, id uuid.UUID) (*MediaItem, error)
	// ListByUserID returns a page of a user's media, newest first. An empty
	// mediaType returns all types.
	ListByUserID(ctx context.Context, userID uuid.UUID, mediaType MediaType, limit, offset int) ([]*MediaItem, error)
	CountByUserID(ctx context.Context, userID uuid.UUID, mediaType MediaType) (int64, error)
	// ListIDsByStatus is used at boot to recover jobs that were queued or
	// in flight when the process last stopped.
	ListIDsByStatus(ctx context.Context, statuses []MediaStatus, limit int) ([]uuid.UUID, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
