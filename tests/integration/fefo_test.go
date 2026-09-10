package integration

import (
	"net/http"
	"testing"
)

// TestBatchListFollowsFEFOOrder proves GET /batches/product/{id} returns
// ACTIVE batches by expiration ascending with NULL expirations last.
func TestBatchListFollowsFEFOOrder(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)

	resp := h.expectStatus(h.do(http.MethodGet, "/api/v1/batches/product/"+seedRawMaterialID, operator, nil), http.StatusOK)

	var batches []struct {
		ID          string `json:"id"`
		BatchNumber string `json:"batch_number"`
	}
	h.decode(resp, &batches)

	wantOrder := []string{seedRMOldBatchID, seedRMMidBatchID, seedRMNewBatchID, seedRMNullBatchID}
	if len(batches) != len(wantOrder) {
		t.Fatalf("batch count = %d, want %d", len(batches), len(wantOrder))
	}
	for i, wantID := range wantOrder {
		if batches[i].ID != wantID {
			t.Errorf("batch[%d] = %s, want %s", i, batches[i].ID, wantID)
		}
	}
}

// TestConsumeDrainsBatchesInFEFOOrder consumes across several seeded batches
// and asserts the oldest expiration is drained first and the NULL expiration
// last, with the audit rows recorded in that same order.
func TestConsumeDrainsBatchesInFEFOOrder(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)

	// 15 units: the 10-unit oldest batch plus 5 from the middle one.
	first := h.expectStatus(h.consume(operator, seedRawMaterialID, "15.0000"), http.StatusCreated)

	var firstTransactions []struct {
		BatchID string `json:"batch_id"`
	}
	h.decode(first, &firstTransactions)
	if len(firstTransactions) != 2 {
		t.Fatalf("first consume transactions = %d, want 2", len(firstTransactions))
	}
	if firstTransactions[0].BatchID != seedRMOldBatchID || firstTransactions[1].BatchID != seedRMMidBatchID {
		t.Errorf("first consume order = %s, %s; want oldest then middle",
			firstTransactions[0].BatchID, firstTransactions[1].BatchID)
	}

	remaining := []struct {
		batchID string
		want    string
	}{
		{seedRMOldBatchID, "0.0000"},
		{seedRMMidBatchID, "5.0000"},
		{seedRMNewBatchID, "10.0000"},
		{seedRMNullBatchID, "10.0000"},
	}
	for _, check := range remaining {
		if got := h.batchQuantity(check.batchID); got != check.want {
			t.Errorf("batch %s quantity = %s, want %s", check.batchID, got, check.want)
		}
	}

	// Drain the remaining 25 units: middle (5), newest (10), NULL-expiry (10).
	second := h.expectStatus(h.consume(operator, seedRawMaterialID, "25.0000"), http.StatusCreated)

	var secondTransactions []struct {
		BatchID string `json:"batch_id"`
	}
	h.decode(second, &secondTransactions)
	if len(secondTransactions) != 3 {
		t.Fatalf("second consume transactions = %d, want 3", len(secondTransactions))
	}
	wantSecondOrder := []string{seedRMMidBatchID, seedRMNewBatchID, seedRMNullBatchID}
	for i, wantID := range wantSecondOrder {
		if secondTransactions[i].BatchID != wantID {
			t.Errorf("second consume transaction[%d] = %s, want %s", i, secondTransactions[i].BatchID, wantID)
		}
	}

	for _, batchID := range []string{seedRMOldBatchID, seedRMMidBatchID, seedRMNewBatchID, seedRMNullBatchID} {
		if got := h.batchQuantity(batchID); got != "0.0000" {
			t.Errorf("batch %s quantity = %s, want 0.0000", batchID, got)
		}
	}
}
