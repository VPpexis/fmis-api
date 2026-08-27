// Package services FEFO selection helpers.
package services

import (
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"

	"fmis-api/internal/models"
)

// FEFOSelection is one batch deduction chosen by the FEFO algorithm.
type FEFOSelection struct {
	BatchID uuid.UUID
	Take    string // decimal string quantity to deduct from the batch
}

// SelectFEFO picks how much to deduct from each batch to cover requested
// quantity. batches must already be ordered by expiration date ascending
// (expired batches last), as returned by the FEFO repository query. It never
// touches the database, so it can be unit-tested in isolation.
func SelectFEFO(batches []models.InventoryBatch, requested float64) ([]FEFOSelection, error) {
	remaining := requested
	var selections []FEFOSelection
	for i := range batches {
		batch := &batches[i]
		if remaining <= 0 {
			break
		}

		current, err := batch.QuantityCurrent.Float64Value()
		if err != nil {
			return nil, fmt.Errorf("parse batch %s quantity: %w", batch.ID, err)
		}
		take := math.Min(current.Float64, remaining)
		if take <= 0 {
			continue
		}

		selections = append(selections, FEFOSelection{
			BatchID: batch.ID,
			Take:    strconv.FormatFloat(take, 'f', 4, 64),
		})
		remaining -= take
	}

	if remaining > 0 {
		return nil, ErrInsufficientStock
	}
	return selections, nil
}
