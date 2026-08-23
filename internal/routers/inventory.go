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

// consume handles POST /api/v1/inventory/consume
func (i *InventoryRouter) consume(w http.ResponseWriter, r *http.Request) {
	var req schemas.ConsumeStockRequest
	if err := decodeAndValidate(i.validate, w, r, &req); err != nil {
		return
	}

	transactions, err := i.svc.Consume(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, transactions)
}

// listTransactions handles GET /api/v1/inventory/transactions/
func (i *InventoryRouter) listTransactions(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	transactionType := r.URL.Query().Get("transaction_type")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")

	transactions, err := i.svc.List(r.Context(), batchID, transactionType, dateFrom, dateTo, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, transactions)
}
