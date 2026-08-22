// Package routers for inventory.
package routers

import (
	"context"
	"encoding/json"
	"fmis-api/internal/models"
	"fmis-api/internal/testutil"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// receiveBatch creates a batch through the real HTTP endpoint and returns its ID.
func receiveBatch(t *testing.T, router http.Handler, user *models.User, product *models.Product, quantity string) string {
	t.Helper()
	body := fmt.Sprintf(`{"product_id": %q, "batch_number": "LOT-%s", "quantity": %q, "expiration_date": "2028-01-01T00:00:00Z"}`,
		product.ID.String(), uuid.NewString()[:8], quantity)
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
		t.Fatalf("decode receive response: %v", err)
	}
	return created.ID
}

// adjust posts an adjustment as the given role and returns the response.
func adjust(t *testing.T, router http.Handler, user *models.User, role, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/adjust", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestAdjustRoutesRequireAuth proves the adjust route needs a token.
func TestAdjustRoutesRequireAuth(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/adjust", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST without token = %d, want 401", rec.Code)
	}
}

// TestAdjustRoutesRBAC proves the ADMIN/OPERATOR-only guard on POST.
func TestAdjustRoutesRBAC(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)
	batchID := receiveBatch(t, router, &user, &product, "10.0000")

	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "WASTE"}`, batchID)
	if rec := adjust(t, router, &user, string(models.UserRoleTypeViewer), body); rec.Code != http.StatusForbidden {
		t.Errorf("VIEWER adjust = %d, want 403", rec.Code)
	}
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
		t.Errorf("OPERATOR adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
}

// TestAdjustWasteHappyPath records a WASTE write-off and verifies the batch
// quantity and the immutable audit row (type, sign, actor).
func TestAdjustWasteHappyPath(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)
	batchID := receiveBatch(t, router, &user, &product, "10.0000")

	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-5.0000", "transaction_type": "WASTE", "reference_note": "spoilage"}`, batchID)
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
		t.Fatalf("adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var current, txType, change, performedBy, note string
	err := pool.QueryRow(ctx, `
		SELECT b.quantity_current::text, t.transaction_type::text, t.quantity_change::text, t.performed_by::text, coalesce(t.reference_note, '')
		FROM inventory_batches b
		JOIN stock_transactions t ON t.batch_id = b.id
		WHERE b.id = $1
		ORDER BY t.created_at DESC
		LIMIT 1`, batchID).Scan(&current, &txType, &change, &performedBy, &note)
	if err != nil {
		t.Fatalf("query batch: %v", err)
	}
	if current != "5.0000" {
		t.Errorf("quantity_current = %q, want 5.0000", current)
	}
	if txType != "WASTE" {
		t.Errorf("transaction_type = %q, want WASTE", txType)
	}
	if change != "-5.0000" {
		t.Errorf("quantity_change = %q, want -5.0000", change)
	}
	if performedBy != user.ID.String() {
		t.Errorf("performed_by = %q, want %q", performedBy, user.ID.String())
	}
	if note != "spoilage" {
		t.Errorf("reference_note = %q, want spoilage", note)
	}
}

// TestAdjustmentPositiveCorrection verifies ADJUSTMENT may increase stock.
func TestAdjustmentPositiveCorrection(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)
	batchID := receiveBatch(t, router, &user, &product, "10.0000")

	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "3.0000", "transaction_type": "ADJUSTMENT"}`, batchID)
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
		t.Fatalf("adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var current string
	if err := pool.QueryRow(ctx, "SELECT quantity_current::text FROM inventory_batches WHERE id = $1", batchID).Scan(&current); err != nil {
		t.Fatalf("query batch: %v", err)
	}
	if current != "13.0000" {
		t.Errorf("quantity_current = %q, want 13.0000", current)
	}
}

// TestAdjustRejectsInvalidPayloads proves sign and type rules: WASTE must be
// negative, quantity must be non-zero, and the type must be WASTE or ADJUSTMENT.
func TestAdjustRejectsInvalidPayloads(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)
	batchID := receiveBatch(t, router, &user, &product, "10.0000")

	tests := []struct {
		name string
		body string
	}{
		{"waste with positive quantity", fmt.Sprintf(`{"batch_id": %q, "quantity_change": "2.0000", "transaction_type": "WASTE"}`, batchID)},
		{"zero quantity", fmt.Sprintf(`{"batch_id": %q, "quantity_change": "0.0000", "transaction_type": "ADJUSTMENT"}`, batchID)},
		{"invalid transaction type", fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "FROG"}`, batchID)},
		{"non numeric quantity", fmt.Sprintf(`{"batch_id": %q, "quantity_change": "abc", "transaction_type": "WASTE"}`, batchID)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), tt.body); rec.Code != http.StatusBadRequest {
				t.Errorf("adjust = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}

			var current string
			if err := pool.QueryRow(ctx, "SELECT quantity_current::text FROM inventory_batches WHERE id = $1", batchID).Scan(&current); err != nil {
				t.Fatalf("query batch: %v", err)
			}
			if current != "10.0000" {
				t.Errorf("quantity_current = %q after rejected adjust, want 10.0000", current)
			}

			var txns int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM stock_transactions").Scan(&txns); err != nil {
				t.Fatalf("count transactions: %v", err)
			}
			if txns != 1 {
				t.Errorf("transaction count = %d after rejected adjust, want 1 (INCOMING only)", txns)
			}
		})
	}
}

// TestAdjustBatchNotFound returns 404 for an unknown batch.
func TestAdjustBatchNotFound(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	router := newTestRouter(t, pool)

	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "WASTE"}`, uuid.New().String())
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusNotFound {
		t.Errorf("adjust unknown batch = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

// TestAdjustInsufficientStock rejects write-offs larger than the batch holds.
func TestAdjustInsufficientStock(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)
	batchID := receiveBatch(t, router, &user, &product, "10.0000")

	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-15.0000", "transaction_type": "WASTE"}`, batchID)
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusConflict {
		t.Errorf("adjust over stock = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}

	var current string
	if err := pool.QueryRow(ctx, "SELECT quantity_current::text FROM inventory_batches WHERE id = $1", batchID).Scan(&current); err != nil {
		t.Fatalf("query batch: %v", err)
	}
	if current != "10.0000" {
		t.Errorf("quantity_current = %q after rejected adjust, want 10.0000", current)
	}
}
