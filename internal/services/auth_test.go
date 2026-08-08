// Package services for auth.
package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
