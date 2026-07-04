package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"outofmatrix/internal/domain"
)

// Claims is the JWT payload for authenticated sessions.
type Claims struct {
	UserID uuid.UUID   `json:"uid"`
	Role   domain.Role `json:"role"`
	jwt.RegisteredClaims
}

// AuthUsecase handles registration, login and token validation.
type AuthUsecase struct {
	users  domain.UserRepository
	secret []byte
	ttl    time.Duration
}

func NewAuthUsecase(users domain.UserRepository, secret string, ttl time.Duration) *AuthUsecase {
	return &AuthUsecase{
		users:  users,
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Register creates a new account. The first account on the server becomes an
// admin would be a nice touch for real deployments; here every account is a
// regular user unless promoted directly in the database.
func (a *AuthUsecase) Register(ctx context.Context, username, password string) (*domain.User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, fmt.Errorf("%w: username must be 3-64 characters", domain.ErrInvalidInput)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password must be at least 8 characters", domain.ErrInvalidInput)
	}
	if len(password) > 72 {
		// bcrypt silently truncates beyond 72 bytes; reject instead.
		return nil, fmt.Errorf("%w: password must be at most 72 characters", domain.ErrInvalidInput)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	user := &domain.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: string(hash),
		Role:         domain.RoleUser,
		CreatedAt:    time.Now().UTC(),
	}
	if err := a.users.Create(ctx, user); err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			return nil, fmt.Errorf("%w: username %q is taken", domain.ErrAlreadyExists, username)
		}
		return nil, err
	}
	return user, nil
}

// Login verifies credentials and returns a signed JWT plus the user. Both a
// missing user and a wrong password surface as the same ErrUnauthorized so
// the API does not leak which usernames exist.
func (a *AuthUsecase) Login(ctx context.Context, username, password string) (string, *domain.User, error) {
	user, err := a.users.FindByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, fmt.Errorf("%w: invalid credentials", domain.ErrUnauthorized)
		}
		return "", nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", nil, fmt.Errorf("%w: invalid credentials", domain.ErrUnauthorized)
	}

	now := time.Now().UTC()
	claims := &Claims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)),
			Issuer:    "outofmatrix",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
	if err != nil {
		return "", nil, fmt.Errorf("auth: sign token: %w", err)
	}
	return token, user, nil
}

// ValidateToken parses and verifies a JWT, returning its claims.
func (a *AuthUsecase) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnauthorized, err)
	}
	if !token.Valid || claims.UserID == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid token", domain.ErrUnauthorized)
	}
	return claims, nil
}
