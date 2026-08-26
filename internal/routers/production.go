// Package routers for production
package routers

import (
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"
	"fmis-api/internal/services"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// ProductionOrderRouter serves the /api/v1/production endpoints.
type ProductionOrderRouter struct {
	svc      *services.ProductionOrderService
	validate *validator.Validate
	logger   *slog.Logger
}

// NewProductionOrderRouter constructs the production HTTP handlers.
func NewProductionOrderRouter(svc *services.ProductionOrderService, logger *slog.Logger) *ProductionOrderRouter {
	return &ProductionOrderRouter{svc: svc, validate: schemas.NewValidator(), logger: logger}
}

// create handles POST /api/v1/production
func (po *ProductionOrderRouter) create(w http.ResponseWriter, r *http.Request) {
	var req schemas.CreateProductionOrderRequest
	if err := decodeAndValidate(po.validate, w, r, &req); err != nil {
		return
	}

	productionOrder, productionOrderLineItems, err := po.svc.Create(r.Context(), &req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Order     models.ProductionOrder           `json:"order"`
		LineItems []models.ProductionOrderLineItem `json:"line_items"`
	}{
		Order:     productionOrder,
		LineItems: productionOrderLineItems,
	})
}

// complete handles POST /api/v1/production/{id}/complete
func (po *ProductionOrderRouter) complete(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, productionOrderLineItem, err := po.svc.Complete(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Order     models.ProductionOrder           `json:"order"`
		LineItems []models.ProductionOrderLineItem `json:"line_items"`
	}{
		Order:     productionOrder,
		LineItems: productionOrderLineItem,
	})
}

// start handles POST /api/v1/production/{id}/start
func (po *ProductionOrderRouter) start(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, err := po.svc.Start(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productionOrder)
}

// cancel handles POST /api/v1/production/{id}/cancel
func (po *ProductionOrderRouter) cancel(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, err := po.svc.Cancel(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productionOrder)
}
