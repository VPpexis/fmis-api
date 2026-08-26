// Package repositories for production.
package repositories

import (
	"context"
	"fmis-api/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateProductionOrderParams carries the values needed to insert a new production_order.
type CreateProductionOrderParams struct {
	OutputBatchID uuid.UUID
	CreatedBy     uuid.UUID
}

// CreateProductionOrderLineItemParams carries the values needed to insert a new production_order_line_item.
type CreateProductionOrderLineItemParams struct {
	ProductionOrderID uuid.UUID
	InputBatchID      uuid.UUID
	QuantityConsumed  string
}

// ListProductionOrderParams is the payload for GET /api/v1/production.
type ListProductionOrderParams struct {
	Status *models.ProductionOrderStatusType
	Limit  int
	Offset int
}

// ProductionOrderRepository reads and writes production_orders.
type ProductionOrderRepository struct{}

// CreateProductionOrder inserts a new PLANNED production order and retunrs the stored row.
func (r *ProductionOrderRepository) CreateProductionOrder(ctx context.Context, q Querier, p CreateProductionOrderParams) (models.ProductionOrder, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO production_orders (output_batch_id, status, created_by)
		VALUES ($1, 'PLANNED', $2)
		RETURNING id, output_batch_id, status, created_by, created_at, completed_at`,
		p.OutputBatchID, p.CreatedBy)
	return scanProductionOrder(row)
}

// CreateProductionOrderLineItem inserts a new production order line item and returns the stored row.
func (r *ProductionOrderRepository) CreateProductionOrderLineItem(ctx context.Context, q Querier, p CreateProductionOrderLineItemParams) (models.ProductionOrderLineItem, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO production_order_line_items (production_order_id, input_batch_id, quantity_consumed)
		VALUES ($1, $2, $3)
		RETURNING id, production_order_id, input_batch_id, quantity_consumed, created_at`,
		p.ProductionOrderID, p.InputBatchID, p.QuantityConsumed)
	return scanProductionOrderLineItem(row)
}

// GetProductionOrderLineItemsByOrderID fetch all line items for production order.
func (r *ProductionOrderRepository) GetProductionOrderLineItemsByOrderID(ctx context.Context, q Querier, orderID uuid.UUID) ([]models.ProductionOrderLineItem, error) {
	rows, err := q.Query(ctx, `
		SELECT id, production_order_id, input_batch_id, quantity_consumed, created_at
		FROM production_order_line_items
		WHERE production_order_id = $1`,
		orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	productionOrderLineItems := make([]models.ProductionOrderLineItem, 0)
	for rows.Next() {
		p, err := scanProductionOrderLineItem(rows)
		if err != nil {
			return nil, err
		}
		productionOrderLineItems = append(productionOrderLineItems, p)
	}
	return productionOrderLineItems, rows.Err()
}

// GetProductionOrderByIDForUpdate fetches a production order by its primary key and locks the row
// until the surrounding transaction commits.
func (r *ProductionOrderRepository) GetProductionOrderByIDForUpdate(ctx context.Context, q Querier, id uuid.UUID) (models.ProductionOrder, error) {
	row := q.QueryRow(ctx, `
		SELECT id, output_batch_id, status, created_by, created_at, completed_at
		FROM production_orders
		WHERE id = $1
		FOR UPDATE`,
		id)
	return scanProductionOrder(row)
}

// GetProductionOrderByID fetches a production order by its primary key.
func (r *ProductionOrderRepository) GetProductionOrderByID(ctx context.Context, q Querier, id uuid.UUID) (models.ProductionOrder, error) {
	row := q.QueryRow(ctx, `
		SELECT id, output_batch_id, status, created_by, created_at, completed_at
		FROM production_orders
		WHERE id = $1`,
		id)
	return scanProductionOrder(row)
}

// ListProductionOrders returns all production orders with an optional status filter.
func (r *ProductionOrderRepository) ListProductionOrders(ctx context.Context, q Querier, p ListProductionOrderParams) ([]models.ProductionOrder, error) {
	rows, err := q.Query(ctx, `
		SELECT id, output_batch_id, status, created_by, created_at, completed_at
		FROM production_orders
		WHERE $1::production_order_status IS NULL OR status = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		p.Status, p.Limit, p.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	productionOrders := make([]models.ProductionOrder, 0)
	for rows.Next() {
		por, err := scanProductionOrder(rows)
		if err != nil {
			return nil, err
		}
		productionOrders = append(productionOrders, por)
	}
	return productionOrders, rows.Err()
}

// UpdateProductionOrderStatus updates the status of the production_order
func (r *ProductionOrderRepository) UpdateProductionOrderStatus(ctx context.Context, q Querier, orderID uuid.UUID, status models.ProductionOrderStatusType) (models.ProductionOrder, error) {
	row := q.QueryRow(ctx, `
		UPDATE production_orders
		SET status = $2,
			completed_at = CASE WHEN $3::text = 'COMPLETED' THEN now() ELSE completed_at END
		WHERE id = $1
		RETURNING id, output_batch_id, status, created_by, created_at, completed_at`,
		orderID, status, status)
	return scanProductionOrder(row)
}

// scanProductionOrderLineItem maps one production_order_line_item row into a models.
func scanProductionOrderLineItem(row pgx.Row) (models.ProductionOrderLineItem, error) {
	var p models.ProductionOrderLineItem
	err := row.Scan(
		&p.ID, &p.ProductionOrderID, &p.InputBatchID,
		&p.QuantityConsumed, &p.CreatedAt,
	)
	return p, err
}

// scanProductionOrder maps one production_order row into a models.
func scanProductionOrder(row pgx.Row) (models.ProductionOrder, error) {
	var p models.ProductionOrder
	err := row.Scan(
		&p.ID, &p.OutputBatchID, &p.Status, &p.CreatedBy,
		&p.CreatedAt, &p.CompletedAt,
	)
	return p, err
}
