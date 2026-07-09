package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	blur_hash, thumbnail_path, hls_path, is_favorite, captured_at, metadata, created_at, updated_at`

// prefixColumns qualifies every column in a comma-separated list with a table
// alias, e.g. prefixColumns("id, title", "m.") → "m.id, m.title".
func prefixColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func (r *MediaRepository) Save(ctx context.Context, m *domain.MediaItem) error {
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("media repository: marshal metadata: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO media_items (`+mediaColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		m.ID, m.UserID, m.Title, m.FilePath, string(m.Type), string(m.Status),
		m.FileSize, m.MimeType, m.BlurHash, m.ThumbnailPath, m.HLSPath,
		m.IsFavorite, m.CapturedAt, meta, m.CreatedAt, m.UpdatedAt,
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
	// is_favorite is deliberately absent: it is owned by SetFavorite so a
	// favorite toggled mid-processing is never clobbered by the pipeline.
	tag, err := r.pool.Exec(ctx, `
		UPDATE media_items
		SET title = $2,
		    status = $3,
		    blur_hash = $4,
		    thumbnail_path = $5,
		    hls_path = $6,
		    captured_at = $7,
		    metadata = $8,
		    updated_at = $9
		WHERE id = $1`,
		m.ID, m.Title, string(m.Status), m.BlurHash, m.ThumbnailPath,
		m.HLSPath, m.CapturedAt, meta, m.UpdatedAt,
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

// mediaFilterSQL builds the WHERE conditions (beyond user_id = $1) and the
// matching argument list for a MediaListOptions filter.
func mediaFilterSQL(userID uuid.UUID, opts domain.MediaListOptions) (string, []any) {
	conds := []string{"user_id = $1"}
	args := []any{userID}

	if opts.Type != "" {
		args = append(args, string(opts.Type))
		conds = append(conds, fmt.Sprintf("type = $%d", len(args)))
	}
	if opts.FavoritesOnly {
		conds = append(conds, "is_favorite")
	}
	if opts.Query != "" {
		args = append(args, "%"+escapeLike(opts.Query)+"%")
		conds = append(conds, fmt.Sprintf("title ILIKE $%d", len(args)))
	}
	return strings.Join(conds, " AND "), args
}

// escapeLike neutralises LIKE wildcards in user input (backslash is the
// default LIKE escape character in PostgreSQL).
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// mediaOrderSQL maps a validated MediaSort to a safe ORDER BY clause. The
// expressions mirror the indexes in migration 003. Sort values are
// whitelisted here — never interpolated from raw input.
func mediaOrderSQL(opts domain.MediaListOptions) string {
	dir := "DESC"
	if opts.Ascending {
		dir = "ASC"
	}
	switch opts.Sort {
	case domain.MediaSortName:
		return fmt.Sprintf("lower(title) %s, id %s", dir, dir)
	case domain.MediaSortCaptured:
		return fmt.Sprintf("COALESCE(captured_at, created_at) %s, id %s", dir, dir)
	default: // MediaSortAdded
		return fmt.Sprintf("created_at %s, id %s", dir, dir)
	}
}

func (r *MediaRepository) ListByUserID(ctx context.Context, userID uuid.UUID, opts domain.MediaListOptions) ([]*domain.MediaItem, error) {
	where, args := mediaFilterSQL(userID, opts)
	args = append(args, opts.Limit, opts.Offset)
	query := fmt.Sprintf(`
		SELECT %s
		FROM media_items
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		mediaColumns, where, mediaOrderSQL(opts), len(args)-1, len(args),
	)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("media repository: list for user %s: %w", userID, err)
	}
	defer rows.Close()

	items := make([]*domain.MediaItem, 0, opts.Limit)
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

func (r *MediaRepository) CountByUserID(ctx context.Context, userID uuid.UUID, opts domain.MediaListOptions) (int64, error) {
	where, args := mediaFilterSQL(userID, opts)
	var total int64
	err := r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM media_items WHERE %s`, where),
		args...,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("media repository: count for user %s: %w", userID, err)
	}
	return total, nil
}

func (r *MediaRepository) SetFavorite(ctx context.Context, id uuid.UUID, favorite bool) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE media_items
		SET is_favorite = $2, updated_at = now()
		WHERE id = $1`,
		id, favorite,
	)
	if err != nil {
		return fmt.Errorf("media repository: set favorite %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("media repository: set favorite %s: %w", id, domain.ErrNotFound)
	}
	return nil
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
		&m.IsFavorite, &m.CapturedAt, &metaJSON, &m.CreatedAt, &m.UpdatedAt,
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
