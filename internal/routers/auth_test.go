// Package routers for auth.
package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"
	"fmis-api/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// postJSON performs an HTTP call against router with the given method, path and JSON body.
func postJSON(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// registerViaAPI registers through the HTTP layer and returns the raw refresh token.
func registerViaAPI(t *testing.T, router http.Handler) string {
	t.Helper()
	body := fmt.Sprintf(`{"username": "api-%s", "email": "%s@example.com", "password": "correct-horse-battery"}`,
		uuid.NewString()[:8], uuid.NewString()[:8])
	rec := postJSON(router, http.MethodPost, "/api/v1/auth/register", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var tokens schemas.TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	return tokens.RefreshToken
}

// TestRefreshRouteRotatesToken proves the endpoint issues a new pair and kills the old token.
func TestRefreshRouteRotatesToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	refresh := registerViaAPI(t, router)

	rec := postJSON(router, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token": "`+refresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var tokens schemas.TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("refresh must return a new token pair")
	}

	rec = postJSON(router, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token": "`+refresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reuse of rotated token = %d, want 401", rec.Code)
	}
}

// TestRefreshRouteRejectsUnknownToken proves garbage tokens return 401.
func TestRefreshRouteRejectsUnknownToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	rec := postJSON(router, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token": "deadbeef00000000"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown token = %d, want 401", rec.Code)
	}
}

// TestLogoutRouteRevokesToken proves logout returns 204 and kills the token.
func TestLogoutRouteRevokesToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	refresh := registerViaAPI(t, router)

	rec := postJSON(router, http.MethodPost, "/api/v1/auth/logout", `{"refresh_token": "`+refresh+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	rec = postJSON(router, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token": "`+refresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout = %d, want 401", rec.Code)
	}
}

// TestMeRouteRequiresToken proves /me is behind the JWT middleware.
func TestMeRouteRequiresToken(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	rec := postJSON(router, http.MethodGet, "/api/v1/auth/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("me without token = %d, want 401", rec.Code)
	}
}

// TestMeRouteReturnsProfile proves /me reads the user from the JWT context.
func TestMeRouteReturnsProfile(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()

	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeAdmin)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(user.Role)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me with token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var me schemas.MeResponse
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if me.ID != user.ID.String() || me.Username != user.Username || me.Email != user.Email || me.Role != string(user.Role) {
		t.Errorf("me = %+v, want id %s username %s email %s role %s",
			me, user.ID.String(), user.Username, user.Email, string(user.Role))
	}
}
