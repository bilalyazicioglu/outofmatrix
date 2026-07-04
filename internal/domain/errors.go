package domain

import "errors"

// Sentinel errors shared across all layers. Repositories and usecases wrap
// these with fmt.Errorf("...: %w", Err...) so the delivery layer can map them
// to HTTP status codes with errors.Is without knowing about SQL or FFmpeg.
var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrUnavailable   = errors.New("temporarily unavailable")
)
