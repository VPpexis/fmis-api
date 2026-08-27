// Package routers for production
package routers

import (
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

// list handles GET /api/v1/production
// @Summary List production orders
// @Description Lists production orders, optionally filtered by status and paginated.
// @Tags production
// @Produce json
// @Param status query string false "Filter by status (PLANNED, IN_PROGRESS, COMPLETED, CANCELLED)"
// @Param limit query int false "Max results (default 100)"
// @Param offset query int false "Skip N results"
// @Success 200 {array} schemas.ProductionOrder
// @Failure 400 {object} map[string]string "Invalid filter values"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Router /production [get]
// @Security BearerAuth
func (po *ProductionOrderRouter) list(w http.ResponseWriter, r *http.Request) {
	productionOrderStatusType := r.URL.Query().Get("status")
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")

	productionOrders, err := po.svc.List(r.Context(), productionOrderStatusType, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productionOrders)
}

// create handles POST /api/v1/production
// @Summary Create a production order
// @Description Creates a PLANNED production order: validates the output product and input batches, reserves the output batch, and inserts the order with its line items in one transaction. ADMIN or OPERATOR role required.
// @Tags production
// @Accept json
// @Produce json
// @Param body body schemas.CreateProductionOrderRequest true "Production order payload"
// @Success 201 {object} schemas.ProductionOrderResponse
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Unknown product or input batch"
// @Failure 422 {object} map[string]string "Non-finished-good output or non-white-label input"
// @Router /production [post]
// @Security BearerAuth
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
	writeJSON(w, http.StatusCreated, schemas.ProductionOrderResponse{
		Order:     productionOrder,
		LineItems: productionOrderLineItems,
	})
}

// complete handles POST /api/v1/production/{id}/complete
// @Summary Complete a production order
// @Description Atomically deducts input batch quantities, finalizes the output batch, and links all transactions to the order. ADMIN or OPERATOR role required.
// @Tags production
// @Produce json
// @Param id path string true "Production order UUID"
// @Success 200 {object} schemas.ProductionOrderResponse
// @Failure 400 {object} map[string]string "Malformed id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Production order not found"
// @Failure 409 {object} map[string]string "Order not in IN_PROGRESS state"
// @Router /production/{id}/complete [post]
// @Security BearerAuth
func (po *ProductionOrderRouter) complete(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, productionOrderLineItem, err := po.svc.Complete(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ProductionOrderResponse{
		Order:     productionOrder,
		LineItems: productionOrderLineItem,
	})
}

// getByID handles GET /api/v1/production/{id}
// @Summary Get a production order
// @Description Retrieves a single production order with its line items.
// @Tags production
// @Produce json
// @Param id path string true "Production order UUID"
// @Success 200 {object} schemas.ProductionOrderResponse
// @Failure 400 {object} map[string]string "Malformed id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 404 {object} map[string]string "Production order not found"
// @Router /production/{id} [get]
// @Security BearerAuth
func (po *ProductionOrderRouter) getByID(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, productionOrderLineItem, err := po.svc.GetByID(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schemas.ProductionOrderResponse{
		Order:     productionOrder,
		LineItems: productionOrderLineItem,
	})
}

// start handles POST /api/v1/production/{id}/start
// @Summary Start a production order
// @Description Transitions a PLANNED order to IN_PROGRESS. The order row is locked to prevent concurrent starts. ADMIN or OPERATOR role required.
// @Tags production
// @Produce json
// @Param id path string true "Production order UUID"
// @Success 200 {object} schemas.ProductionOrder
// @Failure 400 {object} map[string]string "Malformed id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Production order not found"
// @Failure 409 {object} map[string]string "Order not in PLANNED state"
// @Router /production/{id}/start [post]
// @Security BearerAuth
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
// @Summary Cancel a production order
// @Description Cancels a PLANNED or IN_PROGRESS order. No inventory impact. ADMIN role required.
// @Tags production
// @Produce json
// @Param id path string true "Production order UUID"
// @Success 200 {object} schemas.ProductionOrder
// @Failure 400 {object} map[string]string "Malformed id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Production order not found"
// @Failure 409 {object} map[string]string "Order not in PLANNED or IN_PROGRESS state"
// @Router /production/{id}/cancel [post]
// @Security BearerAuth
func (po *ProductionOrderRouter) cancel(w http.ResponseWriter, r *http.Request) {
	productionOrderID := chi.URLParam(r, "id")

	productionOrder, err := po.svc.Cancel(r.Context(), productionOrderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, productionOrder)
}
