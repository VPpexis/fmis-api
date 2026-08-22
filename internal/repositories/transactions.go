package repositories

import (
	"context"
	"fmis-api/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateStockTransactionParams caries the values needed to insert a stock movement row.
type CreateStockTransactionParams struct {
	BatchID         uuid.UUID
	QuantityChange  string
	TransactionType models.TransactionType
	PerformedBy     uuid.UUID
	ReferenceNote   *string
}

// StockTransactionRepository reads and writes stock_transactions audit rows.
type StockTransactionRepository struct{}

// CreateStockTransaction create a stock_transaction.
func (r *StockTransactionRepository) CreateStockTransaction(ctx context.Context, q Querier, p CreateStockTransactionParams) (models.StockTransaction, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO stock_transactions (batch_id, quantity_change, transaction_type, performed_by, reference_note)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, batch_id, production_order_id, quantity_change, transaction_type, performed_by, reference_note, created_at`,
		p.BatchID, p.QuantityChange, p.TransactionType, p.PerformedBy, p.ReferenceNote)
	return scanStockTransaction(row)
}

// scanStockTransaction maps one stock_transaction row into a models.
func scanStockTransaction(row pgx.Row) (models.StockTransaction, error) {
	var t models.StockTransaction
	err := row.Scan(
		&t.ID, &t.BatchID, &t.ProductionOrderID, &t.QuantityChange, &t.TransactionType,
		&t.PerformedBy, &t.ReferenceNote, &t.CreatedAt,
	)
	return t, err
}
