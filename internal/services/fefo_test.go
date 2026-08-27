// Package services FEFO selection tests.
package services

import (
	"errors"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"fmis-api/internal/models"
)

func batchWithQuantity(t *testing.T, quantity string) models.InventoryBatch {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(quantity); err != nil {
		t.Fatalf("scan quantity %q: %v", quantity, err)
	}
	return models.InventoryBatch{
		ID:              uuid.New(),
		QuantityCurrent: n,
		Status:          models.BatchStatusTypeActive,
	}
}

func batchesWithQuantities(t *testing.T, quantities []string) []models.InventoryBatch {
	t.Helper()
	batches := make([]models.InventoryBatch, len(quantities))
	for i, q := range quantities {
		batches[i] = batchWithQuantity(t, q)
	}
	return batches
}

func TestSelectFEFO(t *testing.T) {
	tests := []struct {
		name       string
		quantities []string
		requested  string
		wantTakes  []string
		wantFrom   []int // index of the input batch each selection was taken from
		wantErr    bool
	}{
		{"single batch covers request", []string{"10.0000"}, "4", []string{"4.0000"}, []int{0}, false},
		{"spans multiple batches", []string{"3.0000", "5.0000", "10.0000"}, "8", []string{"3.0000", "5.0000"}, []int{0, 1}, false},
		{"exact stock", []string{"2.5000", "1.0000"}, "3.5000", []string{"2.5000", "1.0000"}, []int{0, 1}, false},
		{"fractional take rounds to 4 decimals", []string{"0.5000"}, "0.25", []string{"0.2500"}, []int{0}, false},
		{"skips zero-quantity batch", []string{"0.0000", "4.0000"}, "2", []string{"2.0000"}, []int{1}, false},
		{"stop at exact coverage", []string{"3.0000", "5.0000"}, "3", []string{"3.0000"}, []int{0}, false},
		{"insufficient stock", []string{"1.0000", "2.0000"}, "4", nil, nil, true},
		{"empty batches", nil, "1", nil, nil, true},
		{"zero request leaves batches untouched", []string{"5.0000"}, "0", nil, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batches := batchesWithQuantities(t, tt.quantities)
			requested, err := strconv.ParseFloat(tt.requested, 64)
			if err != nil {
				t.Fatalf("parse requested %q: %v", tt.requested, err)
			}

			got, err := SelectFEFO(batches, requested)
			if tt.wantErr {
				if !errors.Is(err, ErrInsufficientStock) {
					t.Fatalf("want ErrInsufficientStock, got %v", err)
				}
				if got != nil {
					t.Fatalf("want nil selections on error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectFEFO returned unexpected error: %v", err)
			}

			if len(got) != len(tt.wantTakes) {
				t.Fatalf("want %d selections, got %d: %v", len(tt.wantTakes), len(got), got)
			}
			for i, want := range tt.wantTakes {
				if got[i].Take != want {
					t.Errorf("selection %d: want take %q, got %q", i, want, got[i].Take)
				}
				wantID := batches[tt.wantFrom[i]].ID
				if got[i].BatchID != wantID {
					t.Errorf("selection %d: want batch %s, got %s", i, wantID, got[i].BatchID)
				}
			}
		})
	}
}

func TestSelectFEFOLeavesInputsUntouched(t *testing.T) {
	batches := batchesWithQuantities(t, []string{"3.0000", "5.0000"})
	original := make([]models.InventoryBatch, len(batches))
	copy(original, batches)

	selections, err := SelectFEFO(batches, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(selections) != 2 {
		t.Fatalf("want 2 selections, got %d", len(selections))
	}
	for i := range batches {
		if batches[i].QuantityCurrent.Int.Cmp(original[i].QuantityCurrent.Int) != 0 {
			t.Errorf("batch %d quantity was mutated by SelectFEFO", i)
		}
	}
}
