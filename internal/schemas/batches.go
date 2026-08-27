// Package schemas for batches.
package schemas

import (
	"time"

	"fmis-api/internal/models"
)

// CreateBatchRequest is the payload for creating a new inventory batch.
type CreateBatchRequest struct {
	ProductID      string     `json:"product_id" validate:"required,uuid"`
	BatchNumber    string     `json:"batch_number" validate:"required,max=100"`
	Quantity       string     `json:"quantity" validate:"required,quantity"`
	ExpirationDate *time.Time `json:"expiration_date"`
}

// InventoryBatch is the batch entity returned by batch endpoints.
type InventoryBatch = models.InventoryBatch
