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
	"net/url"
	"strings"
	"testing"
	"time"

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

// listTransactions GETs /api/v1/inventory/transactions/ with the given query
// suffix (e.g. "?limit=2") as the given role and returns the response.
func listTransactions(t *testing.T, router http.Handler, user *models.User, role, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/transactions/"+query, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// transactionIDs decodes a transaction list response into its IDs.
func transactionIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode transactions response: %v", err)
	}
	ids := make([]string, 0, len(list))
	for _, tr := range list {
		ids = append(ids, tr.ID)
	}
	return ids
}

// TestListTransactionsRequireAuth proves the transactions route needs a token.
func TestListTransactionsRequireAuth(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/transactions/", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without token = %d, want 401", rec.Code)
	}
}

// TestListTransactionsAllRolesAndFilters proves every role may read the audit
// trail and that the batch and type filters narrow the result set.
func TestListTransactionsAllRolesAndFilters(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)

	batchA := receiveBatch(t, router, &user, &product, "10.0000")
	batchB := receiveBatch(t, router, &user, &product, "10.0000")

	// 5 transactions total: INCOMING x2 (one per receive), WASTE x2, ADJUSTMENT x1.
	for _, body := range []string{
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "WASTE"}`, batchA),
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "1.0000", "transaction_type": "ADJUSTMENT"}`, batchA),
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-3.0000", "transaction_type": "WASTE"}`, batchB),
	} {
		if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
			t.Fatalf("setup adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
		}
	}

	tests := []struct {
		name  string
		role  string
		query string
		want  int
	}{
		{"viewer sees all", string(models.UserRoleTypeViewer), "", 5},
		{"operator sees all", string(models.UserRoleTypeOperator), "", 5},
		{"filter by batch A", string(models.UserRoleTypeViewer), "?batch_id=" + batchA, 3},
		{"filter by batch B", string(models.UserRoleTypeViewer), "?batch_id=" + batchB, 2},
		{"filter by type WASTE", string(models.UserRoleTypeViewer), "?transaction_type=WASTE", 2},
		{"filter by type INCOMING", string(models.UserRoleTypeViewer), "?transaction_type=INCOMING", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := listTransactions(t, router, &user, tt.role, tt.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("list = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			if got := len(transactionIDs(t, rec)); got != tt.want {
				t.Errorf("len(list) = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestListTransactionsRejectsInvalidFilters proves malformed filter values
// are rejected with 400 instead of silently ignored.
func TestListTransactionsRejectsInvalidFilters(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	router := newTestRouter(t, pool)

	tests := []struct {
		name  string
		query string
	}{
		{"invalid batch uuid", "?batch_id=not-a-uuid"},
		{"invalid transaction type", "?transaction_type=FROG"},
		{"invalid date_from", "?date_from=garbage"},
		{"invalid date_to", "?date_to=garbage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := listTransactions(t, router, &user, string(models.UserRoleTypeViewer), tt.query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("list %s = %d, want 400; body: %s", tt.query, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestListTransactionsDateRange proves the date range filter brackets
// transactions by created_at with inclusive bounds.
func TestListTransactionsDateRange(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)

	batchID := receiveBatch(t, router, &user, &product, "10.0000")
	body := fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "WASTE"}`, batchID)
	if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
		t.Fatalf("setup adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var wasteID, incomingID string
	var wasteTS, incomingTS time.Time
	if err := pool.QueryRow(ctx, "SELECT id::text, created_at FROM stock_transactions WHERE transaction_type = 'WASTE'").Scan(&wasteID, &wasteTS); err != nil {
		t.Fatalf("query waste transaction: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT id::text, created_at FROM stock_transactions WHERE transaction_type = 'INCOMING'").Scan(&incomingID, &incomingTS); err != nil {
		t.Fatalf("query incoming transaction: %v", err)
	}
	escaped := func(ts time.Time) string { return url.QueryEscape(ts.Format(time.RFC3339Nano)) }

	tests := []struct {
		name   string
		query  string
		want   int
		wantID string
	}{
		{"exact instant", "?date_from=" + escaped(wasteTS) + "&date_to=" + escaped(wasteTS), 1, wasteID},
		{"up to the incoming", "?date_to=" + escaped(incomingTS), 1, incomingID},
		{"after the waste", "?date_from=" + escaped(wasteTS.Add(time.Second)), 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := listTransactions(t, router, &user, string(models.UserRoleTypeViewer), tt.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("list = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			ids := transactionIDs(t, rec)
			if len(ids) != tt.want {
				t.Errorf("len(list) = %d, want %d", len(ids), tt.want)
			}
			if tt.want == 1 && len(ids) == 1 && ids[0] != tt.wantID {
				t.Errorf("hit = %s, want %s", ids[0], tt.wantID)
			}
		})
	}
}

// TestListTransactionsPagination proves limit/offset page through the audit
// trail in newest-first order without overlap.
func TestListTransactionsPagination(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	router := newTestRouter(t, pool)

	batchA := receiveBatch(t, router, &user, &product, "10.0000")
	batchB := receiveBatch(t, router, &user, &product, "10.0000")
	for _, body := range []string{
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-2.0000", "transaction_type": "WASTE"}`, batchA),
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "1.0000", "transaction_type": "ADJUSTMENT"}`, batchA),
		fmt.Sprintf(`{"batch_id": %q, "quantity_change": "-3.0000", "transaction_type": "WASTE"}`, batchB),
	} {
		if rec := adjust(t, router, &user, string(models.UserRoleTypeOperator), body); rec.Code != http.StatusCreated {
			t.Fatalf("setup adjust = %d, want 201; body: %s", rec.Code, rec.Body.String())
		}
	}

	rows, err := pool.Query(ctx, "SELECT id::text FROM stock_transactions ORDER BY created_at DESC")
	if err != nil {
		t.Fatalf("query expected order: %v", err)
	}
	expected := make([]string, 0, 5)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan expected order: %v", err)
		}
		expected = append(expected, id)
	}
	rows.Close()
	if len(expected) != 5 {
		t.Fatalf("setup produced %d transactions, want 5", len(expected))
	}

	var collected []string
	for offset := 0; offset < len(expected); offset += 2 {
		rec := listTransactions(t, router, &user, string(models.UserRoleTypeViewer), fmt.Sprintf("?limit=2&offset=%d", offset))
		if rec.Code != http.StatusOK {
			t.Fatalf("page offset=%d = %d, want 200; body: %s", offset, rec.Code, rec.Body.String())
		}
		page := transactionIDs(t, rec)
		if len(page) > 2 {
			t.Fatalf("page offset=%d returned %d rows, want <= 2", offset, len(page))
		}
		collected = append(collected, page...)
	}

	if len(collected) != len(expected) {
		t.Fatalf("pages returned %d rows total, want %d", len(collected), len(expected))
	}
	for i := range expected {
		if collected[i] != expected[i] {
			t.Errorf("page order mismatch at %d: got %s, want %s", i, collected[i], expected[i])
		}
	}
}
