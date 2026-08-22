package repositories

import (
	"context"
	"fmis-api/internal/models"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateStockTransactionParams carries the values needed to insert a stock movement row.
type CreateStockTransactionParams struct {
	BatchID         uuid.UUID
	QuantityChange  string
	TransactionType models.TransactionType
	PerformedBy     uuid.UUID
	ReferenceNote   *string
}

// ListStockTransactionParams carries the values needed to list stock transactions with pagination.
type ListStockTransactionParams struct {
	BatchID         *uuid.UUID
	TransactionType *models.TransactionType
	DateFrom        *time.Time
	DateTo          *time.Time
	Limit           int
	Offset          int
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

// ListStockTransaction list stock transactions with pagination
func (r *StockTransactionRepository) ListStockTransaction(ctx context.Context, q Querier, p ListStockTransactionParams) ([]models.StockTransaction, error) {
	query := `
		SELECT id, batch_id, production_order_id, quantity_change, transaction_type, performed_by, reference_note, created_at
		FROM stock_transactions
		WHERE 1=1
	`
	args := []any{}

	if p.BatchID != nil {
		args = append(args, *p.BatchID)
		query += fmt.Sprintf(" AND batch_id = $%d", len(args))
	}
	if p.TransactionType != nil {
		args = append(args, *p.TransactionType)
		query += fmt.Sprintf(" AND transaction_type = $%d", len(args))
	}
	if p.DateFrom != nil {
		args = append(args, *p.DateFrom)
		query += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if p.DateTo != nil {
		args = append(args, *p.DateTo)
		query += fmt.Sprintf(" AND created_at <= $%d", len(args))
	}

	args = append(args, p.Limit, p.Offset)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := make([]models.StockTransaction, 0)
	for rows.Next() {
		t, err := scanStockTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, t)
	}
	return transactions, rows.Err()
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
