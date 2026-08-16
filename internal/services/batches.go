// Package services contains doman logic and owns transactions.
package services

import (
	"context"
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors that router maps to HTTP status codes.
var (
	ErrInvalidRequest     = errors.New("invalid request")
	ErrProductNotFound    = errors.New("product not found")
	ErrBatchNotFound      = errors.New("batch not found")
	ErrExpirationRequired = errors.New("expiration date is required for this product type")
	ErrInvalidBatchState  = errors.New("batch cannot be quarantined from its current status")
)

// BatchService owns the business logic for inventory batches.
type BatchService struct {
	pool     *pgxpool.Pool
	products *repositories.ProductRepository
	batches  *repositories.BatchRepository
}

// NewBatchService creates a new BatchService.
func NewBatchService(pool *pgxpool.Pool) *BatchService {
	return &BatchService{
		pool:     pool,
		products: &repositories.ProductRepository{},
		batches:  &repositories.BatchRepository{},
	}
}

// Receive creates a new batch and records the initial stock.
func (s *BatchService) Receive(ctx context.Context, req schemas.CreateBatchRequest) (models.InventoryBatch, error) {
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		return models.InventoryBatch{}, ErrInvalidRequest
	}

	performedByRaw := middleware.UserIDFromContext(ctx)
	performedBy, err := uuid.Parse(performedByRaw)
	if err != nil {
		return models.InventoryBatch{}, ErrInvalidRequest
	}

	var batch models.InventoryBatch
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		product, getErr := s.products.GetProductByID(ctx, tx, productID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrProductNotFound
			}
			return fmt.Errorf("get product: %w", getErr)
		}

		if product.ProductType != models.ProductTypePackaging && req.ExpirationDate == nil {
			return ErrExpirationRequired
		}

		batch, err = s.batches.CreateBatch(ctx, tx, repositories.CreateBatchParams{
			ProductID:      productID,
			BatchNumber:    req.BatchNumber,
			Quantity:       req.Quantity,
			ExpirationDate: req.ExpirationDate,
		})
		if err != nil {
			return fmt.Errorf("create batch: %w", err)
		}
		_, err = s.batches.CreateStockTransaction(ctx, tx, repositories.CreateStockTransacitonParams{
			BatchID:        batch.ID,
			QuantityChange: req.Quantity,
			PerformedBy:    performedBy,
		})
		if err != nil {
			return fmt.Errorf("create stock transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return models.InventoryBatch{}, err
	}
	return batch, nil
}

// ListActiveByProduct ID returns all non-expired batches for a product
func (s *BatchService) ListActiveByProduct(ctx context.Context, productIDStr string) ([]models.InventoryBatch, error) {
	productID, err := uuid.Parse(productIDStr)
	if err != nil {
		return nil, ErrInvalidRequest
	}

	if _, getErr := s.products.GetProductByID(ctx, s.pool, productID); getErr != nil {
		if errors.Is(getErr, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, fmt.Errorf("get product: %w", getErr)
	}

	batches, err := s.batches.GetActivateBatchesByProduct(ctx, s.pool, productID)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	return batches, nil
}

// Quarantine moves a batch to QUARANTINED, only from ACTIVE. The row is locked
// with SELECT ... FOR UPDATE so concurrent requests cannot both pass the state
// guard: only the first transition from ACTIVE succeeds, the rest get
// ErrInvalidBatchState.
func (s *BatchService) Quarantine(ctx context.Context, batchIDStr string) (models.InventoryBatch, error) {
	batchID, err := uuid.Parse(batchIDStr)
	if err != nil {
		return models.InventoryBatch{}, ErrInvalidRequest
	}

	var batch models.InventoryBatch
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		batch, err = s.batches.GetBatchByIDForUpdate(ctx, tx, batchID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrBatchNotFound
			}
			return fmt.Errorf("get batch: %w", err)
		}

		if batch.Status != models.BatchStatusTypeActive {
			return ErrInvalidBatchState
		}

		batch, err = s.batches.SetBatchStatus(ctx, tx, batchID, models.BatchStatusTypeQuarantined)
		if err != nil {
			return fmt.Errorf("set batch status: %w", err)
		}
		return nil
	})
	if err != nil {
		return models.InventoryBatch{}, err
	}
	return batch, nil
}
