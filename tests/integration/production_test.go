package integration

import (
	"net/http"
	"testing"
	"time"
)

// TestProductionCompleteInheritsOldestInputExpiration runs a seeded production
// order to completion and verifies the output batch is activated with the
// quantity requested and the MIN expiration of its inputs.
func TestProductionCompleteInheritsOldestInputExpiration(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)

	created := h.createProductionOrder(operator, seedFinishedGoodID, "PROD-OK-1", "10.0000",
		productionLine{seedWLOldBatchID, "5.0000"},
		productionLine{seedWLNewBatchID, "5.0000"},
	)

	started := h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/start", operator, nil), http.StatusOK)
	var startedOrder struct {
		Status string `json:"status"`
	}
	h.decode(started, &startedOrder)
	if startedOrder.Status != "IN_PROGRESS" {
		t.Fatalf("started order status = %q, want IN_PROGRESS", startedOrder.Status)
	}

	completed := h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/complete", operator, nil), http.StatusOK)
	var completedOrder productionOrderResponse
	h.decode(completed, &completedOrder)
	if completedOrder.Order.Status != "COMPLETED" {
		t.Fatalf("completed order status = %q, want COMPLETED", completedOrder.Order.Status)
	}

	if got := h.batchStatus(created.Order.OutputBatchID); got != "ACTIVE" {
		t.Errorf("output batch status = %q, want ACTIVE", got)
	}
	if got := h.batchQuantity(created.Order.OutputBatchID); got != "10.0000" {
		t.Errorf("output batch quantity = %s, want 10.0000", got)
	}

	wantExpiration := time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC)
	if got := h.batchExpiration(created.Order.OutputBatchID); !got.Equal(wantExpiration) {
		t.Errorf("output batch expiration = %s, want %s (MIN of inputs)", got, wantExpiration)
	}

	if got := h.batchQuantity(seedWLOldBatchID); got != "0.0000" {
		t.Errorf("old input batch quantity = %s, want 0.0000", got)
	}
	if got := h.batchQuantity(seedWLNewBatchID); got != "0.0000" {
		t.Errorf("new input batch quantity = %s, want 0.0000", got)
	}
	if got := h.countTransactions("USED_IN_PRODUCTION"); got != 2 {
		t.Errorf("USED_IN_PRODUCTION transactions = %d, want 2", got)
	}
}

// TestProductionCompleteRollsBackOnInsufficientStock proves a failed completion
// is atomic: no input is deducted, the output batch stays RESERVED, the order
// stays IN_PROGRESS, and no production transactions are recorded.
func TestProductionCompleteRollsBackOnInsufficientStock(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)

	// The second line asks for 6 units of a batch that only holds 5.
	created := h.createProductionOrder(operator, seedFinishedGoodID, "PROD-FAIL-1", "11.0000",
		productionLine{seedWLOldBatchID, "5.0000"},
		productionLine{seedWLNewBatchID, "6.0000"},
	)

	h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/start", operator, nil), http.StatusOK)

	failed := h.expectStatus(h.do(http.MethodPost, "/api/v1/production/"+created.Order.ID+"/complete", operator, nil), http.StatusConflict)
	var envelope struct {
		Error string `json:"error"`
	}
	h.decode(failed, &envelope)
	if envelope.Error == "" {
		t.Error("409 response did not use the error envelope")
	}

	if got := h.batchQuantity(seedWLOldBatchID); got != "5.0000" {
		t.Errorf("old input batch quantity = %s, want 5.0000 (rollback)", got)
	}
	if got := h.batchQuantity(seedWLNewBatchID); got != "5.0000" {
		t.Errorf("new input batch quantity = %s, want 5.0000 (rollback)", got)
	}
	if got := h.batchStatus(created.Order.OutputBatchID); got != "RESERVED" {
		t.Errorf("output batch status = %q, want RESERVED (rollback)", got)
	}
	if got := h.countTransactions("USED_IN_PRODUCTION"); got != 0 {
		t.Errorf("USED_IN_PRODUCTION transactions = %d, want 0 (rollback)", got)
	}

	fetched := h.expectStatus(h.do(http.MethodGet, "/api/v1/production/"+created.Order.ID, operator, nil), http.StatusOK)
	var fetchedOrder productionOrderResponse
	h.decode(fetched, &fetchedOrder)
	if fetchedOrder.Order.Status != "IN_PROGRESS" {
		t.Errorf("order status after failed complete = %q, want IN_PROGRESS", fetchedOrder.Order.Status)
	}
}
