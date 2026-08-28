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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors the router maps to HTTP status codes.
var (
	ErrDuplicate           = errors.New("username or email already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)

// AuthService issues tokens and manages user credentials.
type AuthService struct {
	db         repositories.Querier
	tx         TxStarter
	users      UserStore
	refresh    RefreshTokenStore
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewAuthService wires the auth domain against a real connection pool.
func NewAuthService(pool *pgxpool.Pool, jwtSecret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return NewAuthServiceWithDeps(
		pool, poolTxStarter(pool),
		&repositories.UserRepository{},
		&repositories.RefreshTokenRepository{},
		jwtSecret, accessTTL, refreshTTL,
	)
}

// NewAuthServiceWithDeps wires the auth domain with injectable dependencies,
// enabling unit tests with mocked repositories and a fake transaction runner.
func NewAuthServiceWithDeps(db repositories.Querier, tx TxStarter, users UserStore, refresh RefreshTokenStore, jwtSecret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		db:         db,
		tx:         tx,
		users:      users,
		refresh:    refresh,
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
	err = s.tx(ctx, func(tx pgx.Tx) error {
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
	user, err := s.users.GetUserByIdentifier(ctx, s.db, req.Identifier)
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

	_, err = s.refresh.CreateRefreshToken(ctx, s.db, repositories.CreateRefreshTokenParams{
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

// Refresh rotates a refresh token and returns a new access token pair.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (schemas.TokenResponse, error) {
	hash := hashToken(refreshToken)
	token, err := s.refresh.FindByHash(ctx, s.db, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return schemas.TokenResponse{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("find refresh token: %w", err)
	}
	if token.Revoked {
		return schemas.TokenResponse{}, ErrInvalidRefreshToken
	}
	if token.ExpiresAt.Before(time.Now()) {
		return schemas.TokenResponse{}, ErrInvalidRefreshToken
	}

	user, err := s.users.GetUserByID(ctx, s.db, token.UserID)
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("get user: %w", err)
	}
	if !user.IsActive {
		return schemas.TokenResponse{}, ErrInvalidRefreshToken
	}

	refreshRaw, refreshHash, err := newRefreshToken()
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("generate refresh token: %w", err)
	}

	err = s.tx(ctx, func(tx pgx.Tx) error {
		if getErr := s.refresh.Revoke(ctx, tx, hash); getErr != nil {
			return getErr
		}
		_, getErr := s.refresh.CreateRefreshToken(ctx, tx, repositories.CreateRefreshTokenParams{
			UserID:    user.ID,
			TokenHash: refreshHash,
			ExpiresAt: time.Now().Add(s.refreshTTL),
		})
		return getErr
	})
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	access, err := s.issueAccessToken(&user)
	if err != nil {
		return schemas.TokenResponse{}, fmt.Errorf("issue access token: %w", err)
	}
	return tokenResponse(access, refreshRaw, s.accessTTL), nil
}

// Logout revokes the given refresh token.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	hash := hashToken(refreshToken)
	err := s.refresh.Revoke(ctx, s.db, hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

// Me returns the current user.
func (s *AuthService) Me(ctx context.Context, userID string) (schemas.MeResponse, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return schemas.MeResponse{}, ErrInvalidCredentials
	}

	user, err := s.users.GetUserByID(ctx, s.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return schemas.MeResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return schemas.MeResponse{}, fmt.Errorf("get user: %w", err)
	}

	return schemas.MeResponse{
		ID:       user.ID.String(),
		Username: user.Username,
		Email:    user.Email,
		Role:     string(user.Role),
	}, nil
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
	return raw, hashToken(raw), nil
}

// hashToken
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// verifyPassword compares a bcrypt hash with a plain
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
