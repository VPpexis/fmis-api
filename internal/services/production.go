// Package services contains domain logic and owns transactions.
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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors that router maps to HTTP status code.
var (
	ErrProductionOrderNotFound     = errors.New("production order not found")
	ErrInvalidProductionOrderState = errors.New("invalid production order state")
	ErrInvalidProductionInput      = errors.New("invalid production input")
)

// ProductionOrderService owns the business logic for production_orders.
type ProductionOrderService struct {
	pool             *pgxpool.Pool
	products         *repositories.ProductRepository
	batches          *repositories.BatchRepository
	productionOrder  *repositories.ProductionOrderRepository
	stockTransaction *repositories.StockTransactionRepository
}

// NewProductionOrderService creates a new production_order.
func NewProductionOrderService(pool *pgxpool.Pool) *ProductionOrderService {
	return &ProductionOrderService{
		pool:             pool,
		products:         &repositories.ProductRepository{},
		batches:          &repositories.BatchRepository{},
		productionOrder:  &repositories.ProductionOrderRepository{},
		stockTransaction: &repositories.StockTransactionRepository{},
	}
}

// List returns all production orders with an optional status filter.
func (s *ProductionOrderService) List(ctx context.Context, productionOrderStatusTypeStr, limitStr, offsetStr string) ([]models.ProductionOrder, error) {
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

	var productionOrderStatusType *models.ProductionOrderStatusType
	if productionOrderStatusTypeStr != "" {
		status := models.ProductionOrderStatusType(productionOrderStatusTypeStr)
		switch status {
		case models.ProductionOrderStatusTypePlanned,
			models.ProductionOrderStatusTypeInProgress,
			models.ProductionOrderStatusTypeCompleted,
			models.ProductionOrderStatusTypeCancelled:
			productionOrderStatusType = &status
		default:
			return nil, ErrInvalidRequest
		}
	}

	productionOrders, err := s.productionOrder.ListProductionOrders(ctx, s.pool, repositories.ListProductionOrderParams{
		Status: productionOrderStatusType,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list production order: %w", err)
	}
	return productionOrders, nil
}

// Create creates a new production_order and records it.
func (s *ProductionOrderService) Create(ctx context.Context, req *schemas.CreateProductionOrderRequest) (models.ProductionOrder, []models.ProductionOrderLineItem, error) {
	outputProductID, err := uuid.Parse(req.OutputProductID)
	if err != nil {
		return models.ProductionOrder{}, make([]models.ProductionOrderLineItem, 0), ErrInvalidRequest
	}

	createdByRaw := middleware.UserIDFromContext(ctx)
	createdBy, err := uuid.Parse(createdByRaw)
	if err != nil {
		return models.ProductionOrder{}, make([]models.ProductionOrderLineItem, 0), ErrInvalidRequest
	}

	var productionOrder models.ProductionOrder
	productionOrderLineItems := make([]models.ProductionOrderLineItem, 0)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		product, getErr := s.products.GetProductByID(ctx, tx, outputProductID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrProductNotFound
			}
			return fmt.Errorf("get products: %w", getErr)
		}

		if product.ProductType != models.ProductTypeFinishedGood {
			return ErrInvalidProductionInput
		}

		for index := range req.LineItems {
			item := &req.LineItems[index]
			inputBatchID, getErr2 := uuid.Parse(item.InputBatchID)
			if getErr2 != nil {
				return ErrInvalidRequest
			}

			inputBatch, getErr2 := s.batches.GetBatchByID(ctx, tx, inputBatchID)
			if getErr2 != nil {
				if errors.Is(getErr2, pgx.ErrNoRows) {
					return ErrBatchNotFound
				}
				return fmt.Errorf("get input batch: %w", getErr2)
			}

			inputProduct, getErr2 := s.products.GetProductByID(ctx, tx, inputBatch.ProductID)
			if getErr2 != nil {
				return fmt.Errorf("get input product: %w", getErr2)
			}

			if inputProduct.ProductType != models.ProductTypeWhiteLabel &&
				inputProduct.ProductType != models.ProductTypePackaging {
				return ErrInvalidProductionInput
			}
		}

		outputBatch, getErr := s.batches.CreateBatch(ctx, tx, repositories.CreateBatchParams{
			ProductID:      outputProductID,
			BatchNumber:    req.OutputBatchNumber,
			Quantity:       req.OutputQuantity,
			ExpirationDate: req.OutputExpirationDate,
			Status:         models.BatchStatusTypeReserved,
		})
		if getErr != nil {
			return fmt.Errorf("create output batch: %w", getErr)
		}

		productionOrder, getErr = s.productionOrder.CreateProductionOrder(ctx, tx, repositories.CreateProductionOrderParams{
			OutputBatchID: outputBatch.ID,
			CreatedBy:     createdBy,
		})
		if getErr != nil {
			return fmt.Errorf("create production order: %w", getErr)
		}

		for index := range req.LineItems {
			item := &req.LineItems[index]
			inputBatchID, getErr := uuid.Parse(item.InputBatchID)
			if getErr != nil {
				return ErrInvalidRequest
			}
			productionOrderLineItem, getErr := s.productionOrder.CreateProductionOrderLineItem(ctx, tx, repositories.CreateProductionOrderLineItemParams{
				ProductionOrderID: productionOrder.ID,
				InputBatchID:      inputBatchID,
				QuantityConsumed:  item.QuantityConsumed,
			})
			if getErr != nil {
				return fmt.Errorf("create line item: %w", getErr)
			}
			productionOrderLineItems = append(productionOrderLineItems, productionOrderLineItem)
		}
		return nil
	})
	if err != nil {
		return models.ProductionOrder{}, make([]models.ProductionOrderLineItem, 0), err
	}
	return productionOrder, productionOrderLineItems, nil
}

// GetByID returns a single production order by ID with its line items.
func (s *ProductionOrderService) GetByID(ctx context.Context, productionOrderIDStr string) (models.ProductionOrder, []models.ProductionOrderLineItem, error) {
	productionOrderID, err := uuid.Parse(productionOrderIDStr)
	if err != nil {
		return models.ProductionOrder{}, nil, ErrInvalidRequest
	}

	productionOrder, err := s.productionOrder.GetProductionOrderByID(ctx, s.pool, productionOrderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ProductionOrder{}, nil, ErrProductionOrderNotFound
		}
		return models.ProductionOrder{}, nil, fmt.Errorf("get production order: %w", err)
	}

	productionOrderLineItems, err := s.productionOrder.GetProductionOrderLineItemsByOrderID(ctx, s.pool, productionOrder.ID)
	if err != nil {
		return models.ProductionOrder{}, nil, fmt.Errorf("list production line items: %w", err)
	}
	return productionOrder, productionOrderLineItems, nil
}

// Start transitions a PLANNED production order to IN_PROGRESS. The row is
// locked with SELECT ... FOR UPDATE so concurrent starts cannot both pass
// the status guard: only the first transition succeeds.
func (s *ProductionOrderService) Start(ctx context.Context, orderIDStr string) (models.ProductionOrder, error) {
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return models.ProductionOrder{}, ErrInvalidRequest
	}

	var productionOrder models.ProductionOrder
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var getErr error
		productionOrder, getErr = s.productionOrder.GetProductionOrderByIDForUpdate(ctx, tx, orderID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrProductionOrderNotFound
			}
			return fmt.Errorf("get production order: %w", getErr)
		}

		if productionOrder.Status != models.ProductionOrderStatusTypePlanned {
			return ErrInvalidProductionOrderState
		}

		productionOrder, getErr = s.productionOrder.UpdateProductionOrderStatus(ctx, tx, orderID, models.ProductionOrderStatusTypeInProgress)
		if getErr != nil {
			return fmt.Errorf("update production order status: %w", getErr)
		}
		return nil
	})
	if err != nil {
		return models.ProductionOrder{}, err
	}
	return productionOrder, nil
}

// Cancel transitions a PLANNED or IN_PROGRESS production order to CANCELLED.
func (s *ProductionOrderService) Cancel(ctx context.Context, orderIDStr string) (models.ProductionOrder, error) {
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return models.ProductionOrder{}, ErrInvalidRequest
	}

	var productionOrder models.ProductionOrder
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var getErr error
		productionOrder, getErr = s.productionOrder.GetProductionOrderByIDForUpdate(ctx, tx, orderID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrProductionOrderNotFound
			}
			return fmt.Errorf("get production order: %w", getErr)
		}

		if productionOrder.Status != models.ProductionOrderStatusTypePlanned && productionOrder.Status != models.ProductionOrderStatusTypeInProgress {
			return ErrInvalidProductionOrderState
		}

		productionOrder, getErr = s.productionOrder.UpdateProductionOrderStatus(ctx, tx, orderID, models.ProductionOrderStatusTypeCancelled)
		if getErr != nil {
			return fmt.Errorf("update production order status: %w", getErr)
		}
		return nil
	})
	if err != nil {
		return models.ProductionOrder{}, err
	}
	return productionOrder, nil
}

// Complete atomically consumes the order's input batches (USED_IN_PRODUCTION),
// activates the RESERVED output batch with the MIN input expiration date, and
// marks the order COMPLETED. The order row is locked FOR UPDATE so duplicate
// or concurrent completes cannot both pass the status guard.
func (s *ProductionOrderService) Complete(ctx context.Context, orderIDStr string) (models.ProductionOrder, []models.ProductionOrderLineItem, error) {
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return models.ProductionOrder{}, nil, ErrInvalidRequest
	}

	performedByRaw := middleware.UserIDFromContext(ctx)
	performedBy, err := uuid.Parse(performedByRaw)
	if err != nil {
		return models.ProductionOrder{}, nil, ErrInvalidRequest
	}

	var productionOrder models.ProductionOrder
	productionOrderLineItems := make([]models.ProductionOrderLineItem, 0)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var getErr error
		productionOrder, getErr = s.productionOrder.GetProductionOrderByIDForUpdate(ctx, tx, orderID)
		if getErr != nil {
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrProductionOrderNotFound
			}
			return fmt.Errorf("get production order: %w", getErr)
		}
		if productionOrder.Status != models.ProductionOrderStatusTypeInProgress {
			return ErrInvalidProductionOrderState
		}

		productionOrderLineItems, getErr = s.productionOrder.GetProductionOrderLineItemsByOrderID(ctx, tx, orderID)
		if getErr != nil {
			return fmt.Errorf("get production order line items: %w", getErr)
		}

		var outputExpiration *time.Time
		for i := range productionOrderLineItems {
			item := &productionOrderLineItems[i]

			batch, getErr2 := s.batches.GetBatchByIDForUpdate(ctx, tx, item.InputBatchID)
			if getErr2 != nil {
				return fmt.Errorf("lock input batch: %w", getErr2)
			}

			consumed, getErr2 := item.QuantityConsumed.Float64Value()
			if getErr2 != nil {
				return fmt.Errorf("parse consumed quantity: %w", getErr2)
			}
			current, getErr2 := batch.QuantityCurrent.Float64Value()
			if getErr2 != nil {
				return fmt.Errorf("parse batch %s quantity: %w", batch.ID, getErr2)
			}
			if current.Float64 < consumed.Float64 {
				return ErrInsufficientStock
			}

			deductStr := strconv.FormatFloat(-consumed.Float64, 'f', 4, 64)
			if _, getErr2 = s.batches.UpdateBatchQuantity(ctx, tx, item.InputBatchID, deductStr); getErr2 != nil {
				return fmt.Errorf("deduct input batches: %w", getErr2)
			}

			if _, getErr2 = s.stockTransaction.CreateStockTransaction(ctx, tx, &repositories.CreateStockTransactionParams{
				BatchID:           item.InputBatchID,
				ProductionOrderID: &orderID,
				QuantityChange:    deductStr,
				TransactionType:   models.TransactionTypeUsedInProduction,
				PerformedBy:       performedBy,
			}); getErr2 != nil {
				return fmt.Errorf("record production transaction: %w", getErr2)
			}

			if batch.ExpirationDate.Valid &&
				(outputExpiration == nil || batch.ExpirationDate.Time.Before(*outputExpiration)) {
				t := batch.ExpirationDate.Time
				outputExpiration = &t
			}
		}

		if _, getErr2 := s.batches.ActivateProductionOutput(ctx, tx, productionOrder.OutputBatchID, outputExpiration); getErr2 != nil {
			return fmt.Errorf("activate output batch: %w", getErr2)
		}

		productionOrder, getErr = s.productionOrder.UpdateProductionOrderStatus(ctx, tx, orderID, models.ProductionOrderStatusTypeCompleted)
		if getErr != nil {
			return fmt.Errorf("update production order status: %w", getErr)
		}
		return nil
	})
	if err != nil {
		return models.ProductionOrder{}, nil, err
	}
	return productionOrder, productionOrderLineItems, nil
}
