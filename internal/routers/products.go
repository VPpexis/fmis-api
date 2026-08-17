package routers

import (
	"fmis-api/internal/schemas"
	"fmis-api/internal/services"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// ProductRouter serves the /api/v1/products endpoints
type ProductRouter struct {
	svc      *services.ProductService
	validate *validator.Validate
	logger   *slog.Logger
}

// NewProductRouter constructs the product HTTP handlers.
func NewProductRouter(svc *services.ProductService, logger *slog.Logger) *ProductRouter {
	return &ProductRouter{svc: svc, validate: schemas.NewValidator(), logger: logger}
}

// create handles POST /api/v1/products
func (p *ProductRouter) create(w http.ResponseWriter, r *http.Request) {
	var req schemas.CreateProductRequest
	if err := decodeAndValidate(p.validate, w, r, &req); err != nil {
		return
	}

	product, err := p.svc.Create(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, product)
}

// list handles GET /api/v1/products
func (p *ProductRouter) list(w http.ResponseWriter, r *http.Request) {
	productType := r.URL.Query().Get("product_type")
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")

	products, err := p.svc.List(r.Context(), productType, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, products)
}

// getByID handles GET /api/v1/products/{product_id}
func (p *ProductRouter) getByID(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "product_id")

	product, err := p.svc.GetByID(r.Context(), productID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// update handles PATCH /api/v1/products/{product_id}
func (p *ProductRouter) update(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "product_id")
	var req schemas.UpdateProductRequest
	if err := decodeAndValidate(p.validate, w, r, &req); err != nil {
		return
	}

	product, err := p.svc.Update(r.Context(), productID, req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// delete handles DELETE /api/v1/products/{id}
func (p *ProductRouter) delete(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "product_id")

	if err := p.svc.SoftDelete(r.Context(), productID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
