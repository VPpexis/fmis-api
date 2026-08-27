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
// @Summary Create a product
// @Description Adds a new catalog item. ADMIN or OPERATOR role required.
// @Tags products
// @Accept json
// @Produce json
// @Param body body schemas.CreateProductRequest true "Product payload"
// @Success 201 {object} schemas.Product
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 409 {object} map[string]string "SKU already exists"
// @Router /products [post]
// @Security BearerAuth
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
// @Summary List products
// @Description Lists catalog products, optionally filtered by product type and paginated.
// @Tags products
// @Produce json
// @Param product_type query string false "Filter by type (RAW_MATERIAL, PACKAGING, FINISHED_GOOD, WHITE_LABEL)"
// @Param limit query int false "Max results (default 100)"
// @Param offset query int false "Skip N results"
// @Success 200 {array} schemas.Product
// @Failure 400 {object} map[string]string "Invalid filter values"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Router /products [get]
// @Security BearerAuth
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
// @Summary Get a product
// @Description Retrieves a single catalog product by UUID.
// @Tags products
// @Produce json
// @Param product_id path string true "Product UUID"
// @Success 200 {object} schemas.Product
// @Failure 400 {object} map[string]string "Malformed product id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 404 {object} map[string]string "Product not found"
// @Router /products/{product_id} [get]
// @Security BearerAuth
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
// @Summary Update a product
// @Description Partially updates a catalog product. ADMIN role required.
// @Tags products
// @Accept json
// @Produce json
// @Param product_id path string true "Product UUID"
// @Param body body schemas.UpdateProductRequest true "Fields to update"
// @Success 200 {object} schemas.Product
// @Failure 400 {object} map[string]string "Invalid body or product id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 409 {object} map[string]string "SKU already exists"
// @Router /products/{product_id} [patch]
// @Security BearerAuth
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
// @Summary Delete a product
// @Description Soft-deletes a catalog product. ADMIN role required. Fails if the product still has active batches.
// @Tags products
// @Param product_id path string true "Product UUID"
// @Success 204 "No content"
// @Failure 400 {object} map[string]string "Malformed product id"
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Failure 403 {object} map[string]string "Insufficient role"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 409 {object} map[string]string "Product has active batches"
// @Router /products/{product_id} [delete]
// @Security BearerAuth
func (p *ProductRouter) delete(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "product_id")

	if err := p.svc.SoftDelete(r.Context(), productID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
