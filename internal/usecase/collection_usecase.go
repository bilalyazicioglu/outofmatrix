package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"outofmatrix/internal/domain"
)

// CollectionUsecase manages playlists and albums with ownership enforcement.
type CollectionUsecase struct {
	collections domain.CollectionRepository
	media       domain.MediaRepository
}

func NewCollectionUsecase(collections domain.CollectionRepository, media domain.MediaRepository) *CollectionUsecase {
	return &CollectionUsecase{collections: collections, media: media}
}

func (c *CollectionUsecase) Create(ctx context.Context, userID uuid.UUID, name string, ctype domain.CollectionType) (*domain.Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: collection name must be 1-128 characters", domain.ErrInvalidInput)
	}
	if !ctype.Valid() {
		return nil, fmt.Errorf("%w: collection type must be playlist or album", domain.ErrInvalidInput)
	}
	col := &domain.Collection{
		ID:        uuid.New(),
		UserID:    userID,
		Name:      name,
		Type:      ctype,
		CreatedAt: time.Now().UTC(),
	}
	if err := c.collections.Create(ctx, col); err != nil {
		return nil, err
	}
	return col, nil
}

func (c *CollectionUsecase) List(ctx context.Context, userID uuid.UUID) ([]*domain.Collection, error) {
	return c.collections.ListByUserID(ctx, userID)
}

// Get returns a collection and its ordered items, enforcing ownership.
func (c *CollectionUsecase) Get(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, id uuid.UUID) (*domain.Collection, []*domain.MediaItem, error) {
	col, err := c.getOwned(ctx, requesterID, requesterRole, id)
	if err != nil {
		return nil, nil, err
	}
	items, err := c.collections.ListItems(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return col, items, nil
}

// AddItem places a media item in a collection. Both the collection and the
// media item must belong to the requester.
func (c *CollectionUsecase) AddItem(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, collectionID, mediaID uuid.UUID, position int) error {
	if _, err := c.getOwned(ctx, requesterID, requesterRole, collectionID); err != nil {
		return err
	}
	media, err := c.media.FindByID(ctx, mediaID)
	if err != nil {
		return err
	}
	if media.UserID != requesterID && requesterRole != domain.RoleAdmin {
		return fmt.Errorf("%w: media %s", domain.ErrForbidden, mediaID)
	}
	return c.collections.AddItem(ctx, &domain.CollectionItem{
		CollectionID: collectionID,
		MediaID:      mediaID,
		Position:     position,
		AddedAt:      time.Now().UTC(),
	})
}

func (c *CollectionUsecase) RemoveItem(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, collectionID, mediaID uuid.UUID) error {
	if _, err := c.getOwned(ctx, requesterID, requesterRole, collectionID); err != nil {
		return err
	}
	return c.collections.RemoveItem(ctx, collectionID, mediaID)
}

func (c *CollectionUsecase) Delete(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, id uuid.UUID) error {
	if _, err := c.getOwned(ctx, requesterID, requesterRole, id); err != nil {
		return err
	}
	return c.collections.Delete(ctx, id)
}

func (c *CollectionUsecase) getOwned(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, id uuid.UUID) (*domain.Collection, error) {
	col, err := c.collections.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if col.UserID != requesterID && requesterRole != domain.RoleAdmin {
		return nil, fmt.Errorf("%w: collection %s", domain.ErrForbidden, id)
	}
	return col, nil
}
