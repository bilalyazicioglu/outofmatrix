package usecase

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"outofmatrix/internal/domain"
)

// Chunk size bounds. Clients may request their own chunk size within these
// limits; DefaultChunkSize is used when they don't.
const (
	MinChunkSize     = 1 << 20  // 1 MiB
	MaxChunkSize     = 64 << 20 // 64 MiB
	DefaultChunkSize = 8 << 20  // 8 MiB
)

// UploadUsecase implements resumable chunked uploads. Each chunk is written
// at index*chunkSize into a sparse staging file with WriteAt, so memory use
// is one copy buffer regardless of file size, chunks may arrive in any order
// or in parallel, and an interrupted upload resumes after asking the server
// which chunks it already has.
type UploadUsecase struct {
	repo    domain.UploadRepository
	media   *MediaUsecase
	maxSize int64
	ttl     time.Duration
	log     *slog.Logger
}

func NewUploadUsecase(repo domain.UploadRepository, media *MediaUsecase, maxSize int64, ttl time.Duration, log *slog.Logger) *UploadUsecase {
	if log == nil {
		log = slog.Default()
	}
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return &UploadUsecase{repo: repo, media: media, maxSize: maxSize, ttl: ttl, log: log}
}

// CreateSession opens a new resumable upload.
func (u *UploadUsecase) CreateSession(ctx context.Context, userID uuid.UUID, filename, title, mimeType string, totalSize, chunkSize int64) (*domain.UploadSession, error) {
	if strings.TrimSpace(filename) == "" {
		return nil, fmt.Errorf("%w: filename is required", domain.ErrInvalidInput)
	}
	if totalSize <= 0 {
		return nil, fmt.Errorf("%w: size must be positive", domain.ErrInvalidInput)
	}
	if totalSize > u.maxSize {
		return nil, fmt.Errorf("%w: file exceeds the %d byte limit", domain.ErrInvalidInput, u.maxSize)
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
	}
	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}

	now := time.Now().UTC()
	session := &domain.UploadSession{
		ID:          uuid.New(),
		UserID:      userID,
		Filename:    filepath.Base(filename),
		Title:       strings.TrimSpace(title),
		MimeType:    strings.TrimSpace(mimeType),
		TotalSize:   totalSize,
		ChunkSize:   chunkSize,
		TotalChunks: int((totalSize + chunkSize - 1) / chunkSize),
		CreatedAt:   now,
		ExpiresAt:   now.Add(u.ttl),
	}

	// Create the staging file up front so every later chunk write is a plain
	// open+WriteAt with no create/exists races between parallel chunks.
	f, err := os.OpenFile(u.media.UploadStagingPath(session.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("upload: create staging file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("upload: close staging file: %w", err)
	}

	if err := u.repo.CreateSession(ctx, session); err != nil {
		_ = os.Remove(u.media.UploadStagingPath(session.ID))
		return nil, err
	}
	return session, nil
}

// getOwned loads a session and enforces ownership and expiry.
func (u *UploadUsecase) getOwned(ctx context.Context, userID, sessionID uuid.UUID) (*domain.UploadSession, error) {
	session, err := u.repo.FindSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.UserID != userID {
		return nil, fmt.Errorf("%w: upload session %s", domain.ErrForbidden, sessionID)
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, fmt.Errorf("%w: upload session expired", domain.ErrNotFound)
	}
	return session, nil
}

// SaveChunk streams one chunk's bytes into the staging file at its exact
// offset. Re-uploading a chunk is harmless (same bytes, same offset), which
// makes client retry logic trivial.
func (u *UploadUsecase) SaveChunk(ctx context.Context, userID, sessionID uuid.UUID, index int, body io.Reader) error {
	session, err := u.getOwned(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if index < 0 || index >= session.TotalChunks {
		return fmt.Errorf("%w: chunk index %d out of range [0,%d)", domain.ErrInvalidInput, index, session.TotalChunks)
	}

	expected := session.ChunkSize
	if index == session.TotalChunks-1 {
		expected = session.TotalSize - int64(index)*session.ChunkSize
	}

	f, err := os.OpenFile(u.media.UploadStagingPath(session.ID), os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("upload: open staging file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(io.NewOffsetWriter(f, int64(index)*session.ChunkSize), io.LimitReader(body, expected))
	if err != nil {
		return fmt.Errorf("upload: write chunk %d: %w", index, err)
	}
	if written != expected {
		return fmt.Errorf("%w: chunk %d is %d bytes, expected %d", domain.ErrInvalidInput, index, written, expected)
	}
	// Anything left in the body means the client sent an oversized chunk.
	var overflow [1]byte
	if n, _ := body.Read(overflow[:]); n > 0 {
		return fmt.Errorf("%w: chunk %d larger than expected %d bytes", domain.ErrInvalidInput, index, expected)
	}

	return u.repo.RecordChunk(ctx, session.ID, index, expected)
}

// Status reports which chunks have been received, enabling resume.
func (u *UploadUsecase) Status(ctx context.Context, userID, sessionID uuid.UUID) (*domain.UploadSession, []int, error) {
	session, err := u.getOwned(ctx, userID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	received, err := u.repo.ListChunkIndexes(ctx, session.ID)
	if err != nil {
		return nil, nil, err
	}
	return session, received, nil
}

// Complete verifies every chunk arrived, promotes the staged file to a real
// MediaItem (which queues background processing) and disposes the session.
func (u *UploadUsecase) Complete(ctx context.Context, userID, sessionID uuid.UUID) (*domain.MediaItem, error) {
	session, err := u.getOwned(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}

	count, err := u.repo.CountChunks(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	if count != session.TotalChunks {
		return nil, fmt.Errorf("%w: %d of %d chunks received", domain.ErrInvalidInput, count, session.TotalChunks)
	}

	stagingPath := u.media.UploadStagingPath(session.ID)
	info, err := os.Stat(stagingPath)
	if err != nil {
		return nil, fmt.Errorf("upload: stat staging file: %w", err)
	}
	if info.Size() != session.TotalSize {
		return nil, fmt.Errorf("%w: assembled file is %d bytes, expected %d", domain.ErrInvalidInput, info.Size(), session.TotalSize)
	}

	mimeType := session.MimeType
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = detectFileMime(session.Filename, stagingPath)
	}

	item, err := u.media.CreateFromFile(ctx, userID, session.Title, session.Filename, mimeType, stagingPath)
	if err != nil {
		return nil, err
	}
	if err := u.repo.DeleteSession(ctx, session.ID); err != nil {
		// The media item is live; a leftover session row is only clutter and
		// the janitor will collect it once it expires.
		u.log.Warn("upload: session cleanup failed", "session_id", session.ID, "error", err)
	}
	return item, nil
}

// Abort discards a session and its staged bytes.
func (u *UploadUsecase) Abort(ctx context.Context, userID, sessionID uuid.UUID) error {
	session, err := u.getOwned(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if err := u.repo.DeleteSession(ctx, session.ID); err != nil {
		return err
	}
	if err := os.Remove(u.media.UploadStagingPath(session.ID)); err != nil && !os.IsNotExist(err) {
		u.log.Warn("upload: staging file not removed", "session_id", session.ID, "error", err)
	}
	return nil
}

// CleanupExpired removes stale sessions and their staging files. Run it
// periodically from a janitor goroutine.
func (u *UploadUsecase) CleanupExpired(ctx context.Context) (int, error) {
	sessions, err := u.repo.ListExpired(ctx, time.Now().UTC(), 100)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, s := range sessions {
		if err := u.repo.DeleteSession(ctx, s.ID); err != nil {
			u.log.Warn("upload janitor: delete session", "session_id", s.ID, "error", err)
			continue
		}
		if err := os.Remove(u.media.UploadStagingPath(s.ID)); err != nil && !os.IsNotExist(err) {
			u.log.Warn("upload janitor: remove staging file", "session_id", s.ID, "error", err)
		}
		removed++
	}
	return removed, nil
}

// detectFileMime resolves a MIME type from the filename extension, falling
// back to sniffing the file's first 512 bytes.
func detectFileMime(filename, path string) string {
	if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); byExt != "" {
		return byExt
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	return http.DetectContentType(head[:n])
}
