package integration

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestConsumeConcurrentTwoRequestsOneWins proves the FOR UPDATE row lock
// serializes two simultaneous consumes of 7 units against a single 10-unit
// batch: exactly one may succeed and the other must fail on insufficient
// stock, never overselling.
func TestConsumeConcurrentTwoRequestsOneWins(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)
	productID := h.createProduct(operator, "CONC-TWO", "Concurrent Two", "kg", "RAW_MATERIAL")
	batchID := h.receiveBatch(operator, productID, "CONC-TWO-LOT", "10.0000", expirationPtr())

	statuses, errs := runConcurrentConsumes(h, operator, productID, "7.0000", 2)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("consume worker %d: %v", i, err)
		}
	}

	successes := 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumes = %d, want exactly 1; statuses: %v", successes, statuses)
	}

	if got := h.batchQuantity(batchID); got != "3.0000" {
		t.Errorf("batch quantity = %s, want 3.0000", got)
	}
	if got := h.countTransactions("OUTGOING"); got != 1 {
		t.Errorf("OUTGOING transactions = %d, want 1", got)
	}
}

// TestConsumeConcurrentNoOverselling fires 20 parallel one-unit consumes at a
// 10-unit batch. Exactly 10 must succeed and the batch must end empty: stale
// reads without the row lock would let more requests through.
func TestConsumeConcurrentNoOverselling(t *testing.T) {
	h := newSeededHarness(t)
	operator := h.login(seedOperatorUser, seedPassword)
	productID := h.createProduct(operator, "CONC-MANY", "Concurrent Many", "kg", "RAW_MATERIAL")
	batchID := h.receiveBatch(operator, productID, "CONC-MANY-LOT", "10.0000", expirationPtr())

	const requests = 20
	statuses, errs := runConcurrentConsumes(h, operator, productID, "1.0000", requests)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("consume worker %d: %v", i, err)
		}
	}

	successes := 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			successes++
		}
	}
	if successes != 10 {
		t.Errorf("successful consumes = %d, want exactly 10 (overselling)", successes)
	}

	if got := h.batchQuantity(batchID); got != "0.0000" {
		t.Errorf("batch quantity = %s, want 0.0000", got)
	}
	if got := h.countTransactions("OUTGOING"); got != 10 {
		t.Errorf("OUTGOING transactions = %d, want 10", got)
	}
}

// expirationPtr returns a future expiration date for products that require one.
func expirationPtr() *time.Time {
	expiration := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)
	return &expiration
}

// runConcurrentConsumes starts every worker on a shared barrier and returns
// each worker's HTTP status (or transport error). It never calls t.Fatalf, so
// it is safe to use from goroutines.
func runConcurrentConsumes(h *harness, token, productID, quantity string, workers int) ([]int, []error) {
	statuses := make([]int, workers)
	errs := make([]error, workers)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			statuses[index], errs[index] = h.doConcurrent(http.MethodPost, "/api/v1/inventory/consume", token, map[string]any{
				"product_id": productID,
				"quantity":   quantity,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	return statuses, errs
}
