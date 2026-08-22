// Package services for inventory.
package services

import (
	"context"
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors that router maps to HTTP status codes.
var (
	ErrInsufficientStock = errors.New("insufficient stock")
)

// InventoryService owns the business logic for stock adjustments.
type InventoryService struct {
	pool              *pgxpool.Pool
	batches           *repositories.BatchRepository
	stockTransactions *repositories.StockTransactionRepository
}

// NewInventoryService creates a new InventoryService.
func NewInventoryService(pool *pgxpool.Pool) *InventoryService {
	return &InventoryService{
		pool:              pool,
		batches:           &repositories.BatchRepository{},
		stockTransactions: &repositories.StockTransactionRepository{},
	}
}

// Adjust records a WASTE or ADJUSTMENT stock movement.
func (s *InventoryService) Adjust(ctx context.Context, req schemas.AdjustStockRequest) (models.StockTransaction, error) {
	batchID, err := uuid.Parse(req.BatchID)
	if err != nil {
		return models.StockTransaction{}, ErrInvalidRequest
	}
	delta, err := strconv.ParseFloat(req.QuantityChange, 64)
	if err != nil {
		return models.StockTransaction{}, ErrInvalidRequest
	}

	performedByRaw := middleware.UserIDFromContext(ctx)
	performedBy, err := uuid.Parse(performedByRaw)
	if err != nil {
		return models.StockTransaction{}, ErrInvalidRequest
	}

	if req.TransactionType == string(models.TransactionTypeWaste) && delta >= 0 {
		return models.StockTransaction{}, ErrInvalidRequest
	}

	var transaction models.StockTransaction
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		batch, getErr := s.batches.GetBatchByIDForUpdate(ctx, tx, batchID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrBatchNotFound
			}
			return fmt.Errorf("get batch: %w", getErr)
		}

		qty, getErr := batch.QuantityCurrent.Float64Value()
		if getErr != nil {
			return fmt.Errorf("parse current quantity: %w", getErr)
		}
		if qty.Float64+delta < 0 {
			return ErrInsufficientStock
		}

		_, getErr = s.batches.UpdateBatchQuantity(ctx, tx, batchID, req.QuantityChange)
		if getErr != nil {
			return fmt.Errorf("update batch quantity: %w", getErr)
		}

		transaction, getErr = s.stockTransactions.CreateStockTransaction(ctx, tx, repositories.CreateStockTransactionParams{
			BatchID:         batchID,
			QuantityChange:  req.QuantityChange,
			TransactionType: models.TransactionType(req.TransactionType),
			PerformedBy:     performedBy,
			ReferenceNote:   req.ReferenceNote,
		})
		if getErr != nil {
			return fmt.Errorf("create stock transaction: %w", getErr)
		}
		return nil
	})
	if err != nil {
		return models.StockTransaction{}, err
	}
	return transaction, nil
}
