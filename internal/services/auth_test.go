// Package services for auth.
package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"
	"fmis-api/internal/testutil"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestHashAndVerifyPassword tests the bcrypt helpers.
func TestHashAndVerifyPassword(t *testing.T) {
	const password = "correct-horse-battery"

	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if hash == password {
		t.Error("hash must never equal the plaintext password")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash %q does not look like bcrypt (want $2... prefix)", hash)
	}

	if !verifyPassword(hash, password) {
		t.Error("verifyPassword should accept the correct password")
	}

	tests := []struct{ name, attempt string }{
		{"wrong password", "wrong-password"},
		{"empty password", ""},
		{"similar password", "correct-horse-better"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if verifyPassword(hash, tt.attempt) {
				t.Errorf("verfiyPassword accepted %q for a different password", tt.attempt)
			}
		})
	}
}

// TestNewRefreshToken checks the raw token and its SHA-256 hash
func TestNewRefreshToken(t *testing.T) {
	raw, hash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}

	if len(raw) != 64 || len(hash) != 64 {
		t.Fatalf("raw len = %d, hash len = %d; want 64 hex chars each", len(raw), len(hash))
	}

	sum := sha256.Sum256([]byte(raw))
	if want := hex.EncodeToString(sum[:]); hash != want {
		t.Errorf("hash = %s, want sha256(raw) = %s", hash, want)
	}

	other, _, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}
	if other == raw {
		t.Fatalf("two consecutive tokens must differ")
	}
}

// TestIssueAccessToken tests the access token issuance.
func TestIssueAccessToken(t *testing.T) {
	user := models.User{
		ID:   uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		Role: models.UserRoleTypeViewer,
	}

	svc := NewAuthService(nil, "test-secret", 15*time.Minute, 7*24*time.Hour)
	token, err := svc.issueAccessToken(&user)
	if err != nil {
		t.Fatalf("issueAccessToken: %v", err)
	}

	claims := &middleware.Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims,
		func(_ *jwt.Token) (any, error) { return []byte("test-secret"), nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil {
		t.Fatalf("parsing issued token: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("issued token should be valid")
	}
	if claims.UserID != user.ID.String() {
		t.Errorf("user_id = %q, want %q", claims.UserID, user.ID.String())
	}
	if claims.Role != string(models.UserRoleTypeViewer) {
		t.Errorf("role = %q, want %q", claims.Role, string(models.UserRoleTypeViewer))
	}

	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 14*time.Minute || ttl > 15*time.Minute {
		t.Errorf("token TTL = %v, want ~15m", ttl)
	}
}

// TestIssueAccessTokenRejectsWrongSecret tests access token if wrong secret is rejects.
func TestIssueAccessTokenRejectsWrongSecret(t *testing.T) {
	svc := NewAuthService(nil, "test-secret", 15*time.Minute, 7*24*time.Hour)
	token, err := svc.issueAccessToken(&models.User{
		ID:   uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		Role: models.UserRoleTypeAdmin,
	})
	if err != nil {
		t.Fatalf("issueAccessToken: %v", err)
	}

	_, err = jwt.ParseWithClaims(token, &middleware.Claims{},
		func(_ *jwt.Token) (any, error) { return []byte("wrong-secret"), nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err == nil {
		t.Error("token signed with a differentsecret must fail verification")
	}
}

// TestTokenResponse checks the OAuth-style response shape.
func TestTokenResponse(t *testing.T) {
	resp := tokenResponse("access", "refresh", 15*time.Minute)

	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900 (15 in seconds)", resp.ExpiresIn)
	}
	if resp.AccessToken != "access" || resp.RefreshToken != "refresh" {
		t.Error("tokens must pass through unchanged")
	}
}

// newTestAuthService builds an AuthService against the caller's test pool.
func newTestAuthService(pool *pgxpool.Pool) *AuthService {
	return NewAuthService(pool, "test-secret", 15*time.Minute, 7*24*time.Hour)
}

// registerUser registers a unique user through the service and returns the token pair.
func registerUser(ctx context.Context, t *testing.T, svc *AuthService) schemas.TokenResponse {
	t.Helper()
	tokens, err := svc.Register(ctx, schemas.RegisterRequest{
		Username: "learner-" + uuid.NewString()[:8],
		Email:    uuid.NewString()[:8] + "@example.com",
		Password: "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return tokens
}

// TestRefreshRotatesToken proves the old token dies and a new pair is issued.
func TestRefreshRotatesToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := newTestAuthService(pool)

	tokens := registerUser(ctx, t, svc)

	rotated, err := svc.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if rotated.AccessToken == "" || rotated.RefreshToken == "" {
		t.Fatal("Refresh must return a new token pair")
	}
	if rotated.RefreshToken == tokens.RefreshToken {
		t.Error("refresh token must rotate to a new value")
	}

	if _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("reusing the rotated token: err = %v, want ErrInvalidRefreshToken", err)
	}
}

// TestRefreshRejectsUnknownToken proves garbage tokens get a 401-class error.
func TestRefreshRejectsUnknownToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc := newTestAuthService(pool)

	if _, err := svc.Refresh(context.Background(), "deadbeef"+strings.Repeat("0", 56)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("unknown token: err = %v, want ErrInvalidRefreshToken", err)
	}
}

// TestRefreshRejectsRevokedToken proves revoked tokens are rejected.
func TestRefreshRejectsRevokedToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := newTestAuthService(pool)

	tokens := registerUser(ctx, t, svc)
	if err := svc.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("refresh after logout: err = %v, want ErrInvalidRefreshToken", err)
	}
}

// TestRefreshRejectsExpiredToken backdates the stored token and expects rejection.
func TestRefreshRejectsExpiredToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := newTestAuthService(pool)

	tokens := registerUser(ctx, t, svc)
	if _, err := pool.Exec(ctx, `UPDATE refresh_tokens SET expires_at = now() - interval '1 hour'`); err != nil {
		t.Fatalf("backdate token: %v", err)
	}

	if _, err := svc.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Errorf("expired token: err = %v, want ErrInvalidRefreshToken", err)
	}
}

// TestLogoutMarksTokenRevoked proves logout flips the DB flag and stays idempotent.
func TestLogoutMarksTokenRevoked(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := newTestAuthService(pool)

	tokens := registerUser(ctx, t, svc)
	if err := svc.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	var revoked bool
	if err := pool.QueryRow(ctx, `SELECT revoked FROM refresh_tokens WHERE token_hash = $1`, hashToken(tokens.RefreshToken)).Scan(&revoked); err != nil {
		t.Fatalf("query token: %v", err)
	}
	if !revoked {
		t.Error("logout must mark the stored token revoked")
	}

	if err := svc.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Errorf("second logout should succeed (idempotent), got %v", err)
	}
}

// TestMe returns the profile for an authenticated user and rejects unknown IDs.
func TestMe(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := newTestAuthService(pool)

	req := schemas.RegisterRequest{
		Username: "me-user-" + uuid.NewString()[:8],
		Email:    uuid.NewString()[:8] + "@example.com",
		Password: "correct-horse-battery",
	}
	if _, err := svc.Register(ctx, req); err != nil {
		t.Fatalf("register: %v", err)
	}

	var id uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE username = $1`, req.Username).Scan(&id); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	me, err := svc.Me(ctx, id.String())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Username != req.Username || me.Email != req.Email {
		t.Errorf("Me = %+v, want username %q email %q", me, req.Username, req.Email)
	}
	if me.Role != string(models.UserRoleTypeViewer) {
		t.Errorf("role = %q, want %q", me.Role, string(models.UserRoleTypeViewer))
	}

	if _, err := svc.Me(ctx, uuid.New().String()); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Me(ctx, "not-a-uuid"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("malformed id: err = %v, want ErrInvalidCredentials", err)
	}
}
