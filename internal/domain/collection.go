package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CollectionType distinguishes Spotify-style playlists from Google
// Photos-style albums. Both share the same storage model.
type CollectionType string

const (
	CollectionTypePlaylist CollectionType = "playlist"
	CollectionTypeAlbum    CollectionType = "album"
)

func (t CollectionType) Valid() bool {
	return t == CollectionTypePlaylist || t == CollectionTypeAlbum
}

// Collection is an ordered, named set of media items owned by a user.
type Collection struct {
	ID        uuid.UUID      `json:"id"`
	UserID    uuid.UUID      `json:"user_id"`
	Name      string         `json:"name"`
	Type      CollectionType `json:"type"`
	CreatedAt time.Time      `json:"created_at"`
}

// CollectionItem is the membership of one MediaItem in one Collection.
type CollectionItem struct {
	CollectionID uuid.UUID `json:"collection_id"`
	MediaID      uuid.UUID `json:"media_id"`
	Position     int       `json:"position"`
	AddedAt      time.Time `json:"added_at"`
}

// CollectionRepository is the persistence port for collections.
type CollectionRepository interface {
	Create(ctx context.Context, c *Collection) error
	FindByID(ctx context.Context, id uuid.UUID) (*Collection, error)
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*Collection, error)
	AddItem(ctx context.Context, item *CollectionItem) error
	RemoveItem(ctx context.Context, collectionID, mediaID uuid.UUID) error
	// ListItems returns the collection's media ordered by position, then by
	// the time each item was added.
	ListItems(ctx context.Context, collectionID uuid.UUID) ([]*MediaItem, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
