package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UploadSession is one resumable chunked upload in progress. The client
// declares the file size up front, receives a session ID plus chunk size,
// and PUTs chunks in any order; chunks land at chunk_index*chunk_size inside
// a sparse .part file, so nothing is ever buffered in RAM and an interrupted
// upload resumes exactly where it stopped — across reconnects and server
// restarts alike.
type UploadSession struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Filename    string    `json:"filename"`
	Title       string    `json:"title,omitempty"`
	MimeType    string    `json:"mime_type,omitempty"`
	TotalSize   int64     `json:"total_size"`
	ChunkSize   int64     `json:"chunk_size"`
	TotalChunks int       `json:"total_chunks"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// UploadRepository is the persistence port for upload sessions.
type UploadRepository interface {
	CreateSession(ctx context.Context, s *UploadSession) error
	FindSession(ctx context.Context, id uuid.UUID) (*UploadSession, error)
	// RecordChunk marks one chunk as received (idempotent).
	RecordChunk(ctx context.Context, sessionID uuid.UUID, index int, size int64) error
	ListChunkIndexes(ctx context.Context, sessionID uuid.UUID) ([]int, error)
	CountChunks(ctx context.Context, sessionID uuid.UUID) (int, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error
	ListExpired(ctx context.Context, before time.Time, limit int) ([]*UploadSession, error)
}
