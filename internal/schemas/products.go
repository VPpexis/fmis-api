// Package schemas for products.
package schemas

// CreateProductRequest is the payload for POST /api/v1/products.
type CreateProductRequest struct {
	SKU           string `json:"sku" validate:"required,max=100"`
	Name          string `json:"name" validate:"required,max=200"`
	UnitOfMeasure string `json:"unit_of_measure" validate:"required,max=20"`
	ProductType   string `json:"product_type" validate:"required,oneof=RAW_MATERIAL PACKAGING FINISHED_GOOD WHITE_LABEL"`
	IsPurchasable bool   `json:"is_purchasable"`
	IsSellable    bool   `json:"is_sellable"`
}

// UpdateProductRequest is the payload for PATCH /api/v1/products/{product_id}.
type UpdateProductRequest struct {
	SKU           *string `json:"sku" validate:"omitempty,max=100"`
	Name          *string `json:"name" validate:"omitempty,max=200"`
	UnitOfMeasure *string `json:"unit_of_measure" validate:"omitempty,max=20"`
	IsPurchasable *bool   `json:"is_purchasable"`
	IsSellable    *bool   `json:"is_sellable"`
}
