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
func (b *BatchRouter) quarantine(w http.ResponseWriter, r *http.Request) {
	batchID := chi.URLParam(r, "batch_id")

	batch, err := b.svc.Quarantine(r.Context(), batchID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, batch)
}
