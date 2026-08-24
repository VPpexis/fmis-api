// Package services for production orders.
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedProduction creates a FINISHED_GOOD output product, a WHITE_LABEL input
// product with an ACTIVE batch, and a PLANNED production order via the service.
func seedProduction(t *testing.T, pool *pgxpool.Pool) (*ProductionOrderService, models.User, models.ProductionOrder, []models.ProductionOrderLineItem, models.InventoryBatch) {
	t.Helper()
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	inputBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      inProduct.ID.String(),
		BatchNumber:    "LOT-INPUT",
		Quantity:       "10.0000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed input batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	order, items, err := svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: inputBatch.ID.String(), QuantityConsumed: "2.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-OUTPUT",
		OutputQuantity:       "2.0000",
		OutputExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed production order: %v", err)
	}
	return svc, user, order, items, inputBatch
}

// TestCreateProductionOrder covers the happy path: the order is PLANNED, the
// output batch is created RESERVED (not sellable), and line items persist.
func TestCreateProductionOrder(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	_, user, order, items, inputBatch := seedProduction(t, pool)
	ctx := context.Background()

	if order.Status != models.ProductionOrderStatusTypePlanned {
		t.Errorf("status = %s, want PLANNED", order.Status)
	}
	if order.CreatedBy != user.ID {
		t.Errorf("created_by = %v, want %v", order.CreatedBy, user.ID)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].InputBatchID != inputBatch.ID {
		t.Errorf("item input_batch_id = %v, want %v", items[0].InputBatchID, inputBatch.ID)
	}
	if items[0].ProductionOrderID != order.ID {
		t.Errorf("item order id = %v, want %v", items[0].ProductionOrderID, order.ID)
	}

	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM inventory_batches WHERE id = $1`,
		order.OutputBatchID).Scan(&status); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "RESERVED" {
		t.Errorf("output batch status = %s, want RESERVED", status)
	}

	var lineItemCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM production_order_line_items WHERE production_order_id = $1`,
		order.ID).Scan(&lineItemCount); err != nil {
		t.Fatalf("count line items: %v", err)
	}
	if lineItemCount != 1 {
		t.Errorf("line item rows = %d, want 1", lineItemCount)
	}

	var consumedCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM stock_transactions WHERE transaction_type = 'USED_IN_PRODUCTION'`,
		).Scan(&consumedCount); err != nil {
		t.Fatalf("count consumption transactions: %v", err)
	}
	if consumedCount != 0 {
		t.Errorf("USED_IN_PRODUCTION rows = %d, want 0 (consumption happens on complete)", consumedCount)
	}
}

// TestCreateRejectsNonFinishedGoodOutput covers the acceptance criterion that
// the output product must be FINISHED_GOOD.
func TestCreateRejectsNonFinishedGoodOutput(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	raw := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	svc := NewProductionOrderService(pool)

	_, _, err := svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: uuid.New().String(), QuantityConsumed: "1.0000"},
		},
		OutputProductID:     raw.ID.String(),
		OutputBatchNumber:   "B-BAD",
		OutputQuantity:      "1.0000",
	})
	if !errors.Is(err, ErrInvalidProductionInput) {
		t.Errorf("Create() error = %v, want ErrInvalidProductionInput", err)
	}
}

// TestCreateRejectsUnknownInputBatchAndRollsBack proves atomicity: a bad line
// item must roll back every write, including the output batch.
func TestCreateRejectsUnknownInputBatchAndRollsBack(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	inputBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      inProduct.ID.String(),
		BatchNumber:    "LOT-OK",
		Quantity:       "10.0000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed input batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	_, _, err = svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: inputBatch.ID.String(), QuantityConsumed: "2.0000"},
			{InputBatchID: uuid.New().String(), QuantityConsumed: "1.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-ROLLBACK",
		OutputQuantity:       "3.0000",
		OutputExpirationDate: &exp,
	})
	if !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("Create() error = %v, want ErrBatchNotFound", err)
	}

	var orders, batches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM production_orders`).Scan(&orders); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orders != 0 {
		t.Errorf("production order rows = %d, want 0 (transaction must roll back)", orders)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_batches`).Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batches != 1 {
		t.Errorf("batch rows = %d, want 1 (output batch must roll back)", batches)
	}
}

// TestCreateRejectsInvalidInputProductType covers the design rule that inputs
// must be WHITE_LABEL or PACKAGING products.
func TestCreateRejectsInvalidInputProductType(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	raw := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	rawBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      raw.ID.String(),
		BatchNumber:    "LOT-RAW",
		Quantity:       "5.0000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed raw material batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	_, _, err = svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: rawBatch.ID.String(), QuantityConsumed: "1.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-BADINPUT",
		OutputQuantity:       "1.0000",
		OutputExpirationDate: &exp,
	})
	if !errors.Is(err, ErrInvalidProductionInput) {
		t.Errorf("Create() error = %v, want ErrInvalidProductionInput", err)
	}
}

// TestStartTransitionsToInProgress covers the PLANNED -> IN_PROGRESS path.
func TestStartTransitionsToInProgress(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc, _, order, _, _ := seedProduction(t, pool)
	ctx := context.Background()

	started, err := svc.Start(ctx, order.ID.String())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != models.ProductionOrderStatusTypeInProgress {
		t.Errorf("status = %s, want IN_PROGRESS", started.Status)
	}
}

// TestStartRejectsInvalidTransition covers the acceptance criterion that
// invalid status transitions are rejected.
func TestStartRejectsInvalidTransition(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc, _, order, _, _ := seedProduction(t, pool)
	ctx := context.Background()

	if _, err := svc.Start(ctx, order.ID.String()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := svc.Start(ctx, order.ID.String()); !errors.Is(err, ErrInvalidProductionOrderState) {
		t.Errorf("second Start error = %v, want ErrInvalidProductionOrderState", err)
	}
}

// TestStartNotFound covers unknown and malformed order IDs.
func TestStartNotFound(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc := NewProductionOrderService(pool)
	ctx := context.Background()

	if _, err := svc.Start(ctx, uuid.New().String()); !errors.Is(err, ErrProductionOrderNotFound) {
		t.Errorf("unknown order error = %v, want ErrProductionOrderNotFound", err)
	}
	if _, err := svc.Start(ctx, "not-a-uuid"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("malformed id error = %v, want ErrInvalidRequest", err)
	}
}

// TestStartConcurrent proves the FOR UPDATE lock serializes simultaneous
// starts: exactly one transition from PLANNED succeeds, the other is rejected.
func TestStartConcurrent(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc, _, order, _, _ := seedProduction(t, pool)
	ctx := context.Background()

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.Start(ctx, order.ID.String())
			results <- err
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrInvalidProductionOrderState):
			conflicts++
		default:
			t.Errorf("Start error = %v, want nil or ErrInvalidProductionOrderState", err)
		}
	}
	if successes != 1 {
		t.Errorf("successful starts = %d, want exactly 1", successes)
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want exactly 1", conflicts)
	}
}
