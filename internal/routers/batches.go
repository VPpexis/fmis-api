// Package routers for batches.
package routers

import (
	"fmis-api/internal/schemas"
	"fmis-api/internal/services"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// BatchRouter serves the /api/v1/batches endpoints.
type BatchRouter struct {
	svc      *services.BatchService
	validate *validator.Validate
	logger   *slog.Logger
}

// NewBatchRouter constructs the batch HTTP handlers.
func NewBatchRouter(svc *services.BatchService, logger *slog.Logger) *BatchRouter {
	return &BatchRouter{svc: svc, validate: schemas.NewValidator(), logger: logger}
}

// receive handles POST /api/v1/batches
// @Summary Receive a batch
// @Description Registers an incoming lot of a product. ADMIN or OPERATOR role required.
// @Tags batches
// @Accept json
// @Produce json
// @Param body body schemas.CreateBatchRequest true "Batch payload"
// @Success 201 {object} schemas.InventoryBatch
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 422 {object} map[string]string "Expiration date required for this product"
// @Router /batches [post]
// @Security BearerAuth
func (b *BatchRouter) receive(w http.ResponseWriter, r *http.Request) {
	var req schemas.CreateBatchRequest
	if err := decodeAndValidate(b.validate, w, r, &req); err != nil {
		return
	}

	batch, err := b.svc.Receive(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, batch)
}

// listByProduct handles GET /api/v1/batches/product/{product_id}
// @Summary List active batches for a product
// @Description Returns all non-depleted, non-quarantined batches for a product, ordered FEFO (soonest expiration first).
// @Tags batches
// @Produce json
// @Param product_id path string true "Product UUID"
// @Success 200 {array} schemas.InventoryBatch
// @Failure 400 {object} map[string]string "Malformed product id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 404 {object} map[string]string "Product not found"
// @Router /batches/product/{product_id} [get]
// @Security BearerAuth
func (b *BatchRouter) listByProduct(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "product_id")

	batches, err := b.svc.ListActiveByProduct(r.Context(), productID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batches)
}

// quarantine handles PATCH /api/v1/batches/{batch_id}/quarantine
// @Summary Quarantine a batch
// @Description Marks an ACTIVE batch as QUARANTINED so it is excluded from FEFO consumption. ADMIN or OPERATOR role required.
// @Tags batches
// @Produce json
// @Param batch_id path string true "Batch UUID"
// @Success 200 {object} schemas.InventoryBatch
// @Failure 400 {object} map[string]string "Malformed batch id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Batch not found"
// @Failure 409 {object} map[string]string "Batch not in ACTIVE state"
// @Router /batches/{batch_id}/quarantine [patch]
// @Security BearerAuth
func (b *BatchRouter) quarantine(w http.ResponseWriter, r *http.Request) {
	batchID := chi.URLParam(r, "batch_id")

	batch, err := b.svc.Quarantine(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batch)
}
