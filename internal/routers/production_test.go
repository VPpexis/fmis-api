// Package routers for production orders.
package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"fmis-api/internal/models"
	"fmis-api/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedInputBatch receives an ACTIVE WHITE_LABEL batch through the HTTP API.
func seedInputBatch(t *testing.T, router http.Handler, user *models.User, productID string) string {
	t.Helper()
	body := fmt.Sprintf(`{"product_id": %q, "batch_number": "LOT-IN", "quantity": "10.0000", "expiration_date": "2027-01-01T00:00:00Z"}`, productID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/batches/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed input batch = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode seed response: %v", err)
	}
	return created.ID
}

// createProductionOrder posts a create request and returns the response recorder.
func createProductionOrder(t *testing.T, router http.Handler, user *models.User, inputBatchID, outputProductID string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{
		"line_items": [{"input_batch_id": %q, "quantity_consumed": "2.0000"}],
		"output_product_id": %q,
		"output_batch_number": "B-OUTPUT",
		"output_quantity": "2.0000",
		"output_expiration_date": "2027-01-01T00:00:00Z"
	}`, inputBatchID, outputProductID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestProductionRoutesRequireAuth proves both production routes need a token.
func TestProductionRoutesRequireAuth(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST create without token = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/production/"+uuid.New().String()+"/start", http.NoBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST start without token = %d, want 401", rec.Code)
	}
}

// TestProductionRoutesRBAC proves the ADMIN/OPERATOR-only guard.
func TestProductionRoutesRBAC(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	body := fmt.Sprintf(`{
		"line_items": [{"input_batch_id": %q, "quantity_consumed": "1.0000"}],
		"output_product_id": %q,
		"output_batch_number": "B-RBAC",
		"output_quantity": "1.0000"
	}`, uuid.New().String(), outProduct.ID.String())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeViewer)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("VIEWER create = %d, want 403", rec.Code)
	}
}

// TestCreateProductionOrderHTTP is the full HTTP lifecycle for create: the
// order is PLANNED and the output batch exists as RESERVED.
func TestCreateProductionOrderHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		Order struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			OutputBatchID string `json:"output_batch_id"`
		} `json:"order"`
		LineItems []struct {
			ID           string `json:"id"`
			InputBatchID string `json:"input_batch_id"`
		} `json:"line_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Order.Status != "PLANNED" {
		t.Errorf("order status = %s, want PLANNED", created.Order.Status)
	}
	if len(created.LineItems) != 1 {
		t.Fatalf("len(line_items) = %d, want 1", len(created.LineItems))
	}
	if created.LineItems[0].InputBatchID != inputBatchID {
		t.Errorf("line item input_batch_id = %s, want %s", created.LineItems[0].InputBatchID, inputBatchID)
	}

	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM inventory_batches WHERE id = $1`,
		created.Order.OutputBatchID).Scan(&status); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "RESERVED" {
		t.Errorf("output batch status = %s, want RESERVED", status)
	}
}

// TestCreateProductionOrderRejectsInvalidBody proves bad payloads get a 400.
func TestCreateProductionOrderRejectsInvalidBody(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)

	body := `{"line_items": [], "output_product_id": "not-a-uuid", "output_batch_number": "B", "output_quantity": "1.0000"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid body = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// TestStartProductionOrderHTTP covers the start lifecycle: RBAC guard, happy
// path, duplicate conflict, and unknown-order 404.
func TestStartProductionOrderHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	var created struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	startURL := "/api/v1/production/" + created.Order.ID + "/start"
	startAs := func(role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, startURL, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
		r := httptest.NewRecorder()
		router.ServeHTTP(r, req)
		return r
	}

	if r := startAs(string(models.UserRoleTypeViewer)); r.Code != http.StatusForbidden {
		t.Errorf("VIEWER start = %d, want 403", r.Code)
	}

	rec = startAs(string(models.UserRoleTypeOperator))
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.Status != "IN_PROGRESS" {
		t.Errorf("status = %s, want IN_PROGRESS", started.Status)
	}

	if r := startAs(string(models.UserRoleTypeOperator)); r.Code != http.StatusConflict {
		t.Errorf("second start = %d, want 409", r.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/"+uuid.New().String()+"/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown order start = %d, want 404", rec.Code)
	}
}
