// Package services contains domain logic and owns transactions.
package services

import (
	"context"
	"time"

	"fmis-api/internal/models"
	"fmis-api/internal/repositories"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxStarter begins a transaction and hands it to fn, committing on success and
// rolling back when fn returns an error. It wraps pgx.BeginFunc so services can
// be unit-tested with a fake transaction runner instead of a real pool.
type TxStarter func(ctx context.Context, fn func(tx pgx.Tx) error) error

// poolTxStarter binds TxStarter to a real connection pool.
func poolTxStarter(pool *pgxpool.Pool) TxStarter {
	return func(ctx context.Context, fn func(tx pgx.Tx) error) error {
		return pgx.BeginFunc(ctx, pool, fn)
	}
}

// UserStore is the subset of the user repository the auth domain needs.
type UserStore interface {
	CreateUser(ctx context.Context, q repositories.Querier, p repositories.CreateUserParams) (models.User, error)
	GetUserByIdentifier(ctx context.Context, q repositories.Querier, identifier string) (models.User, error)
	GetUserByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.User, error)
}

// RefreshTokenStore is the subset of the refresh token repository the auth domain needs.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, q repositories.Querier, p repositories.CreateRefreshTokenParams) (models.RefreshToken, error)
	FindByHash(ctx context.Context, q repositories.Querier, tokenHash string) (models.RefreshToken, error)
	Revoke(ctx context.Context, q repositories.Querier, tokenHash string) error
}

// ProductStore is the subset of the product repository the batch and production domains need.
type ProductStore interface {
	GetProductByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.Product, error)
}

// BatchStore is the subset of the batch repository the service layer needs.
type BatchStore interface {
	CreateBatch(ctx context.Context, q repositories.Querier, p repositories.CreateBatchParams) (models.InventoryBatch, error)
	GetBatchByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.InventoryBatch, error)
	GetBatchByIDForUpdate(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.InventoryBatch, error)
	GetActiveBatchesByProduct(ctx context.Context, q repositories.Querier, productID uuid.UUID) ([]models.InventoryBatch, error)
	GetActiveBatchesByProductForUpdate(ctx context.Context, q repositories.Querier, productID uuid.UUID) ([]models.InventoryBatch, error)
	SetBatchStatus(ctx context.Context, q repositories.Querier, batchID uuid.UUID, status models.BatchStatusType) (models.InventoryBatch, error)
	UpdateBatchQuantity(ctx context.Context, q repositories.Querier, batchID uuid.UUID, delta string) (models.InventoryBatch, error)
	ActivateProductionOutput(ctx context.Context, q repositories.Querier, batchID uuid.UUID, expirationDate *time.Time) (models.InventoryBatch, error)
}

// ProductionOrderStore is the subset of the production order repository the service layer needs.
type ProductionOrderStore interface {
	CreateProductionOrder(ctx context.Context, q repositories.Querier, p repositories.CreateProductionOrderParams) (models.ProductionOrder, error)
	CreateProductionOrderLineItem(ctx context.Context, q repositories.Querier, p repositories.CreateProductionOrderLineItemParams) (models.ProductionOrderLineItem, error)
	GetProductionOrderByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.ProductionOrder, error)
	GetProductionOrderByIDForUpdate(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.ProductionOrder, error)
	GetProductionOrderLineItemsByOrderID(ctx context.Context, q repositories.Querier, orderID uuid.UUID) ([]models.ProductionOrderLineItem, error)
	ListProductionOrders(ctx context.Context, q repositories.Querier, p repositories.ListProductionOrderParams) ([]models.ProductionOrder, error)
	UpdateProductionOrderStatus(ctx context.Context, q repositories.Querier, orderID uuid.UUID, status models.ProductionOrderStatusType) (models.ProductionOrder, error)
}

// StockTransactionStore is the subset of the stock transaction repository the service layer needs.
type StockTransactionStore interface {
	CreateStockTransaction(ctx context.Context, q repositories.Querier, p *repositories.CreateStockTransactionParams) (models.StockTransaction, error)
	ListStockTransaction(ctx context.Context, q repositories.Querier, p repositories.ListStockTransactionParams) ([]models.StockTransaction, error)
}
