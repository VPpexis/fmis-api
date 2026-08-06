package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

// signToken creates a signed access token carrying the given claims.
func signToken(t *testing.T, secret, userID, role string, exp time.Time) string {
	t.Helper()
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}
	return signed
}

// contextCapturer records what the wrapped handler saw in its request context.
type contextCapturer struct {
	userID string
	role   string
	called bool
}

func (c *contextCapturer) capture(_ http.ResponseWriter, r *http.Request) {
	c.called = true
	c.userID = UserIDFromContext(r.Context())
	c.role = RoleFromContext(r.Context())
}

// serve runs the Auth middleware around a capturing handler with the given
// Authorization header value ("" means the header is absent).
func serve(t *testing.T, secret, authHeader string) (*httptest.ResponseRecorder, *contextCapturer) {
	t.Helper()
	capturer := &contextCapturer{}
	req := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	Auth(secret)(http.HandlerFunc(capturer.capture)).ServeHTTP(rec, req)
	return rec, capturer
}

func TestAuthValidTokenPopulatesContext(t *testing.T) {
	token := signToken(t, testSecret, "user-123", "ADMIN", time.Now().Add(time.Minute))

	rec, capturer := serve(t, testSecret, "Bearer "+token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !capturer.called {
		t.Fatal("handler was not reached")
	}
	if capturer.userID != "user-123" {
		t.Errorf("userID = %q, want %q", capturer.userID, "user-123")
	}
	if capturer.role != "ADMIN" {
		t.Errorf("role = %q, want %q", capturer.role, "ADMIN")
	}
}

func TestAuthRejectsUnauthenticatedRequests(t *testing.T) {
	expired := signToken(t, testSecret, "user-123", "OPERATOR", time.Now().Add(-time.Minute))
	wrongSig := signToken(t, "another-secret", "user-123", "OPERATOR", time.Now().Add(time.Minute))

	tests := []struct {
		name       string
		authHeader string
	}{
		{name: "missing header", authHeader: ""},
		{name: "malformed header", authHeader: "Basic abc123"},
		{name: "bare token without Bearer scheme", authHeader: tokenWithoutBearerPrefix(t)},
		{name: "expired token", authHeader: "Bearer " + expired},
		{name: "wrong signature", authHeader: "Bearer " + wrongSig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, capturer := serve(t, testSecret, tt.authHeader)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if capturer.called {
				t.Error("handler ran despite invalid token")
			}
		})
	}
}

// tokenWithoutBearerPrefix returns a structurally valid token used to prove
// that a missing "Bearer " scheme is rejected even when the token itself is fine.
func tokenWithoutBearerPrefix(t *testing.T) string {
	return signToken(t, testSecret, "user-123", "VIEWER", time.Now().Add(time.Minute))
}
