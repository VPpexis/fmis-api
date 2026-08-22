// Package routers for inventory.
package routers

import (
	"fmis-api/internal/schemas"
	"fmis-api/internal/services"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// InventoryRouter serves the /api/v1/inventory endpoints.
type InventoryRouter struct {
	svc      *services.InventoryService
	validate *validator.Validate
	logger   *slog.Logger
}

// NewInventoryRouter constructs the inventory HTTP handlers.
func NewInventoryRouter(svc *services.InventoryService, logger *slog.Logger) *InventoryRouter {
	return &InventoryRouter{svc: svc, validate: schemas.NewValidator(), logger: logger}
}

// adjust handles POST /api/v1/inventory/adjust
func (i *InventoryRouter) adjust(w http.ResponseWriter, r *http.Request) {
	var req schemas.AdjustStockRequest
	if err := decodeAndValidate(i.validate, w, r, &req); err != nil {
		return
	}

	transaction, err := i.svc.Adjust(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, transaction)
}
