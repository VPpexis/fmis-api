// Package services contains domain logic and owns transactions.
package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors the router maps to HTTP status codes.
var (
	ErrDuplicate          = errors.New("username or email already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// AuthService issues tokens and manages user credentials.
type AuthService struct {
	pool       *pgxpool.Pool
	users      *repositories.UserRepository
	refresh    *repositories.RefreshTokenRepository
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewAuthService wires the auth domain.
func NewAuthService(pool *pgxpool.Pool, jwtSecret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		pool:       pool,
		users:      &repositories.UserRepository{},
		refresh:    &repositories.RefreshTokenRepository{},
		jwtSecret:  []byte(jwtSecret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// Register creates a user and returns a token pair.
func (s *AuthService) Register(ctx context.Context, req schemas.RegisterRequest) (schemas.TokenResponse, error) {
	hash, err := hashPassword(req.Password)
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("hash password: %w", err)
	}

	refreshRaw, refreshHash, err := newRefreshToken()
	if err != nil {
		return schemas.TokenResponse{}, err
	}

	var user models.User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		user, err = s.users.CreateUser(ctx, tx, repositories.CreateUserParams{
			Username:     req.Username,
			Email:        req.Email,
			PasswordHash: hash,
			Role:         models.UserRoleTypeViewer,
		})
		if err != nil {
			return err
		}
		_, err = s.refresh.CreateRefreshToken(ctx, tx, repositories.CreateRefreshTokenParams{
			UserID:    user.ID,
			TokenHash: refreshHash,
			ExpiresAt: time.Now().Add(s.refreshTTL),
		})
		return err
	})
	if err != nil {
		if repositories.IsUniqueViolation(err) {
			return schemas.TokenResponse{}, ErrDuplicate
		}
		return schemas.TokenResponse{}, fmt.Errorf("register: %w", err)
	}
	access, err := s.issueAccessToken(&user)
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("issue access token: %w", err)
	}
	return tokenResponse(access, refreshRaw, s.accessTTL), nil
}

// Login verifies credentials and returns a token pair.
func (s *AuthService) Login(ctx context.Context, req schemas.LoginRequest) (schemas.TokenResponse, error) {
	user, err := s.users.GetUserByIdentifier(ctx, s.pool, req.Identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return schemas.TokenResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("get user: %w", err)
	}

	if !user.IsActive || !verifyPassword(user.PasswordHash, req.Password) {
		return schemas.TokenResponse{}, ErrInvalidCredentials
	}

	refreshRaw, refreshHash, err := newRefreshToken()
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("generate refresh token: %w", err)
	}

	_, err = s.refresh.CreateRefreshToken(ctx, s.pool, repositories.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().Add(s.refreshTTL),
	})
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("store refresh token: %w", err)
	}

	access, err := s.issueAccessToken(&user)
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("issue access token: %w", err)
	}
	return tokenResponse(access, refreshRaw, s.accessTTL), nil
}

// issueAccessToken signs a 15-minute HS256 JWT with the user's identity.
func (s *AuthService) issueAccessToken(user *models.User) (string, error) {
	claims := middleware.Claims{
		UserID: user.ID.String(),
		Role:   string(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

// newRefreshToken returns a raw token and its SHA-256 hash.
func newRefreshToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}

func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// hashPassword wraps bcrypt with default cost.
func hashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// tokenResponse builds the wire format with a stable token type and TTL in seconds.
func tokenResponse(access, refresh string, ttl time.Duration) schemas.TokenResponse {
	return schemas.TokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(ttl.Seconds()),
	}
}
