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
	"github.com/jackc/pgx/v5/pgtype"
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
		OutputProductID:   raw.ID.String(),
		OutputBatchNumber: "B-BAD",
		OutputQuantity:    "1.0000",
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

// completeFixture carries everything a completion test needs to assert on.
type completeFixture struct {
	svc   *ProductionOrderService
	user  models.User
	order models.ProductionOrder
	early models.InventoryBatch
	late  models.InventoryBatch
	pkg   models.InventoryBatch
}

// seedCompleteFixture creates a FINISHED_GOOD output product, two WHITE_LABEL
// input batches with different expiration dates plus a PACKAGING input batch
// without one, and an IN_PROGRESS production order via the service.
func seedCompleteFixture(t *testing.T, pool *pgxpool.Pool) *completeFixture {
	t.Helper()
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	whiteProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)
	pkgProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypePackaging)

	early := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	earlyBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      whiteProduct.ID.String(),
		BatchNumber:    "LOT-WHITE-EARLY",
		Quantity:       "10.0000",
		ExpirationDate: &early,
	})
	if err != nil {
		t.Fatalf("seed early white label batch: %v", err)
	}
	lateBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      whiteProduct.ID.String(),
		BatchNumber:    "LOT-WHITE-LATE",
		Quantity:       "10.0000",
		ExpirationDate: &late,
	})
	if err != nil {
		t.Fatalf("seed late white label batch: %v", err)
	}
	pkgBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:   pkgProduct.ID.String(),
		BatchNumber: "LOT-PKG",
		Quantity:    "5.0000",
	})
	if err != nil {
		t.Fatalf("seed packaging batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	order, _, err := svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: earlyBatch.ID.String(), QuantityConsumed: "2.0000"},
			{InputBatchID: lateBatch.ID.String(), QuantityConsumed: "2.0000"},
			{InputBatchID: pkgBatch.ID.String(), QuantityConsumed: "2.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-OUTPUT",
		OutputQuantity:       "2.0000",
		OutputExpirationDate: &late,
	})
	if err != nil {
		t.Fatalf("seed production order: %v", err)
	}

	started, err := svc.Start(ctx, order.ID.String())
	if err != nil {
		t.Fatalf("seed start production order: %v", err)
	}
	return &completeFixture{svc: svc, user: user, order: started, early: earlyBatch, late: lateBatch, pkg: pkgBatch}
}

// currentQuantity reads a batch's current quantity straight from the database.
func currentQuantity(t *testing.T, pool *pgxpool.Pool, batchID uuid.UUID) float64 {
	t.Helper()
	var q float64
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity_current FROM inventory_batches WHERE id = $1`, batchID).Scan(&q); err != nil {
		t.Fatalf("query batch %s quantity: %v", batchID, err)
	}
	return q
}

// TestCompleteHappyPath covers the full completion: quantities are deducted,
// the output batch is activated with the MIN expiration, the order is
// COMPLETED with completed_at, and every transaction is linked to the order.
func TestCompleteHappyPath(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	f := seedCompleteFixture(t, pool)
	ctx := userContext(&f.user)

	completed, _, err := f.svc.Complete(ctx, f.order.ID.String())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != models.ProductionOrderStatusTypeCompleted {
		t.Errorf("status = %s, want COMPLETED", completed.Status)
	}
	if !completed.CompletedAt.Valid {
		t.Error("completed_at is NULL, want a timestamp")
	}

	if got := currentQuantity(t, pool, f.early.ID); got != 8 {
		t.Errorf("early batch quantity = %v, want 8", got)
	}
	if got := currentQuantity(t, pool, f.late.ID); got != 8 {
		t.Errorf("late batch quantity = %v, want 8", got)
	}
	if got := currentQuantity(t, pool, f.pkg.ID); got != 3 {
		t.Errorf("packaging batch quantity = %v, want 3", got)
	}

	var status string
	var outputExp pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `
		SELECT status, expiration_date FROM inventory_batches WHERE id = $1`,
		f.order.OutputBatchID).Scan(&status, &outputExp); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "ACTIVE" {
		t.Errorf("output batch status = %s, want ACTIVE", status)
	}
	wantExp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !outputExp.Valid {
		t.Error("output expiration is NULL, want a timestamp")
	} else if !outputExp.Time.Equal(wantExp) {
		t.Errorf("output expiration = %v, want %v (MIN of inputs)", outputExp.Time, wantExp)
	}

	var txCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM stock_transactions
		WHERE production_order_id = $1 AND transaction_type = 'USED_IN_PRODUCTION'`,
		f.order.ID).Scan(&txCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount != 3 {
		t.Errorf("linked USED_IN_PRODUCTION rows = %d, want 3", txCount)
	}
}

// TestCompleteRejectsPlannedOrder covers the acceptance criterion that only
// IN_PROGRESS orders can be completed.
func TestCompleteRejectsPlannedOrder(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc, user, order, _, _ := seedProduction(t, pool)
	ctx := userContext(&user)

	if _, _, err := svc.Complete(ctx, order.ID.String()); !errors.Is(err, ErrInvalidProductionOrderState) {
		t.Errorf("Complete on PLANNED order error = %v, want ErrInvalidProductionOrderState", err)
	}
}

// TestCompleteDuplicateRejected covers the idempotency rule: a second complete
// call is rejected and quantities are not consumed twice.
func TestCompleteDuplicateRejected(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	f := seedCompleteFixture(t, pool)
	ctx := userContext(&f.user)

	if _, _, err := f.svc.Complete(ctx, f.order.ID.String()); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if _, _, err := f.svc.Complete(ctx, f.order.ID.String()); !errors.Is(err, ErrInvalidProductionOrderState) {
		t.Errorf("second Complete error = %v, want ErrInvalidProductionOrderState", err)
	}
	if got := currentQuantity(t, pool, f.early.ID); got != 8 {
		t.Errorf("early batch quantity = %v, want 8 (no double consumption)", got)
	}
}

// TestCompleteNotFoundAndMalformedID covers unknown and malformed order IDs.
func TestCompleteNotFoundAndMalformedID(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	svc := NewProductionOrderService(pool)
	user := testutil.CreateUser(context.Background(), t, pool, models.UserRoleTypeOperator)
	ctx := userContext(&user)

	if _, _, err := svc.Complete(ctx, uuid.New().String()); !errors.Is(err, ErrProductionOrderNotFound) {
		t.Errorf("unknown order error = %v, want ErrProductionOrderNotFound", err)
	}
	if _, _, err := svc.Complete(ctx, "not-a-uuid"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("malformed id error = %v, want ErrInvalidRequest", err)
	}
}

// TestCompleteInsufficientStockRollsBack proves atomicity: when an input batch
// cannot cover its line item, every write from the completion is rolled back.
func TestCompleteInsufficientStockRollsBack(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	inProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeWhiteLabel)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	inputBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      inProduct.ID.String(),
		BatchNumber:    "LOT-THIN",
		Quantity:       "10.0000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed input batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	order, _, err := svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: inputBatch.ID.String(), QuantityConsumed: "20.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-THIN",
		OutputQuantity:       "2.0000",
		OutputExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("seed production order: %v", err)
	}
	if _, err := svc.Start(ctx, order.ID.String()); err != nil {
		t.Fatalf("seed start: %v", err)
	}

	if _, _, err := svc.Complete(userContext(&user), order.ID.String()); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("Complete error = %v, want ErrInsufficientStock", err)
	}

	if got := currentQuantity(t, pool, inputBatch.ID); got != 10 {
		t.Errorf("input batch quantity = %v, want 10 (must not be deducted)", got)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_batches WHERE id = $1`, order.OutputBatchID).Scan(&status); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if status != "RESERVED" {
		t.Errorf("output batch status = %s, want RESERVED (must roll back)", status)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM production_orders WHERE id = $1`, order.ID).Scan(&status); err != nil {
		t.Fatalf("query order: %v", err)
	}
	if status != "IN_PROGRESS" {
		t.Errorf("order status = %s, want IN_PROGRESS (must roll back)", status)
	}
	var txCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stock_transactions WHERE transaction_type = 'USED_IN_PRODUCTION'`).Scan(&txCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount != 0 {
		t.Errorf("USED_IN_PRODUCTION rows = %d, want 0 (must roll back)", txCount)
	}
}

// TestCompleteConcurrent proves the FOR UPDATE locks serialize simultaneous
// completes: exactly one succeeds, the other is rejected, and the input
// batches are consumed exactly once.
func TestCompleteConcurrent(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	f := seedCompleteFixture(t, pool)
	ctx := userContext(&f.user)

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, _, err := f.svc.Complete(ctx, f.order.ID.String())
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
			t.Errorf("Complete error = %v, want nil or ErrInvalidProductionOrderState", err)
		}
	}
	if successes != 1 {
		t.Errorf("successful completes = %d, want exactly 1", successes)
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want exactly 1", conflicts)
	}
	if got := currentQuantity(t, pool, f.early.ID); got != 8 {
		t.Errorf("early batch quantity = %v, want 8 (must be consumed exactly once)", got)
	}
	var completedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM production_orders WHERE status = 'COMPLETED'`).Scan(&completedCount); err != nil {
		t.Fatalf("count completed orders: %v", err)
	}
	if completedCount != 1 {
		t.Errorf("completed orders = %d, want 1", completedCount)
	}
}

// TestCompleteAllNullExpiration covers the design rule that when every input
// batch has no expiration date, the output batch ends up with NULL too.
func TestCompleteAllNullExpiration(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	outProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypeFinishedGood)
	pkgProduct := testutil.CreateProduct(ctx, t, pool, models.ProductTypePackaging)

	seedExp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	pkgBatch, err := NewBatchService(pool).Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:   pkgProduct.ID.String(),
		BatchNumber: "LOT-PKG-NULL",
		Quantity:    "5.0000",
	})
	if err != nil {
		t.Fatalf("seed packaging batch: %v", err)
	}

	svc := NewProductionOrderService(pool)
	order, _, err := svc.Create(userContext(&user), &schemas.CreateProductionOrderRequest{
		LineItems: []schemas.ProductionLineItemRequest{
			{InputBatchID: pkgBatch.ID.String(), QuantityConsumed: "2.0000"},
		},
		OutputProductID:      outProduct.ID.String(),
		OutputBatchNumber:    "B-NULLEXP",
		OutputQuantity:       "2.0000",
		OutputExpirationDate: &seedExp,
	})
	if err != nil {
		t.Fatalf("seed production order: %v", err)
	}
	if _, err := svc.Start(ctx, order.ID.String()); err != nil {
		t.Fatalf("seed start: %v", err)
	}

	if _, _, err := svc.Complete(userContext(&user), order.ID.String()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var outputExp pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT expiration_date FROM inventory_batches WHERE id = $1`, order.OutputBatchID).Scan(&outputExp); err != nil {
		t.Fatalf("query output batch: %v", err)
	}
	if outputExp.Valid {
		t.Errorf("output expiration = %v, want NULL (all inputs have none)", outputExp.Time)
	}
}
