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
	pool            *pgxpool.Pool
	products        *repositories.ProductRepository
	batches         *repositories.BatchRepository
	productionOrder *repositories.ProductionOrderRepository
}

// NewProductionOrderService creates a new production_order.
func NewProductionOrderService(pool *pgxpool.Pool) *ProductionOrderService {
	return &ProductionOrderService{
		pool:            pool,
		products:        &repositories.ProductRepository{},
		batches:         &repositories.BatchRepository{},
		productionOrder: &repositories.ProductionOrderRepository{},
	}
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
