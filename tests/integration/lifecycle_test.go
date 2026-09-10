package integration

import (
	"net/http"
	"testing"
	"time"
)

// TestFullHTTPLifecycle drives one end-to-end flow through the real HTTP
// surface: register a user, log in, build the catalog, receive stock, consume
// it FEFO, then run a production order from create to completion and verify
// every side effect.
func TestFullHTTPLifecycle(t *testing.T) {
	h := newSeededHarness(t)

	// 1. Self-registration creates a VIEWER that can authenticate.
	registered := h.expectStatus(h.do(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"username": "lifecycle-user",
		"email":    "lifecycle-user@example.com",
		"password": seedPassword,
	}), http.StatusCreated)

	var registeredTokens struct {
		AccessToken string `json:"access_token"`
	}
	h.decode(registered, &registeredTokens)

	var me struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	h.decode(h.expectStatus(h.do(http.MethodGet, "/api/v1/auth/me", registeredTokens.AccessToken, nil), http.StatusOK), &me)
	if me.Role != "VIEWER" {
		t.Fatalf("registered role = %q, want VIEWER", me.Role)
	}

	operator := h.login(seedOperatorUser, seedPassword)

	// 2. Catalog: the products this flow needs.
	rawMaterialID := h.createProduct(operator, "LC-RM-001", "Lifecycle Flour", "kg", "RAW_MATERIAL")
	whiteLabelID := h.createProduct(operator, "LC-WL-001", "Lifecycle Unbranded Loaf", "pack", "WHITE_LABEL")
	finishedGoodID := h.createProduct(operator, "LC-FG-001", "Lifecycle Branded Loaf", "pack", "FINISHED_GOOD")

	// 3. Receive stock: one raw-material batch and two white-label inputs.
	rawMaterialExpiration := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	rawMaterialBatchID := h.receiveBatch(operator, rawMaterialID, "LC-RM-LOT-1", "10.0000", &rawMaterialExpiration)

	oldestInputExpiration := time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC)
	newestInputExpiration := time.Date(2027, time.February, 15, 0, 0, 0, 0, time.UTC)
	oldInputBatchID := h.receiveBatch(operator, whiteLabelID, "LC-WL-OLD", "5.0000", &oldestInputExpiration)
	newInputBatchID := h.receiveBatch(operator, whiteLabelID, "LC-WL-NEW", "5.0000", &newestInputExpiration)

	// 4. Consume raw material through the FEFO endpoint.
	h.expectStatus(h.consume(operator, rawMaterialID, "3.0000"), http.StatusCreated)
	if got := h.batchQuantity(rawMaterialBatchID); got != "7.0000" {
		t.Fatalf("raw material quantity = %s, want 7.0000", got)
	}

	// 5. Production: create a PLANNED order, start it, complete it.
	created := h.createProductionOrder(operator, finishedGoodID, "LC-FG-LOT-1", "10.0000",
		productionLine{oldInputBatchID, "5.0000"},
		productionLine{newInputBatchID, "5.0000"},
	)
	if created.Order.Status != "PLANNED" {
		t.Fatalf("created order status = %q, want PLANNED", created.Order.Status)
	}

	h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/start", operator, nil), http.StatusOK)

	completed := h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/complete", operator, nil), http.StatusOK)
	var completedOrder productionOrderResponse
	h.decode(completed, &completedOrder)
	if completedOrder.Order.Status != "COMPLETED" {
		t.Fatalf("completed order status = %q, want COMPLETED", completedOrder.Order.Status)
	}

	// 6. The output batch is live, holds the full quantity, and expires when
	// the oldest consumed input does.
	if got := h.batchStatus(created.Order.OutputBatchID); got != "ACTIVE" {
		t.Errorf("output batch status = %q, want ACTIVE", got)
	}
	if got := h.batchQuantity(created.Order.OutputBatchID); got != "10.0000" {
		t.Errorf("output batch quantity = %s, want 10.0000", got)
	}
	if got := h.batchExpiration(created.Order.OutputBatchID); !got.Equal(oldestInputExpiration) {
		t.Errorf("output batch expiration = %s, want %s", got, oldestInputExpiration)
	}

	// 7. Inputs are drained and the audit trail is complete.
	if got := h.batchQuantity(oldInputBatchID); got != "0.0000" {
		t.Errorf("old input batch quantity = %s, want 0.0000", got)
	}
	if got := h.batchQuantity(newInputBatchID); got != "0.0000" {
		t.Errorf("new input batch quantity = %s, want 0.0000", got)
	}
	if got := h.countTransactions("INCOMING"); got != 3 {
		t.Errorf("INCOMING transactions = %d, want 3", got)
	}
	if got := h.countTransactions("OUTGOING"); got != 1 {
		t.Errorf("OUTGOING transactions = %d, want 1", got)
	}
	if got := h.countTransactions("USED_IN_PRODUCTION"); got != 2 {
		t.Errorf("USED_IN_PRODUCTION transactions = %d, want 2", got)
	}
}
