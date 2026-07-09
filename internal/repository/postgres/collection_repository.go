package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"outofmatrix/internal/domain"
)

// CollectionRepository implements domain.CollectionRepository on PostgreSQL.
type CollectionRepository struct {
	pool *pgxpool.Pool
}

var _ domain.CollectionRepository = (*CollectionRepository)(nil)

func NewCollectionRepository(pool *pgxpool.Pool) *CollectionRepository {
	return &CollectionRepository{pool: pool}
}

func (r *CollectionRepository) Create(ctx context.Context, c *domain.Collection) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO collections (id, user_id, name, type, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.UserID, c.Name, string(c.Type), c.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("collection repository: create %q: %w", c.Name, domain.ErrAlreadyExists)
		}
		return fmt.Errorf("collection repository: create %q: %w", c.Name, err)
	}
	return nil
}

func (r *CollectionRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Collection, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, type, created_at
		FROM collections
		WHERE id = $1`, id)
	c, err := scanCollection(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("collection repository: find %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("collection repository: find %s: %w", id, err)
	}
	return c, nil
}

func (r *CollectionRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Collection, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, type, created_at
		FROM collections
		WHERE user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("collection repository: list for user %s: %w", userID, err)
	}
	defer rows.Close()

	var out []*domain.Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, fmt.Errorf("collection repository: list for user %s: scan: %w", userID, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collection repository: list for user %s: %w", userID, err)
	}
	return out, nil
}

func (r *CollectionRepository) AddItem(ctx context.Context, item *domain.CollectionItem) error {
	// Upsert so re-adding an item just moves it to the requested position.
	_, err := r.pool.Exec(ctx, `
		INSERT INTO collection_items (collection_id, media_id, position, added_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (collection_id, media_id)
		DO UPDATE SET position = EXCLUDED.position`,
		item.CollectionID, item.MediaID, item.Position, item.AddedAt,
	)
	if err != nil {
		return fmt.Errorf("collection repository: add item %s to %s: %w", item.MediaID, item.CollectionID, err)
	}
	return nil
}

func (r *CollectionRepository) RemoveItem(ctx context.Context, collectionID, mediaID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM collection_items
		WHERE collection_id = $1 AND media_id = $2`,
		collectionID, mediaID,
	)
	if err != nil {
		return fmt.Errorf("collection repository: remove item %s from %s: %w", mediaID, collectionID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("collection repository: remove item %s from %s: %w", mediaID, collectionID, domain.ErrNotFound)
	}
	return nil
}

func (r *CollectionRepository) ListItems(ctx context.Context, collectionID uuid.UUID) ([]*domain.MediaItem, error) {
	// Column list must stay in sync with scanMediaItem — reuse mediaColumns.
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixColumns(mediaColumns, "m.")+`
		FROM collection_items ci
		JOIN media_items m ON m.id = ci.media_id
		WHERE ci.collection_id = $1
		ORDER BY ci.position, ci.added_at`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("collection repository: list items of %s: %w", collectionID, err)
	}
	defer rows.Close()

	var items []*domain.MediaItem
	for rows.Next() {
		m, err := scanMediaItem(rows)
		if err != nil {
			return nil, fmt.Errorf("collection repository: list items of %s: scan: %w", collectionID, err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collection repository: list items of %s: %w", collectionID, err)
	}
	return items, nil
}

func (r *CollectionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM collections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("collection repository: delete %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("collection repository: delete %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func scanCollection(row pgx.Row) (*domain.Collection, error) {
	var (
		c     domain.Collection
		ctype string
	)
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &ctype, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Type = domain.CollectionType(ctype)
	return &c, nil
}
