// Package schemas for production orders.
package schemas

import "time"

// CreateProductionOrderRequest is the payload for creating a production order.
type CreateProductionOrderRequest struct {
	LineItems            []ProductionLineItemRequest `json:"line_items" validate:"required,min=1,dive"`
	OutputProductID      string                      `json:"output_product_id" validate:"required,uuid"`
	OutputBatchNumber    string                      `json:"output_batch_number" validate:"required,max=100"`
	OutputQuantity       string                      `json:"output_quantity" validate:"required,quantity"`
	OutputExpirationDate *time.Time                  `json:"output_expiration_date"`
}

// ProductionLineItemRequest is one input batch and how much it gets consumed.
type ProductionLineItemRequest struct {
	InputBatchID     string `json:"input_batch_id" validate:"required,uuid"`
	QuantityConsumed string `json:"quantity_consumed" validate:"required,quantity"`
}
