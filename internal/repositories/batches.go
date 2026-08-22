// Package repositories excutes parameterized SQL queries against PostgreSQL.
package repositories

import (
	"context"
	"fmis-api/internal/models"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateBatchParams carries the values needed to insert a new inventory batch row.
type CreateBatchParams struct {
	ProductID      uuid.UUID
	BatchNumber    string
	Quantity       string
	ExpirationDate *time.Time
}

// BatchRepository reads and writes inventory_batches and stock_transactions.
type BatchRepository struct{}

// CreateBatch inserts a new ACTIVE batch and returns the stored row.
func (r *BatchRepository) CreateBatch(ctx context.Context, q Querier, p CreateBatchParams) (models.InventoryBatch, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO inventory_batches (product_id, batch_number, quantity_initial, quantity_current, status, expiration_date)
		VALUES ($1, $2, $3, $3, 'ACTIVE', $4)
		RETURNING id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at`,
		p.ProductID, p.BatchNumber, p.Quantity, p.ExpirationDate)
	return scanBatch(row)
}

// GetBatchByID fetches a batch by its primary key.
func (r *BatchRepository) GetBatchByID(ctx context.Context, q Querier, id uuid.UUID) (models.InventoryBatch, error) {
	row := q.QueryRow(ctx, `
		SELECT id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at
		FROM inventory_batches
		WHERE id = $1`,
		id)
	return scanBatch(row)
}

// GetBatchByIDForUpdate fetches a batch by its primary key and locks the row
// until the surrounding transaction commits, serializing concurrent transitions.
func (r *BatchRepository) GetBatchByIDForUpdate(ctx context.Context, q Querier, id uuid.UUID) (models.InventoryBatch, error) {
	row := q.QueryRow(ctx, `
		SELECT id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at
		FROM inventory_batches
		WHERE id = $1
		FOR UPDATE`,
		id)
	return scanBatch(row)
}

// GetActivateBatchesByProduct lists ACTIVE batches for a product in FEFO order:
func (r *BatchRepository) GetActivateBatchesByProduct(ctx context.Context, q Querier, productID uuid.UUID) ([]models.InventoryBatch, error) {
	rows, err := q.Query(ctx, `
		SELECT id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at
		FROM inventory_batches
		WHERE product_id = $1 AND status = 'ACTIVE'
		ORDER BY expiration_date ASC NULLS LAST`,
		productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	batches := make([]models.InventoryBatch, 0)
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, rows.Err()
}

// SetBatchStatus updates the status of a batch.
func (r *BatchRepository) SetBatchStatus(ctx context.Context, q Querier, batchID uuid.UUID, status models.BatchStatusType) (models.InventoryBatch, error) {
	row := q.QueryRow(ctx, `
		UPDATE inventory_batches SET status = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at`,
		batchID, status)
	return scanBatch(row)
}

// UpdateBatchQuantity updates the quantity of the batch.
func (r *BatchRepository) UpdateBatchQuantity(ctx context.Context, q Querier, batchID uuid.UUID, delta string) (models.InventoryBatch, error) {
	row := q.QueryRow(ctx, `
		UPDATE inventory_batches SET quantity_current = quantity_current + $2, updated_at = now()
		WHERE id = $1
		RETURNING id, product_id, batch_number, quantity_initial, quantity_current, status, expiration_date, created_at, updated_at`,
		batchID, delta)
	return scanBatch(row)
}

// CountActivateBatches counts the number of active batches for a product.
func (r *BatchRepository) CountActivateBatches(ctx context.Context, q Querier, id uuid.UUID) (int, error) {
	var count int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM inventory_batches
		WHERE product_id = $1 AND status = 'ACTIVE'`,
		id,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// scanBatch maps one inventory_batches row into a models.
func scanBatch(row pgx.Row) (models.InventoryBatch, error) {
	var b models.InventoryBatch
	err := row.Scan(
		&b.ID, &b.ProductID, &b.BatchNumber,
		&b.QuantityInitial, &b.QuantityCurrent, &b.Status, &b.ExpirationDate,
		&b.CreatedAt, &b.UpdatedAt,
	)
	return b, err
}
