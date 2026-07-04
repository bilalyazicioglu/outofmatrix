package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"outofmatrix/internal/domain"
)

// MediaRepository implements domain.MediaRepository on PostgreSQL using
// plain SQL over pgx.
type MediaRepository struct {
	pool *pgxpool.Pool
}

var _ domain.MediaRepository = (*MediaRepository)(nil)

func NewMediaRepository(pool *pgxpool.Pool) *MediaRepository {
	return &MediaRepository{pool: pool}
}

const mediaColumns = `id, user_id, title, file_path, type, status, file_size, mime_type,
	blur_hash, thumbnail_path, hls_path, metadata, created_at, updated_at`

func (r *MediaRepository) Save(ctx context.Context, m *domain.MediaItem) error {
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("media repository: marshal metadata: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO media_items (`+mediaColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		m.ID, m.UserID, m.Title, m.FilePath, string(m.Type), string(m.Status),
		m.FileSize, m.MimeType, m.BlurHash, m.ThumbnailPath, m.HLSPath, meta,
		m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("media repository: save %s: %w", m.ID, domain.ErrAlreadyExists)
		}
		return fmt.Errorf("media repository: save %s: %w", m.ID, err)
	}
	return nil
}

func (r *MediaRepository) Update(ctx context.Context, m *domain.MediaItem) error {
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("media repository: marshal metadata: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE media_items
		SET title = $2,
		    status = $3,
		    blur_hash = $4,
		    thumbnail_path = $5,
		    hls_path = $6,
		    metadata = $7,
		    updated_at = $8
		WHERE id = $1`,
		m.ID, m.Title, string(m.Status), m.BlurHash, m.ThumbnailPath,
		m.HLSPath, meta, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("media repository: update %s: %w", m.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("media repository: update %s: %w", m.ID, domain.ErrNotFound)
	}
	return nil
}

func (r *MediaRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.MediaItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+mediaColumns+`
		FROM media_items
		WHERE id = $1`, id)
	m, err := scanMediaItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("media repository: find %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("media repository: find %s: %w", id, err)
	}
	return m, nil
}

func (r *MediaRepository) ListByUserID(ctx context.Context, userID uuid.UUID, mediaType domain.MediaType, limit, offset int) ([]*domain.MediaItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+mediaColumns+`
		FROM media_items
		WHERE user_id = $1
		  AND ($2 = '' OR type = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4`,
		userID, string(mediaType), limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("media repository: list for user %s: %w", userID, err)
	}
	defer rows.Close()

	items := make([]*domain.MediaItem, 0, limit)
	for rows.Next() {
		m, err := scanMediaItem(rows)
		if err != nil {
			return nil, fmt.Errorf("media repository: list for user %s: scan: %w", userID, err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media repository: list for user %s: %w", userID, err)
	}
	return items, nil
}

func (r *MediaRepository) CountByUserID(ctx context.Context, userID uuid.UUID, mediaType domain.MediaType) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM media_items
		WHERE user_id = $1
		  AND ($2 = '' OR type = $2)`,
		userID, string(mediaType),
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("media repository: count for user %s: %w", userID, err)
	}
	return total, nil
}

func (r *MediaRepository) ListIDsByStatus(ctx context.Context, statuses []domain.MediaStatus, limit int) ([]uuid.UUID, error) {
	strStatuses := make([]string, len(statuses))
	for i, s := range statuses {
		strStatuses[i] = string(s)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id
		FROM media_items
		WHERE status = ANY($1)
		ORDER BY created_at
		LIMIT $2`,
		strStatuses, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("media repository: list ids by status: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("media repository: list ids by status: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("media repository: list ids by status: %w", err)
	}
	return ids, nil
}

func (r *MediaRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM media_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("media repository: delete %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("media repository: delete %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// scanMediaItem scans one row in mediaColumns order. It works for both
// pgx.Row and pgx.Rows.
func scanMediaItem(row pgx.Row) (*domain.MediaItem, error) {
	var (
		m        domain.MediaItem
		mtype    string
		status   string
		metaJSON []byte
	)
	err := row.Scan(
		&m.ID, &m.UserID, &m.Title, &m.FilePath, &mtype, &status,
		&m.FileSize, &m.MimeType, &m.BlurHash, &m.ThumbnailPath, &m.HLSPath,
		&metaJSON, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	m.Type = domain.MediaType(mtype)
	m.Status = domain.MediaStatus(status)
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &m.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return &m, nil
}
