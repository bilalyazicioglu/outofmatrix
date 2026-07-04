package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"outofmatrix/internal/domain"
)

// UploadRepository implements domain.UploadRepository on PostgreSQL.
type UploadRepository struct {
	pool *pgxpool.Pool
}

var _ domain.UploadRepository = (*UploadRepository)(nil)

func NewUploadRepository(pool *pgxpool.Pool) *UploadRepository {
	return &UploadRepository{pool: pool}
}

const uploadSessionColumns = `id, user_id, filename, title, mime_type, total_size, chunk_size, total_chunks, created_at, expires_at`

func (r *UploadRepository) CreateSession(ctx context.Context, s *domain.UploadSession) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO upload_sessions (`+uploadSessionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		s.ID, s.UserID, s.Filename, s.Title, s.MimeType,
		s.TotalSize, s.ChunkSize, s.TotalChunks, s.CreatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upload repository: create session %s: %w", s.ID, err)
	}
	return nil
}

func (r *UploadRepository) FindSession(ctx context.Context, id uuid.UUID) (*domain.UploadSession, error) {
	var s domain.UploadSession
	err := r.pool.QueryRow(ctx, `
		SELECT `+uploadSessionColumns+`
		FROM upload_sessions
		WHERE id = $1`, id,
	).Scan(&s.ID, &s.UserID, &s.Filename, &s.Title, &s.MimeType,
		&s.TotalSize, &s.ChunkSize, &s.TotalChunks, &s.CreatedAt, &s.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("upload repository: find session %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("upload repository: find session %s: %w", id, err)
	}
	return &s, nil
}

func (r *UploadRepository) RecordChunk(ctx context.Context, sessionID uuid.UUID, index int, size int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO upload_chunks (session_id, chunk_index, size)
		VALUES ($1, $2, $3)
		ON CONFLICT (session_id, chunk_index) DO NOTHING`,
		sessionID, index, size,
	)
	if err != nil {
		return fmt.Errorf("upload repository: record chunk %d of %s: %w", index, sessionID, err)
	}
	return nil
}

func (r *UploadRepository) ListChunkIndexes(ctx context.Context, sessionID uuid.UUID) ([]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT chunk_index
		FROM upload_chunks
		WHERE session_id = $1
		ORDER BY chunk_index`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("upload repository: list chunks of %s: %w", sessionID, err)
	}
	defer rows.Close()

	var indexes []int
	for rows.Next() {
		var i int
		if err := rows.Scan(&i); err != nil {
			return nil, fmt.Errorf("upload repository: list chunks of %s: scan: %w", sessionID, err)
		}
		indexes = append(indexes, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("upload repository: list chunks of %s: %w", sessionID, err)
	}
	return indexes, nil
}

func (r *UploadRepository) CountChunks(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM upload_chunks WHERE session_id = $1`, sessionID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("upload repository: count chunks of %s: %w", sessionID, err)
	}
	return n, nil
}

func (r *UploadRepository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	// upload_chunks rows cascade.
	tag, err := r.pool.Exec(ctx, `DELETE FROM upload_sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("upload repository: delete session %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("upload repository: delete session %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *UploadRepository) ListExpired(ctx context.Context, before time.Time, limit int) ([]*domain.UploadSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+uploadSessionColumns+`
		FROM upload_sessions
		WHERE expires_at < $1
		ORDER BY expires_at
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("upload repository: list expired: %w", err)
	}
	defer rows.Close()

	var out []*domain.UploadSession
	for rows.Next() {
		var s domain.UploadSession
		if err := rows.Scan(&s.ID, &s.UserID, &s.Filename, &s.Title, &s.MimeType,
			&s.TotalSize, &s.ChunkSize, &s.TotalChunks, &s.CreatedAt, &s.ExpiresAt); err != nil {
			return nil, fmt.Errorf("upload repository: list expired: scan: %w", err)
		}
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("upload repository: list expired: %w", err)
	}
	return out, nil
}
