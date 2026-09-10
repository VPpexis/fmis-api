// Package integration contains end-to-end HTTP tests that exercise the full
// API stack (middleware, routers, services, repositories) against a real
// PostgreSQL database, per issue #63. Each test gets a throwaway schema with
// every migration applied, so the suite runs anywhere PostgreSQL is reachable
// via DATABASE_URL (default: the docker compose dev database).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fmis-api/internal/routers"
	"fmis-api/internal/services"
	"fmis-api/internal/testutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testSecret   = "integration-test-secret"
	seedPassword = "Password123!"

	seedAdminID    = "00000000-0000-0000-0000-000000000001"
	seedOperatorID = "00000000-0000-0000-0000-000000000002"
	seedViewerID   = "00000000-0000-0000-0000-000000000003"

	seedAdminUser    = "seed-admin"
	seedOperatorUser = "seed-operator"
	seedViewerUser   = "seed-viewer"

	seedRawMaterialID  = "10000000-0000-0000-0000-000000000001"
	seedWhiteLabelID   = "10000000-0000-0000-0000-000000000003"
	seedFinishedGoodID = "10000000-0000-0000-0000-000000000004"

	seedRMOldBatchID  = "20000000-0000-0000-0000-000000000001"
	seedRMMidBatchID  = "20000000-0000-0000-0000-000000000002"
	seedRMNewBatchID  = "20000000-0000-0000-0000-000000000003"
	seedRMNullBatchID = "20000000-0000-0000-0000-000000000004"

	seedWLOldBatchID = "21000000-0000-0000-0000-000000000001"
	seedWLNewBatchID = "21000000-0000-0000-0000-000000000002"
)

// harness runs the real router over a real HTTP server against a fresh schema.
type harness struct {
	t      *testing.T
	server *httptest.Server
	pool   *pgxpool.Pool
	client *http.Client
}

// newHarness wires the full application (real services and repositories) to a
// per-test PostgreSQL schema and serves it over httptest.Server.
func newHarness(t *testing.T) *harness {
	t.Helper()

	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)

	handler := routers.New(
		services.NewAuthService(pool, testSecret, 15*time.Minute, 7*24*time.Hour),
		services.NewBatchService(pool),
		services.NewProductService(pool),
		services.NewInventoryService(pool),
		services.NewProductionOrderService(pool),
		pool,
		testSecret,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &harness{t: t, server: server, pool: pool, client: server.Client()}
}

// newSeededHarness is newHarness with tests/testdata/seed.sql loaded.
func newSeededHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	testutil.LoadSeed(t, h.pool, "tests/testdata/seed.sql")
	return h
}

// response is a buffered HTTP response so tests can assert after the body closes.
type response struct {
	status int
	body   []byte
}

// do sends a JSON request through the real server and buffers the response.
func (h *harness) do(method, path, token string, body any) response {
	h.t.Helper()

	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal request body: %v", err)
		}
		payload = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, payload)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read response body: %v", err)
	}
	return response{status: resp.StatusCode, body: raw}
}

// doConcurrent is do without test failures, safe to call from goroutines.
func (h *harness) doConcurrent(method, path, token string, body any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// expectStatus fails the test unless the response carries want.
func (h *harness) expectStatus(resp response, want int) response {
	h.t.Helper()
	if resp.status != want {
		h.t.Fatalf("status = %d, want %d; body: %s", resp.status, want, resp.body)
	}
	return resp
}

// decode unmarshals a response body into target.
func (h *harness) decode(resp response, target any) {
	h.t.Helper()
	if err := json.Unmarshal(resp.body, target); err != nil {
		h.t.Fatalf("decode response %q: %v", resp.body, err)
	}
}

// login authenticates through POST /api/v1/auth/login and returns the access token.
func (h *harness) login(identifier, password string) string {
	h.t.Helper()

	resp := h.expectStatus(h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"identifier": identifier,
		"password":   password,
	}), http.StatusOK)

	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	h.decode(resp, &tokens)
	if tokens.AccessToken == "" {
		h.t.Fatal("login returned an empty access token")
	}
	return tokens.AccessToken
}

// createProduct creates a catalog product as token and returns its ID.
func (h *harness) createProduct(token, sku, name, unit, productType string) string {
	h.t.Helper()

	resp := h.expectStatus(h.do(http.MethodPost, "/api/v1/products/", token, map[string]any{
		"sku":             sku,
		"name":            name,
		"unit_of_measure": unit,
		"product_type":    productType,
		"is_purchasable":  true,
		"is_sellable":     productType == "FINISHED_GOOD",
	}), http.StatusCreated)

	var product struct {
		ID string `json:"id"`
	}
	h.decode(resp, &product)
	return product.ID
}

// receiveBatch receives stock as token and returns the new batch ID.
func (h *harness) receiveBatch(token, productID, batchNumber, quantity string, expiration *time.Time) string {
	h.t.Helper()

	body := map[string]any{
		"product_id":   productID,
		"batch_number": batchNumber,
		"quantity":     quantity,
	}
	if expiration != nil {
		body["expiration_date"] = expiration.UTC().Format(time.RFC3339)
	}

	resp := h.expectStatus(h.do(http.MethodPost, "/api/v1/batches/", token, body), http.StatusCreated)

	var batch struct {
		ID string `json:"id"`
	}
	h.decode(resp, &batch)
	return batch.ID
}

// consume posts a FEFO consumption as token.
func (h *harness) consume(token, productID, quantity string) response {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/v1/inventory/consume", token, map[string]any{
		"product_id": productID,
		"quantity":   quantity,
	})
}

// productionOrderResponse is the subset of the production payload tests assert on.
type productionOrderResponse struct {
	Order struct {
		ID            string `json:"id"`
		OutputBatchID string `json:"output_batch_id"`
		Status        string `json:"status"`
	} `json:"order"`
}

// productionLine describes one input batch and the quantity consumed from it.
type productionLine struct {
	batchID  string
	quantity string
}

// createProductionOrder creates a PLANNED order as token.
func (h *harness) createProductionOrder(token, outputProductID, outputBatchNumber, outputQuantity string, lines ...productionLine) productionOrderResponse {
	h.t.Helper()

	lineItems := make([]map[string]string, 0, len(lines))
	for _, line := range lines {
		lineItems = append(lineItems, map[string]string{
			"input_batch_id":    line.batchID,
			"quantity_consumed": line.quantity,
		})
	}

	resp := h.expectStatus(h.do(http.MethodPost, "/api/v1/production/", token, map[string]any{
		"line_items":          lineItems,
		"output_product_id":   outputProductID,
		"output_batch_number": outputBatchNumber,
		"output_quantity":     outputQuantity,
	}), http.StatusCreated)

	var created productionOrderResponse
	h.decode(resp, &created)
	return created
}

// batchQuantity reads a batch's current quantity straight from PostgreSQL.
func (h *harness) batchQuantity(batchID string) string {
	h.t.Helper()
	var quantity string
	if err := h.pool.QueryRow(context.Background(),
		"SELECT quantity_current::text FROM inventory_batches WHERE id = $1", batchID).Scan(&quantity); err != nil {
		h.t.Fatalf("query batch %s quantity: %v", batchID, err)
	}
	return quantity
}

// batchStatus reads a batch's status straight from PostgreSQL.
func (h *harness) batchStatus(batchID string) string {
	h.t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(),
		"SELECT status::text FROM inventory_batches WHERE id = $1", batchID).Scan(&status); err != nil {
		h.t.Fatalf("query batch %s status: %v", batchID, err)
	}
	return status
}

// batchExpiration reads a batch's expiration date straight from PostgreSQL.
func (h *harness) batchExpiration(batchID string) time.Time {
	h.t.Helper()
	var expiration time.Time
	if err := h.pool.QueryRow(context.Background(),
		"SELECT expiration_date FROM inventory_batches WHERE id = $1", batchID).Scan(&expiration); err != nil {
		h.t.Fatalf("query batch %s expiration: %v", batchID, err)
	}
	return expiration
}

// countTransactions counts stock transactions of a given type.
func (h *harness) countTransactions(transactionType string) int {
	h.t.Helper()
	var count int
	if err := h.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM stock_transactions WHERE transaction_type = $1", transactionType).Scan(&count); err != nil {
		h.t.Fatalf("count %s transactions: %v", transactionType, err)
	}
	return count
}
