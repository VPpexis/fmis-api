// Package repositories_test exercises the production queries from outside the
// package (external test package) to avoid the repositories -> testutil
// import cycle; testutil itself depends on repositories.
package repositories_test

import (
	"context"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/testutil"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestProductionOrderRepositorySmoke exercises all four production queries
// against a real database, proving the SQL and scan helpers line up.
func TestProductionOrderRepositorySmoke(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed the output batch (RESERVED, not yet sellable) and an input batch.
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	var outputBatchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status, expiration_date)
		VALUES ($1, 'OUT-1', 5, 5, 'RESERVED', $2)
		RETURNING id`, outProduct.ID, exp).Scan(&outputBatchID); err != nil {
		t.Fatalf("seed output batch: %v", err)
	}

	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	var inputBatchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status, expiration_date)
		VALUES ($1, 'IN-1', 10, 10, 'ACTIVE', $2)
		RETURNING id`, inProduct.ID, exp).Scan(&inputBatchID); err != nil {
		t.Fatalf("seed input batch: %v", err)
	}

	repo := &repositories.ProductionOrderRepository{}

	order, err := repo.CreateProductionOrder(ctx, pool, repositories.CreateProductionOrderParams{
		OutputBatchID: outputBatchID,
		CreatedBy:     user.ID,
	})
	if err != nil {
		t.Fatalf("CreateProductionOrder: %v", err)
	}
	if order.Status != models.ProductionOrderStatusTypePlanned {
		t.Errorf("status = %s, want PLANNED", order.Status)
	}
	if order.CreatedAt.IsZero() {
		t.Error("created_at is zero, want a timestamp")
	}

	item, err := repo.CreateProductionOrderLineItem(ctx, pool, repositories.CreateProductionOrderLineItemParams{
		ProductionOrderID: order.ID,
		InputBatchID:      inputBatchID,
		QuantityConsumed:  "2.0000",
	})
	if err != nil {
		t.Fatalf("CreateProductionOrderLineItem: %v", err)
	}
	if item.InputBatchID != inputBatchID {
		t.Errorf("input_batch_id = %v, want %v", item.InputBatchID, inputBatchID)
	}

	locked, err := repo.GetProductionOrderByIDForUpdate(ctx, pool, order.ID)
	if err != nil {
		t.Fatalf("GetProductionOrderByIDForUpdate: %v", err)
	}
	if locked.ID != order.ID {
		t.Errorf("locked id = %v, want %v", locked.ID, order.ID)
	}

	started, err := repo.UpdateProductionOrderStatus(ctx, pool, order.ID, models.ProductionOrderStatusTypeInProgress)
	if err != nil {
		t.Fatalf("UpdateProductionOrderStatus: %v", err)
	}
	if started.Status != models.ProductionOrderStatusTypeInProgress {
		t.Errorf("status = %s, want IN_PROGRESS", started.Status)
	}
}
