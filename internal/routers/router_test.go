// Package routers error envelope tests.
package routers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fmis-api/internal/testutil"
)

// TestNotFoundReturnsErrorEnvelope proves unknown routes respond with the
// shared {"error": ...} envelope instead of chi's plain-text 404.
func TestNotFoundReturnsErrorEnvelope(t *testing.T) {
	pool := testutil.Pool(t)
	router := newTestRouter(t, pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", http.NoBody)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not the error envelope: %v", rec.Body.String(), err)
	}
	if body.Error != "not found" {
		t.Errorf("404 body = %q, want %q", body.Error, "not found")
	}
}

// TestMethodNotAllowedReturnsErrorEnvelope proves wrong-method requests
// respond with the shared envelope too.
func TestMethodNotAllowedReturnsErrorEnvelope(t *testing.T) {
	pool := testutil.Pool(t)
	router := newTestRouter(t, pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/health", http.NoBody)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not the error envelope: %v", rec.Body.String(), err)
	}
	if body.Error != "method not allowed" {
		t.Errorf("405 body = %q, want %q", body.Error, "method not allowed")
	}
}
