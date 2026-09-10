package integration

import (
	"net/http"
	"testing"
)

// TestProductsLifecycle walks a product through create, read, list, update,
// soft delete, and the RBAC boundary around writes.
func TestProductsLifecycle(t *testing.T) {
	h := newSeededHarness(t)
	admin := h.login(seedAdminUser, seedPassword)
	operator := h.login(seedOperatorUser, seedPassword)
	viewer := h.login(seedViewerUser, seedPassword)

	created := h.expectStatus(h.do(http.MethodPost, "/api/v1/products/", operator, map[string]any{
		"sku":             "PROD-LC-001",
		"name":            "Product Lifecycle",
		"unit_of_measure": "kg",
		"product_type":    "RAW_MATERIAL",
		"is_purchasable":  true,
		"is_sellable":     false,
	}), http.StatusCreated)

	var product struct {
		ID   string `json:"id"`
		SKU  string `json:"sku"`
		Name string `json:"name"`
	}
	h.decode(created, &product)
	if product.SKU != "PROD-LC-001" || product.Name != "Product Lifecycle" {
		t.Fatalf("created product = %+v, want SKU PROD-LC-001 and name Product Lifecycle", product)
	}

	fetched := h.expectStatus(h.do(http.MethodGet, "/api/v1/products/"+product.ID, viewer, nil), http.StatusOK)
	var fetchedProduct struct {
		ID string `json:"id"`
	}
	h.decode(fetched, &fetchedProduct)
	if fetchedProduct.ID != product.ID {
		t.Errorf("fetched product id = %q, want %q", fetchedProduct.ID, product.ID)
	}

	listed := h.expectStatus(h.do(http.MethodGet, "/api/v1/products/?product_type=RAW_MATERIAL", viewer, nil), http.StatusOK)
	var products []productSummary
	h.decode(listed, &products)
	if !containsProduct(products, product.ID) {
		t.Errorf("filtered product list does not contain %q", product.ID)
	}

	updated := h.expectStatus(h.do(http.MethodPatch, "/api/v1/products/"+product.ID, admin, map[string]any{
		"name": "Product Lifecycle Updated",
	}), http.StatusOK)
	var updatedProduct struct {
		Name string `json:"name"`
	}
	h.decode(updated, &updatedProduct)
	if updatedProduct.Name != "Product Lifecycle Updated" {
		t.Errorf("updated product name = %q, want Product Lifecycle Updated", updatedProduct.Name)
	}

	h.expectStatus(h.do(http.MethodPost, "/api/v1/products/", viewer, map[string]any{
		"sku":             "PROD-LC-VIEWER",
		"name":            "Viewer Product",
		"unit_of_measure": "kg",
		"product_type":    "RAW_MATERIAL",
	}), http.StatusForbidden)

	h.expectStatus(h.do(http.MethodDelete, "/api/v1/products/"+product.ID, admin, nil), http.StatusNoContent)
	h.expectStatus(h.do(http.MethodGet, "/api/v1/products/"+product.ID, admin, nil), http.StatusNotFound)
}

// productSummary is the product list entry tests assert on.
type productSummary struct {
	ID string `json:"id"`
}

// containsProduct reports whether any listed product carries the given ID.
func containsProduct(products []productSummary, id string) bool {
	for _, product := range products {
		if product.ID == id {
			return true
		}
	}
	return false
}
