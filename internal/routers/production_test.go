// Package routers for production orders.
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

	req = httptest.NewRequest(http.MethodPost, "/api/v1/production/"+uuid.New().String()+"/complete", http.NoBody)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST complete without token = %d, want 401", rec.Code)
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

// TestCompleteProductionOrderHTTP covers the complete lifecycle: RBAC guard,
// happy path, duplicate 409, and unknown-order 404.
func TestCompleteProductionOrderHTTP(t *testing.T) {
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
			OutputBatchID string `json:"output_batch_id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	startURL := "/api/v1/production/" + created.Order.ID + "/start"
	req := httptest.NewRequest(http.MethodPost, startURL, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	completeURL := "/api/v1/production/" + created.Order.ID + "/complete"
	completeAs := func(role string) *httptest.ResponseRecorder {
		cReq := httptest.NewRequest(http.MethodPost, completeURL, http.NoBody)
		cReq.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
		r := httptest.NewRecorder()
		router.ServeHTTP(r, cReq)
		return r
	}

	if r := completeAs(string(models.UserRoleTypeViewer)); r.Code != http.StatusForbidden {
		t.Errorf("VIEWER complete = %d, want 403", r.Code)
	}

	rec = completeAs(string(models.UserRoleTypeOperator))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var completed struct {
		Order struct {
			Status      string `json:"status"`
			CompletedAt any    `json:"completed_at"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &completed); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if completed.Order.Status != "COMPLETED" {
		t.Errorf("status = %s, want COMPLETED", completed.Order.Status)
	}
	if completed.Order.CompletedAt == nil {
		t.Error("completed_at is null, want a timestamp")
	}

	if r := completeAs(string(models.UserRoleTypeOperator)); r.Code != http.StatusConflict {
		t.Errorf("second complete = %d, want 409", r.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/production/"+uuid.New().String()+"/complete", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown order complete = %d, want 404", rec.Code)
	}

	var qty float64
	if err := pool.QueryRow(ctx, `SELECT quantity_current FROM inventory_batches WHERE id = $1`, inputBatchID).Scan(&qty); err != nil {
		t.Fatalf("query input batch: %v", err)
	}
	if qty != 8 {
		t.Errorf("input batch quantity = %v, want 8 (deducted once)", qty)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_batches WHERE id = $1`, created.Order.OutputBatchID).Scan(&status); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "ACTIVE" {
		t.Errorf("output batch status = %s, want ACTIVE", status)
	}
}

// TestCancelProductionOrderRBAC proves only ADMIN can cancel an order.
func TestCancelProductionOrderRBAC(t *testing.T) {
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
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	cancelURL := "/api/v1/production/" + created.Order.ID + "/cancel"
	cancelAs := func(role string) *httptest.ResponseRecorder {
		cReq := httptest.NewRequest(http.MethodPost, cancelURL, http.NoBody)
		cReq.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
		r := httptest.NewRecorder()
		router.ServeHTTP(r, cReq)
		return r
	}

	if r := cancelAs(string(models.UserRoleTypeViewer)); r.Code != http.StatusForbidden {
		t.Errorf("VIEWER cancel = %d, want 403", r.Code)
	}
	if r := cancelAs(string(models.UserRoleTypeOperator)); r.Code != http.StatusForbidden {
		t.Errorf("OPERATOR cancel = %d, want 403", r.Code)
	}
}

// TestCancelProductionOrderHTTP covers the cancel lifecycle: happy path on a
// PLANNED order with no inventory impact, duplicate 409, and unknown-order 404.
func TestCancelProductionOrderHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	admin := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeAdmin)
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
			OutputBatchID string `json:"output_batch_id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	cancelURL := "/api/v1/production/" + created.Order.ID + "/cancel"
	cancelAs := func(role string) *httptest.ResponseRecorder {
		cReq := httptest.NewRequest(http.MethodPost, cancelURL, http.NoBody)
		cReq.Header.Set("Authorization", "Bearer "+signTestToken(t, admin.ID.String(), role))
		r := httptest.NewRecorder()
		router.ServeHTTP(r, cReq)
		return r
	}

	rec = cancelAs(string(models.UserRoleTypeAdmin))
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if cancelled.Status != "CANCELLED" {
		t.Errorf("status = %s, want CANCELLED", cancelled.Status)
	}

	var qty float64
	if err := pool.QueryRow(ctx, `SELECT quantity_current FROM inventory_batches WHERE id = $1`, inputBatchID).Scan(&qty); err != nil {
		t.Fatalf("query input batch: %v", err)
	}
	if qty != 10 {
		t.Errorf("input batch quantity = %v, want 10 (no inventory impact)", qty)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_batches WHERE id = $1`, created.Order.OutputBatchID).Scan(&status); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "RESERVED" {
		t.Errorf("output batch status = %s, want RESERVED (no inventory impact)", status)
	}

	if r := cancelAs(string(models.UserRoleTypeAdmin)); r.Code != http.StatusConflict {
		t.Errorf("second cancel = %d, want 409", r.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/"+uuid.New().String()+"/cancel", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, admin.ID.String(), string(models.UserRoleTypeAdmin)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown order cancel = %d, want 404", rec.Code)
	}
}

// TestCancelCompletedProductionOrder proves COMPLETED orders cannot be
// cancelled (acceptance criterion).
func TestCancelCompletedProductionOrder(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	admin := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeAdmin)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/complete", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), string(models.UserRoleTypeOperator)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/cancel", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, admin.ID.String(), string(models.UserRoleTypeAdmin)))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("cancel completed = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
}

// listProductionOrders posts a GET /api/v1/production request as the given
// role with the given query string and returns the response recorder.
func listProductionOrders(t *testing.T, router http.Handler, user *models.User, role, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/production/"+query, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestListProductionOrdersHTTP covers GET /api/v1/production: any
// authenticated role can list ([ALL]), and the status filter narrows results.
func TestListProductionOrdersHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	viewer := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeViewer)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	for i := 0; i < 2; i++ {
		rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d = %d, want 201; body: %s", i, rec.Code, rec.Body.String())
		}
	}

	// A VIEWER may list production orders: the route is [ALL].
	rec := listProductionOrders(t, router, &viewer, string(models.UserRoleTypeViewer), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var orders []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("len(orders) = %d, want 2", len(orders))
	}
	for _, o := range orders {
		if o.Status != "PLANNED" {
			t.Errorf("order status = %s, want PLANNED", o.Status)
		}
	}

	// The status filter keeps only matching orders.
	rec = listProductionOrders(t, router, &viewer, string(models.UserRoleTypeViewer), "?status=PLANNED")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	orders = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
		t.Fatalf("decode filtered list response: %v", err)
	}
	if len(orders) != 2 {
		t.Errorf("len(orders) = %d, want 2", len(orders))
	}

	// A filter with no matches returns an empty array, not null.
	rec = listProductionOrders(t, router, &viewer, string(models.UserRoleTypeViewer), "?status=COMPLETED")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	orders = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
		t.Fatalf("decode empty list response: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("len(orders) = %d, want 0", len(orders))
	}
}

// TestListProductionOrdersInvalidStatus proves an unknown status filter gets a
// 400 instead of a 500 from Postgres.
func TestListProductionOrdersInvalidStatus(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	viewer := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeViewer)

	rec := listProductionOrders(t, router, &viewer, string(models.UserRoleTypeViewer), "?status=BOGUS")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// TestListProductionOrdersPagination proves the limit/offset defaults and
// overrides behave like the products list.
func TestListProductionOrdersPagination(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	viewer := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeViewer)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	for i := 0; i < 2; i++ {
		rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d = %d, want 201; body: %s", i, rec.Code, rec.Body.String())
		}
	}

	count := func(query string) int {
		t.Helper()
		rec := listProductionOrders(t, router, &viewer, string(models.UserRoleTypeViewer), query)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s = %d, want 200; body: %s", query, rec.Code, rec.Body.String())
		}
		var orders []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &orders); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		return len(orders)
	}

	if got := count(""); got != 2 {
		t.Errorf("default limit = %d, want 2", got)
	}
	if got := count("?limit=1"); got != 1 {
		t.Errorf("limit=1 = %d, want 1", got)
	}
	if got := count("?offset=1"); got != 1 {
		t.Errorf("offset=1 = %d, want 1", got)
	}
	if got := count("?limit=999"); got != 2 {
		t.Errorf("limit=999 (capped at 100) = %d, want 2", got)
	}
}

// getProductionOrderByID issues a GET /api/v1/production/{id} request as the
// given role and returns the response recorder.
func getProductionOrderByID(t *testing.T, router http.Handler, user *models.User, role, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/production/"+orderID, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, user.ID.String(), role))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestGetProductionOrderByIDHTTP covers GET /api/v1/production/{id}: any
// authenticated role can read it ([ALL]), and the response includes the
// order's line items.
func TestGetProductionOrderByIDHTTP(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	viewer := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeViewer)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)

	inputBatchID := seedInputBatch(t, router, &user, inProduct.ID.String())
	rec := createProductionOrder(t, router, &user, inputBatchID, outProduct.ID.String())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec = getProductionOrderByID(t, router, &viewer, string(models.UserRoleTypeViewer), created.Order.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Order struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"order"`
		LineItems []struct {
			InputBatchID string `json:"input_batch_id"`
		} `json:"line_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detail.Order.ID != created.Order.ID {
		t.Errorf("order id = %s, want %s", detail.Order.ID, created.Order.ID)
	}
	if detail.Order.Status != "PLANNED" {
		t.Errorf("order status = %s, want PLANNED", detail.Order.Status)
	}
	if len(detail.LineItems) != 1 {
		t.Fatalf("len(line_items) = %d, want 1", len(detail.LineItems))
	}
	if detail.LineItems[0].InputBatchID != inputBatchID {
		t.Errorf("line item input_batch_id = %s, want %s", detail.LineItems[0].InputBatchID, inputBatchID)
	}
}

// TestGetProductionOrderByIDErrors covers the malformed-id 400 and
// unknown-order 404 paths.
func TestGetProductionOrderByIDErrors(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	router := newTestRouter(t, pool)
	ctx := context.Background()
	viewer := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeViewer)

	rec := getProductionOrderByID(t, router, &viewer, string(models.UserRoleTypeViewer), "not-a-uuid")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed id = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	rec = getProductionOrderByID(t, router, &viewer, string(models.UserRoleTypeViewer), uuid.New().String())
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}
