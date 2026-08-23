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
	"math"
	"strconv"
	"time"

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

// List list transactions with pagination.
func (s *InventoryService) List(ctx context.Context, batchIDStr, typeStr, fromStr, toStr, limitStr, offsetStr string) ([]models.StockTransaction, error) {
	var batchID *uuid.UUID
	if batchIDStr != "" {
		parsed, err := uuid.Parse(batchIDStr)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		batchID = &parsed
	}

	var transactionType *models.TransactionType
	if typeStr != "" {
		tt := models.TransactionType(typeStr)
		switch tt {
		case models.TransactionTypeIncoming, models.TransactionTypeOutgoing,
			models.TransactionTypeUsedInProduction, models.TransactionTypeWaste,
			models.TransactionTypeAdjustment:
			transactionType = &tt
		default:
			return nil, ErrInvalidRequest
		}
	}

	var dateFrom, dateTo *time.Time
	if fromStr != "" {
		parsed, err := time.Parse(time.RFC3339Nano, fromStr)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		dateFrom = &parsed
	}
	if toStr != "" {
		parsed, err := time.Parse(time.RFC3339Nano, toStr)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		dateTo = &parsed
	}

	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed > 0 {
			offset = parsed
		}
	}

	transactions, err := s.stockTransactions.ListStockTransaction(ctx, s.pool, repositories.ListStockTransactionParams{
		BatchID:         batchID,
		TransactionType: transactionType,
		DateFrom:        dateFrom,
		DateTo:          dateTo,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	return transactions, nil
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

// Consume records a OUTGOING stock movement.
func (s *InventoryService) Consume(ctx context.Context, req schemas.ConsumeStockRequest) ([]models.StockTransaction, error) {
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	remaining, err := strconv.ParseFloat(req.Quantity, 64)
	if err != nil {
		return nil, ErrInvalidRequest
	}

	performedByRaw := middleware.UserIDFromContext(ctx)
	performedBy, err := uuid.Parse(performedByRaw)
	if err != nil {
		return nil, ErrInvalidRequest
	}

	var transactions []models.StockTransaction
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		batches, getErr := s.batches.GetActiveBatchesByProductForUpdate(ctx, tx, productID)
		if getErr != nil {
			return fmt.Errorf("lock batches: %w", getErr)
		}

		for i := range batches {
			batch := &batches[i]
			if remaining <= 0 {
				break
			}

			current, getErr := batch.QuantityCurrent.Float64Value()
			if getErr != nil {
				return fmt.Errorf("parse batch %s quantity: %w", batch.ID, getErr)
			}
			take := math.Min(current.Float64, remaining)
			if take <= 0 {
				continue
			}

			takeStr := strconv.FormatFloat(take, 'f', 4, 64)
			if _, getErr2 := s.batches.UpdateBatchQuantity(ctx, tx, batch.ID, "-"+takeStr); getErr2 != nil {
				return fmt.Errorf("deduct batch %s: %w", batch.ID, getErr2)
			}

			txRow, getErr2 := s.stockTransactions.CreateStockTransaction(ctx, tx, repositories.CreateStockTransactionParams{
				BatchID:         batch.ID,
				QuantityChange:  "-" + takeStr,
				TransactionType: models.TransactionTypeOutgoing,
				PerformedBy:     performedBy,
				ReferenceNote:   req.ReferenceNote,
			})
			if getErr2 != nil {
				return fmt.Errorf("record outgoing for batch %s: %w", batch.ID, getErr2)
			}
			transactions = append(transactions, txRow)
			remaining -= take
		}

		if remaining > 0 {
			return ErrInsufficientStock
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return transactions, nil
}
