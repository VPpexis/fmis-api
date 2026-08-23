// Package schemas for inventory.
package schemas

// AdjustStockRequest is the payload for recording a WASTE or ADJUSTMENT stock movement.
type AdjustStockRequest struct {
	BatchID         string  `json:"batch_id" validate:"required,uuid"`
	QuantityChange  string  `json:"quantity_change" validate:"required,signed_quantity"`
	TransactionType string  `json:"transaction_type" validate:"required,oneof=WASTE ADJUSTMENT"`
	ReferenceNote   *string `json:"reference_note" validate:"omitempty,max=500"`
}

// ConsumeStockRequest is the payload for recording an OUTGOING stock movement.
type ConsumeStockRequest struct {
	ProductID     string  `json:"product_id" validate:"required,uuid"`
	Quantity      string  `json:"quantity" validate:"required,quantity"`
	ReferenceNote *string `json:"reference_note" validate:"omitempty,max=500"`
}
