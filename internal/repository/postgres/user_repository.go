package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"outofmatrix/internal/domain"
)

// UserRepository implements domain.UserRepository on PostgreSQL.
type UserRepository struct {
	pool *pgxpool.Pool
}

var _ domain.UserRepository = (*UserRepository)(nil)

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, role, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		u.ID, u.Username, u.PasswordHash, string(u.Role), u.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("user repository: create %q: %w", u.Username, domain.ErrAlreadyExists)
		}
		return fmt.Errorf("user repository: create %q: %w", u.Username, err)
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at
		FROM users
		WHERE id = $1`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user repository: find %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("user repository: find %s: %w", id, err)
	}
	return u, nil
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at
		FROM users
		WHERE lower(username) = lower($1)`, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user repository: find %q: %w", username, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("user repository: find %q: %w", username, err)
	}
	return u, nil
}

func scanUser(row pgx.Row) (*domain.User, error) {
	var (
		u    domain.User
		role string
	)
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &u.CreatedAt); err != nil {
		return nil, err
	}
	u.Role = domain.Role(role)
	return &u, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique_violation
// (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
