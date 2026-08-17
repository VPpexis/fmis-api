// Package services for products.
package services

import (
	"context"
	"errors"
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"
	"fmis-api/internal/testutil"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createProduct inserts a product through the service and fails the test on error.
func createProduct(ctx context.Context, t *testing.T, svc *ProductService, sku string, ptype models.ProductType) models.Product {
	t.Helper()
	p, err := svc.Create(ctx, schemas.CreateProductRequest{
		SKU:           sku,
		Name:          "Test product",
		UnitOfMeasure: "kg",
		ProductType:   string(ptype),
		IsPurchasable: true,
		IsSellable:    ptype == models.ProductTypeFinishedGood,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	return p
}

// seedBatch inserts a batch directly so tests can shape its status without
// going through the batch service.
func seedBatch(ctx context.Context, t *testing.T, pool *pgxpool.Pool, productID uuid.UUID, status models.BatchStatusType) {
	t.Helper()
	exp := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status, expiration_date)
		VALUES ($1, $2, 10, 10, $3, $4)`,
		productID, "LOT-"+uuid.NewString()[:8], status, exp); err != nil {
		t.Fatalf("seed batch: %v", err)
	}
}

// assertDeleted checks whether the product row carries a deleted_at timestamp.
func assertDeleted(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID, want bool) {
	t.Helper()
	var deletedAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM products WHERE id = $1`, id).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if deletedAt.Valid != want {
		t.Errorf("deleted_at set = %v, want %v", deletedAt.Valid, want)
	}
}

// TestCreateProduct covers the happy path and the duplicate-SKU rule.
func TestCreateProduct(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := NewProductService(pool)

	req := schemas.CreateProductRequest{
		SKU:           "SKU-ALPHA",
		Name:          "Organic Wheat Flour",
		UnitOfMeasure: "kg",
		ProductType:   string(models.ProductTypeRawMaterial),
		IsPurchasable: true,
		IsSellable:    true,
	}

	created, err := svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Error("created product has no ID")
	}
	if created.SKU != req.SKU || created.ProductType != models.ProductTypeRawMaterial {
		t.Errorf("created = sku=%s type=%s, want sku=%s type=%s",
			created.SKU, created.ProductType, req.SKU, models.ProductTypeRawMaterial)
	}

	if _, err := svc.Create(ctx, req); !errors.Is(err, ErrDuplicateSKU) {
		t.Errorf("duplicate SKU error = %v, want ErrDuplicateSKU", err)
	}
}

// TestGetByID covers found, unknown, and soft-deleted products.
func TestGetByID(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := NewProductService(pool)

	created := createProduct(ctx, t, svc, "SKU-GET", models.ProductTypePackaging)

	got, err := svc.GetByID(ctx, created.ID.String())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name || got.ProductType != created.ProductType {
		t.Errorf("GetByID = %+v, want the created product", got)
	}

	if _, err := svc.GetByID(ctx, uuid.New().String()); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("unknown product error = %v, want ErrProductNotFound", err)
	}

	if err := svc.SoftDelete(ctx, created.ID.String()); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := svc.GetByID(ctx, created.ID.String()); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("deleted product error = %v, want ErrProductNotFound", err)
	}
}

// TestUpdateProduct covers partial merge and product_type immutability.
func TestUpdateProduct(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := NewProductService(pool)

	created := createProduct(ctx, t, svc, "SKU-UPD", models.ProductTypeRawMaterial)

	// PATCH with only name: everything else must stay untouched.
	updated, err := svc.Update(ctx, created.ID.String(), schemas.UpdateProductRequest{
		Name: ptr("Renamed product"),
	})
	if err != nil {
		t.Fatalf("Update name: %v", err)
	}
	if updated.Name != "Renamed product" {
		t.Errorf("name = %q, want %q", updated.Name, "Renamed product")
	}
	if updated.SKU != created.SKU || updated.UnitOfMeasure != created.UnitOfMeasure ||
		updated.ProductType != created.ProductType ||
		updated.IsPurchasable != created.IsPurchasable || updated.IsSellable != created.IsSellable {
		t.Errorf("partial update changed untouched fields: got %+v, want original %+v", updated, created)
	}

	// Full PATCH: all fields change, but product_type stays frozen.
	full, err := svc.Update(ctx, created.ID.String(), schemas.UpdateProductRequest{
		SKU:           ptr("SKU-RENAMED"),
		Name:          ptr("Fully updated"),
		UnitOfMeasure: ptr("pcs"),
		IsPurchasable: ptr(false),
		IsSellable:    ptr(true),
	})
	if err != nil {
		t.Fatalf("Update all fields: %v", err)
	}
	if full.SKU != "SKU-RENAMED" || full.UnitOfMeasure != "pcs" || full.IsPurchasable || !full.IsSellable {
		t.Errorf("full update = %+v, want all fields changed", full)
	}
	if full.ProductType != models.ProductTypeRawMaterial {
		t.Errorf("product_type = %s, want RAW_MATERIAL (immutable after create)", full.ProductType)
	}

	if _, err := svc.Update(ctx, uuid.New().String(), schemas.UpdateProductRequest{
		Name: ptr("Ghost"),
	}); !errors.Is(err, ErrProductNotFound) {
		t.Errorf("unknown product error = %v, want ErrProductNotFound", err)
	}
}

// TestListProducts covers the optional type filter, pagination, and the
// exclusion of soft-deleted products.
func TestListProducts(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := NewProductService(pool)

	createProduct(ctx, t, svc, "SKU-RAW-1", models.ProductTypeRawMaterial)
	createProduct(ctx, t, svc, "SKU-RAW-2", models.ProductTypeRawMaterial)
	packaging := createProduct(ctx, t, svc, "SKU-PKG", models.ProductTypePackaging)
	createProduct(ctx, t, svc, "SKU-FG", models.ProductTypeFinishedGood)

	all, err := svc.List(ctx, "", "", "")
	if err != nil {
		t.Fatalf("List without filter: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("len(all) = %d, want 4", len(all))
	}

	raw, err := svc.List(ctx, string(models.ProductTypeRawMaterial), "", "")
	if err != nil {
		t.Fatalf("List RAW_MATERIAL: %v", err)
	}
	if len(raw) != 2 {
		t.Errorf("len(raw) = %d, want 2", len(raw))
	}
	for _, p := range raw {
		if p.ProductType != models.ProductTypeRawMaterial {
			t.Errorf("filtered list contains %s, want only RAW_MATERIAL", p.ProductType)
		}
	}

	pkg, err := svc.List(ctx, string(models.ProductTypePackaging), "", "")
	if err != nil {
		t.Fatalf("List PACKAGING: %v", err)
	}
	if len(pkg) != 1 {
		t.Errorf("len(pkg) = %d, want 1", len(pkg))
	}

	page1, err := svc.List(ctx, "", "2", "0")
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	page2, err := svc.List(ctx, "", "2", "2")
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	page3, err := svc.List(ctx, "", "2", "4")
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page1) != 2 || len(page2) != 2 || len(page3) != 0 {
		t.Errorf("pagination sizes = %d/%d/%d, want 2/2/0", len(page1), len(page2), len(page3))
	}

	if delErr := svc.SoftDelete(ctx, packaging.ID.String()); delErr != nil {
		t.Fatalf("SoftDelete: %v", delErr)
	}
	after, err := svc.List(ctx, "", "", "")
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(after) != 3 {
		t.Errorf("len(after) = %d, want 3 (deleted product excluded)", len(after))
	}
}

// TestSoftDelete covers the acceptance criterion: deletion is blocked while
// ACTIVE batches exist, and succeeds for products without them.
func TestSoftDelete(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	svc := NewProductService(pool)

	t.Run("product without batches is soft-deleted", func(t *testing.T) {
		p := createProduct(ctx, t, svc, "SKU-CLEAN", models.ProductTypeRawMaterial)
		if err := svc.SoftDelete(ctx, p.ID.String()); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		assertDeleted(ctx, t, pool, p.ID, true)
	})

	t.Run("product with ACTIVE batch is rejected", func(t *testing.T) {
		p := createProduct(ctx, t, svc, "SKU-ACTIVE", models.ProductTypeRawMaterial)
		seedBatch(ctx, t, pool, p.ID, models.BatchStatusTypeActive)

		if err := svc.SoftDelete(ctx, p.ID.String()); !errors.Is(err, ErrProductHasActiveBatches) {
			t.Errorf("error = %v, want ErrProductHasActiveBatches", err)
		}
		assertDeleted(ctx, t, pool, p.ID, false)
	})

	t.Run("product with DEPLETED batch is soft-deleted", func(t *testing.T) {
		p := createProduct(ctx, t, svc, "SKU-DEPLETED", models.ProductTypeRawMaterial)
		seedBatch(ctx, t, pool, p.ID, models.BatchStatusTypeDepleted)

		if err := svc.SoftDelete(ctx, p.ID.String()); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		assertDeleted(ctx, t, pool, p.ID, true)
	})

	t.Run("unknown product is not found", func(t *testing.T) {
		if err := svc.SoftDelete(ctx, uuid.New().String()); !errors.Is(err, ErrProductNotFound) {
			t.Errorf("error = %v, want ErrProductNotFound", err)
		}
	})

	t.Run("already deleted product is not found", func(t *testing.T) {
		p := createProduct(ctx, t, svc, "SKU-TWICE", models.ProductTypeRawMaterial)
		if err := svc.SoftDelete(ctx, p.ID.String()); err != nil {
			t.Fatalf("first SoftDelete: %v", err)
		}
		if err := svc.SoftDelete(ctx, p.ID.String()); !errors.Is(err, ErrProductNotFound) {
			t.Errorf("second SoftDelete error = %v, want ErrProductNotFound", err)
		}
	})
}
