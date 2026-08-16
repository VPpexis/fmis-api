// Package routers for batches
package routers

import (
	"context"
	"encoding/json"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/services"
	"fmis-api/internal/testutil"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testSecret = "test-secret"

// signTestToken creates a signed across token for the given role.
func signTestToken(t *testing.T, userID, role string) string {
	t.Helper()
	claims := &middleware.Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

// newTestRouter wires the full router against the real test database.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return New(
		services.NewAuthService(pool, testSecret, 15*time.Minute, 7*24*time.Hour),
		services.NewBatchService(pool),
		pool,
		testSecret,
		logger,
	)
}

// TestBratchesRoutesRequireAuth proves every batch route needs a token.
func TestBratchesRoutesRequireAuth(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST without token = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/batches/product/"+uuid.New().String(), http.NoBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without token = %d, want 401", rec.Code)
	}
}

// TestBatchesRoutesRBAC proves the ADMIN/OPERATOR-only guard on POST.
func TestBatchesRoutesRBAC(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)

	receivedAs := func(role string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"product_id": %q, "batch_number": "LOT-A", "quantity": "10.000", "expiration_date": "2027-01-01T00:00:00Z"}`, product.ID.String())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := receivedAs(string(models.UserRoleTypeViewer)); rec.Code != http.StatusForbidden {
		t.Errorf("VIEWER receive = %d, want 403", rec.Code)
	}
	if rec := receivedAs(string(models.UserRoleTypeOperator)); rec.Code != http.StatusCreated {
		t.Errorf("OPERATOR receive = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
}

// TestListBatchesHTTP is the full HTTP lifecycle: OPERATOR receives batches,
// VIEWER list them in FEFO order.
func TestListBatchesHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	router := newTestRouter(t, pool)

	receive := func(exp string) string {
		body := fmt.Sprintf(`{"product_id": %q, "batch_number": "LOT-%s", "quantity": "10.0000", "expiration_date": %s}`,
			product.ID.String(), uuid.NewString()[:8], exp)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("receive = %d, want 201; body: %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create resonse: %v", err)
		}
		return created.ID
	}

	lateID := receive(`"2028-01-01T00:00:00Z"`)
	earlyID := receive(`"2027-01-01T00:00:00Z"`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/batches/product/"+product.ID.String(), http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeViewer)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}

	var listed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("len(listed) = %d, want 2", len(listed))
	}
	if listed[0].ID != earlyID || listed[1].ID != lateID {
		t.Errorf("FEFO order voilated: got %+v, want [%s, %s]", listed, earlyID, lateID)
	}
}

// TestReceiveRejectsInvalidQuantities tests that the receive endpoint rejects invalid quantities.
func TestReceiveRejectsInvalidQuantities(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)

	tests := []struct {
		name     string
		quantity string
	}{
		{"zero", "0"},
		{"negative", "-5"},
		{"five decimals", "1.12345"},
		{"over precision", "1000000"},
		{"not a number", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"product_id": %q, "batch_number": "LOT-1", "quantity": %q, "expiration_date": "2028-01-01T00:00:00Z"}`, product.ID, tt.quantity)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("receive with quantity %q = %d, want 400; body: %s", tt.quantity, rec.Code, rec.Body.String())
			}

			var batches, txns int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM inventory_batches").Scan(&batches); err != nil {
				t.Fatalf("count batches: %v", err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM stock_transactions").Scan(&txns); err != nil {
				t.Fatalf("count transactions: %v", err)
			}
			if batches != 0 {
				t.Errorf("batch created despite invalid quantity %q", tt.quantity)
			}
			if txns != 0 {
				t.Errorf("stock transaction created despite invalid quantity %q", tt.quantity)
			}
		})
	}
}

// TestReceiveAcceptsValidQuantities tests that valid quantities are accepted.
func TestReceiveAcceptsValidQuantities(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)

	tests := []struct {
		name        string
		batchNumber string
		quantity    string
		want        string
	}{
		{"valid integer", "LOT-INT", "1", "1.0000"},
		{"valid decimal", "LOT-DEC", "0.0001", "0.0001"},
		{"valid maximum", "LOT-MAX", "999999.9999", "999999.9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"product_id": %q, "batch_number": %q, "quantity": %q, "expiration_date": "2028-01-01T00:00:00Z"}`, product.ID, tt.batchNumber, tt.quantity)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("receive with quantity %q = %d, want 201; body: %s", tt.quantity, rec.Code, rec.Body.String())
			}

			var initial, current, change string
			err := pool.QueryRow(ctx, `
				SELECT b.quantity_initial::text, b.quantity_current::text, t.quantity_change::text
				FROM inventory_batches b
				JOIN stock_transactions t ON t.batch_id = b.id
				WHERE b.batch_number = $1`, tt.batchNumber).Scan(&initial, &current, &change)
			if err != nil {
				t.Fatalf("query batch: %v", err)
			}
			if initial != tt.want || current != tt.want || change != tt.want {
				t.Errorf("quantity %q stored as initial=%q current=%q, change=%q, want %q", tt.quantity, initial, current, change, tt.want)
			}
		})
	}
}
