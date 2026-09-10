package integration

import (
	"net/http"
	"testing"
)

// TestSeedUsersLoginWithKnownRoles proves every seeded account authenticates
// through the real login endpoint and resolves to its seeded identity.
func TestSeedUsersLoginWithKnownRoles(t *testing.T) {
	h := newSeededHarness(t)

	cases := []struct {
		username string
		wantID   string
		wantRole string
	}{
		{seedAdminUser, seedAdminID, "ADMIN"},
		{seedOperatorUser, seedOperatorID, "OPERATOR"},
		{seedViewerUser, seedViewerID, "VIEWER"},
	}

	for _, tc := range cases {
		token := h.login(tc.username, seedPassword)

		var me struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		h.decode(h.expectStatus(h.do(http.MethodGet, "/api/v1/auth/me", token, nil), http.StatusOK), &me)

		if me.ID != tc.wantID || me.Role != tc.wantRole {
			t.Errorf("%s resolved to id=%s role=%s, want id=%s role=%s",
				tc.username, me.ID, me.Role, tc.wantID, tc.wantRole)
		}
	}
}

// TestSeedFixturesAreQueryable proves the seeded catalog and stock are visible
// through the API and that the seeded roles enforce the RBAC matrix.
func TestSeedFixturesAreQueryable(t *testing.T) {
	h := newSeededHarness(t)
	admin := h.login(seedAdminUser, seedPassword)
	operator := h.login(seedOperatorUser, seedPassword)
	viewer := h.login(seedViewerUser, seedPassword)

	var products []productSummary
	h.decode(h.expectStatus(h.do(http.MethodGet, "/api/v1/products/", viewer, nil), http.StatusOK), &products)
	for _, wantID := range []string{seedRawMaterialID, seedWhiteLabelID, seedFinishedGoodID} {
		if !containsProduct(products, wantID) {
			t.Errorf("seeded product %s missing from product list", wantID)
		}
	}

	var batches []struct {
		ID string `json:"id"`
	}
	h.decode(h.expectStatus(h.do(http.MethodGet, "/api/v1/batches/product/"+seedRawMaterialID, viewer, nil), http.StatusOK), &batches)
	if len(batches) != 4 {
		t.Errorf("seeded raw material batches = %d, want 4", len(batches))
	}

	h.expectStatus(h.do(http.MethodPost, "/api/v1/products/", viewer, map[string]any{
		"sku":             "SEED-VIEWER-1",
		"name":            "Viewer Product",
		"unit_of_measure": "kg",
		"product_type":    "RAW_MATERIAL",
	}), http.StatusForbidden)

	created := h.expectStatus(h.do(http.MethodPost, "/api/v1/products/", operator, map[string]any{
		"sku":             "SEED-OPERATOR-1",
		"name":            "Operator Product",
		"unit_of_measure": "kg",
		"product_type":    "RAW_MATERIAL",
	}), http.StatusCreated)

	var product struct {
		ID string `json:"id"`
	}
	h.decode(created, &product)

	h.expectStatus(h.do(http.MethodDelete, "/api/v1/products/"+product.ID, operator, nil), http.StatusForbidden)
	h.expectStatus(h.do(http.MethodDelete, "/api/v1/products/"+product.ID, admin, nil), http.StatusNoContent)
}
