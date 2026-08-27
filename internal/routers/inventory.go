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
// @Summary Adjust or waste stock
// @Description Records a WASTE or ADJUSTMENT stock movement on a single batch. ADMIN or OPERATOR role required.
// @Tags inventory
// @Accept json
// @Produce json
// @Param body body schemas.AdjustStockRequest true "Adjustment payload"
// @Success 201 {object} schemas.StockTransaction
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Batch not found"
// @Failure 409 {object} map[string]string "Batch state prevents adjustment"
// @Failure 422 {object} map[string]string "Quantity would exceed available stock"
// @Router /inventory/adjust [post]
// @Security BearerAuth
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
// @Summary Consume stock (FEFO)
// @Description Deducts stock across batches of a product, oldest expiration first (FEFO). ADMIN or OPERATOR role required.
// @Tags inventory
// @Accept json
// @Produce json
// @Param body body schemas.ConsumeStockRequest true "Consumption payload"
// @Success 201 {array} schemas.StockTransaction
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 409 {object} map[string]string "Insufficient stock"
// @Router /inventory/consume [post]
// @Security BearerAuth
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
// @Summary List stock transactions
// @Description Returns the immutable audit trail of stock movements, optionally filtered.
// @Tags inventory
// @Produce json
// @Param batch_id query string false "Filter by batch UUID"
// @Param transaction_type query string false "Filter by type (INCOMING, OUTGOING, USED_IN_PRODUCTION, WASTE, ADJUSTMENT)"
// @Param date_from query string false "Only transactions created at or after this RFC3339 timestamp"
// @Param date_to query string false "Only transactions created at or before this RFC3339 timestamp"
// @Param limit query int false "Max results (default 100)"
// @Param offset query int false "Skip N results"
// @Success 200 {array} schemas.StockTransaction
// @Failure 400 {object} map[string]string "Invalid filter values"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Router /inventory/transactions/ [get]
// @Security BearerAuth
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
