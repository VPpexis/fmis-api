// Package services for batches.
package services

import (
	"context"
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"
	"fmis-api/internal/testutil"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// userContext wraps ctx with the user's ID, as Auth would for a real request.
func userContext(user *models.User) context.Context {
	return middleware.WithUserID(context.Background(), user.ID.String())
}

// ptr returns a pointer to v for nullable fields.
func ptr[T any](v T) *T { return &v }

// TestReceiveExpirationRule verifies the pre-product-type expiration requirement.
func TestReceiveExpirationRule(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	svc := NewBatchService(pool)

	tests := []struct {
		name        string
		productType models.ProductType
		expiration  *time.Time
		wantErr     error
	}{
		{"raw material requires expiration", models.ProductTypeRawMaterial, nil, ErrExpirationRequired},
		{"finished good requires expiration", models.ProductTypeFinishedGood, nil, ErrExpirationRequired},
		{"white label requires expiration", models.ProductTypeWhiteLabel, nil, ErrExpirationRequired},
		{"packaging allows nil expiraton", models.ProductTypePackaging, nil, nil},
		{"raw material with expiration accepted", models.ProductTypeRawMaterial,
			ptr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			product := testutil.CreateProduct(ctx, t, pool, tt.productType)
			_, err := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
				ProductID:      product.ID.String(),
				BatchNumber:    "LOT-" + uuid.NewString()[:8],
				Quantity:       "100.0000",
				ExpirationDate: tt.expiration,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Receive() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestReceiveWritesIncomingTransaction verifies batch + INCOMING tx commit together.
func TestReceiveWritesIncomingTransaction(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	svc := NewBatchService(pool)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	batch, err := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      product.ID.String(),
		BatchNumber:    "LOT-0001",
		Quantity:       "12.5000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if batch.Status != models.BatchStatusTypeActive {
		t.Errorf("status = %s, want ACTIVE", batch.Status)
	}

	var qty pgtype.Numeric
	var performedBy uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT quantity_change, performed_by
		FROM stock_transactions
		WHERE batch_id = $1 AND transaction_type = 'INCOMING'`,
		batch.ID).Scan(&qty, &performedBy)
	if err != nil {
		t.Fatalf("query INCOMING transaction: %v", err)
	}
	q, err := qty.Float64Value()
	if err != nil || !q.Valid {
		t.Fatalf("read quantity_change: %v", err)
	}
	if q.Float64 != 12.5 {
		t.Errorf("quantity_change = %v, want 12.5", q.Float64)
	}
	if performedBy != user.ID {
		t.Errorf("performed_by = %v, want %v", performedBy, user.ID)
	}
}

// TestReceiveRollBacksOnFailedTransactions proves the two inserts share
// one transaction: a failed INCOMING insert must not leave an orphan batch.
func TestReceiveRollsBackOnFailedTransaction(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	svc := NewBatchService(pool)

	ghost := uuid.New()
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.Receive(middleware.WithUserID(context.Background(), ghost.String()), schemas.CreateBatchRequest{
		ProductID:      product.ID.String(),
		BatchNumber:    "LOT-GHOST",
		Quantity:       "5.0000",
		ExpirationDate: &exp,
	})
	if err == nil {
		t.Fatal("Receive() should fail when performed_by references no user")
	}

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM inventory_batches`).Scan(&count); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if count != 0 {
		t.Errorf("batch rows = %d, want 0 (transaction must roll back)", count)
	}
}

// TestListActiveByProductFEFO verfies FEFO ordering and ACTIVE filtering -
// the acceptance criterion for this issue.
func TestListActiveByProductFEFO(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypePackaging)
	svc := NewBatchService(pool)

	late := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	early := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	veryLate := time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, exp := range []*time.Time{&late, nil, &early} {
		if _, err := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
			ProductID:      product.ID.String(),
			BatchNumber:    "LOT-" + uuid.NewString()[:8],
			Quantity:       "50.0000",
			ExpirationDate: exp,
		}); err != nil {
			t.Fatalf("Receive: %v", err)
		}
	}

	quarantined, err := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      product.ID.String(),
		BatchNumber:    "LOT-QUARANTINED",
		Quantity:       "10.0000",
		ExpirationDate: &veryLate,
	})
	if err != nil {
		t.Fatalf("Receive quarantined batch: %v", err)
	}
	if _, err2 := svc.Quarantine(ctx, quarantined.ID.String()); err2 != nil {
		t.Fatalf("Quarantine: %v", err2)
	}

	other := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)
	if _, err3 := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      other.ID.String(),
		BatchNumber:    "LOT-OTHER",
		Quantity:       "1.0000",
		ExpirationDate: &early,
	}); err3 != nil {
		t.Fatalf("Receive other products: %v", err)
	}

	batches, err := svc.ListActiveByProduct(ctx, product.ID.String())
	if err != nil {
		t.Fatalf("ListActiveByProduct: %v", err)
	}
	if len(batches) != 3 {
		t.Fatalf("len = %d, want 3 (excludes quarantined and other product)", len(batches))
	}
	if !batches[0].ExpirationDate.Time.Equal(early) {
		t.Fatalf("first batch expires %v, want %v (soonest first)", batches[0].ExpirationDate.Time, early)
	}
	if !batches[1].ExpirationDate.Time.Equal(late) {
		t.Errorf("second batch expires %v, want %v", batches[1].ExpirationDate.Time, late)
	}
	if batches[2].ExpirationDate.Valid {
		t.Error("Third batch must have NULL expiration (NULL LAST)")
	}
}

// Test Quarantine covers the state guard and not-found cases.
func TestQuarantine(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypePackaging)
	svc := NewBatchService(pool)

	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	batch, err := svc.Receive(userContext(&user), schemas.CreateBatchRequest{
		ProductID:      product.ID.String(),
		BatchNumber:    "LOT-1",
		Quantity:       "9.0000",
		ExpirationDate: &exp,
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	// Step 1: ACTIVE batch can be quarantined.
	quarantined, err := svc.Quarantine(ctx, batch.ID.String())
	if err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	if quarantined.Status != models.BatchStatusTypeQuarantined {
		t.Errorf("status = %s, want QUARANTINED", quarantined.Status)
	}

	// Step 2: already quarantined (not ACTIVE) -> rejected.
	if _, err := svc.Quarantine(ctx, batch.ID.String()); !errors.Is(err, ErrInvalidBatchState) {
		t.Errorf("second Quarantine error = %v, want ErrInvalidBatchState", err)
	}

	// Step 3: unknown batch -> not found.
	if _, err := svc.Quarantine(ctx, uuid.New().String()); !errors.Is(err, ErrBatchNotFound) {
		t.Errorf("unknown batch error = %v, want ErrBatchNotFound", err)
	}
}

// TestDBConstraintsRejectInvalidQuantities checks that the DB rejects invalid quantities.
func TestDBConstraintsRejectInvalidQuantities(t *testing.T) {
	pool := testutil.Pool(t)
	testutil.ResetDB(t, pool)
	ctx := context.Background()
	user := testutil.CreateUser(ctx, t, pool, models.UserRoleTypeOperator)
	product := testutil.CreateProduct(ctx, t, pool, models.ProductTypeRawMaterial)

	// Seed one legit batch — its ID feeds the stock_transactions cases.
	var batchID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status)
		VALUES ($1, 'LOT-OK', 100, 100, 'ACTIVE')
		RETURNING id`, product.ID).Scan(&batchID)
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{"zero initial",
			`INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status) VALUES ($1, 'LOT-Z', 0, 0, 'ACTIVE')`,
			[]any{product.ID}},
		{"negative current",
			`INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status) VALUES ($1, 'LOT-N', 10, -1, 'ACTIVE')`,
			[]any{product.ID}},
		{"negative incoming",
			`INSERT INTO stock_transactions (batch_id, quantity_change, transaction_type, performed_by) VALUES ($1, -5, 'INCOMING', $2)`,
			[]any{batchID, user.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.sql, tt.args...)
			if err == nil {
				t.Fatal("insert succeeded, want check constraint violation")
			}
			if !strings.Contains(err.Error(), "violates check constraint") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	// Negative quantity with OUTGOING must still be allowed (the OR logic).
	if _, err := pool.Exec(ctx,
		`INSERT INTO stock_transactions (batch_id, quantity_change, transaction_type, performed_by) VALUES ($1, -5, 'OUTGOING', $2)`,
		batchID, user.ID); err != nil {
		t.Errorf("OUTGOING negative insert failed: %v", err)
	}
}
