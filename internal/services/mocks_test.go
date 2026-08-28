// Package services unit-test helpers: testify mocks for the repository
// interfaces plus a fake transaction runner. Test-only file, never compiled
// into the application binary.
package services

import (
	"context"
	"time"

	"fmis-api/internal/models"
	"fmis-api/internal/repositories"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
)

// stubQuerier satisfies repositories.Querier but must never be invoked:
// mocked repositories ignore the queryer argument, so unit tests never touch
// a database.
type stubQuerier struct{}

func (stubQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("stubQuerier.Exec must not be called")
}

func (stubQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("stubQuerier.Query must not be called")
}

func (stubQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("stubQuerier.QueryRow must not be called")
}

// fakeTx is a TxStarter that invokes the transaction callback with a nil
// pgx.Tx. Mocked repositories never touch it, so transaction semantics only
// matter at the integration level; here it just runs the service's closure.
func fakeTx(_ context.Context, fn func(tx pgx.Tx) error) error {
	return fn(nil)
}

// MockUserStore is a testify mock of UserStore.
type MockUserStore struct{ mock.Mock }

func (m *MockUserStore) CreateUser(ctx context.Context, q repositories.Querier, p repositories.CreateUserParams) (models.User, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.User), args.Error(1)
}

func (m *MockUserStore) GetUserByIdentifier(ctx context.Context, q repositories.Querier, identifier string) (models.User, error) {
	args := m.Called(ctx, q, identifier)
	return args.Get(0).(models.User), args.Error(1)
}

func (m *MockUserStore) GetUserByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.User, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.User), args.Error(1)
}

// MockRefreshTokenStore is a testify mock of RefreshTokenStore.
type MockRefreshTokenStore struct{ mock.Mock }

func (m *MockRefreshTokenStore) CreateRefreshToken(ctx context.Context, q repositories.Querier, p repositories.CreateRefreshTokenParams) (models.RefreshToken, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.RefreshToken), args.Error(1)
}

func (m *MockRefreshTokenStore) FindByHash(ctx context.Context, q repositories.Querier, tokenHash string) (models.RefreshToken, error) {
	args := m.Called(ctx, q, tokenHash)
	return args.Get(0).(models.RefreshToken), args.Error(1)
}

func (m *MockRefreshTokenStore) Revoke(ctx context.Context, q repositories.Querier, tokenHash string) error {
	args := m.Called(ctx, q, tokenHash)
	return args.Error(0)
}

// MockProductStore is a testify mock of ProductStore.
type MockProductStore struct{ mock.Mock }

func (m *MockProductStore) GetProductByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.Product, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.Product), args.Error(1)
}

// MockBatchStore is a testify mock of BatchStore.
type MockBatchStore struct{ mock.Mock }

func (m *MockBatchStore) CreateBatch(ctx context.Context, q repositories.Querier, p repositories.CreateBatchParams) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) GetBatchByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) GetBatchByIDForUpdate(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) GetActiveBatchesByProduct(ctx context.Context, q repositories.Querier, productID uuid.UUID) ([]models.InventoryBatch, error) {
	args := m.Called(ctx, q, productID)
	return args.Get(0).([]models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) GetActiveBatchesByProductForUpdate(ctx context.Context, q repositories.Querier, productID uuid.UUID) ([]models.InventoryBatch, error) {
	args := m.Called(ctx, q, productID)
	return args.Get(0).([]models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) SetBatchStatus(ctx context.Context, q repositories.Querier, batchID uuid.UUID, status models.BatchStatusType) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, batchID, status)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) UpdateBatchQuantity(ctx context.Context, q repositories.Querier, batchID uuid.UUID, delta string) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, batchID, delta)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

func (m *MockBatchStore) ActivateProductionOutput(ctx context.Context, q repositories.Querier, batchID uuid.UUID, expirationDate *time.Time) (models.InventoryBatch, error) {
	args := m.Called(ctx, q, batchID, expirationDate)
	return args.Get(0).(models.InventoryBatch), args.Error(1)
}

// MockProductionOrderStore is a testify mock of ProductionOrderStore.
type MockProductionOrderStore struct{ mock.Mock }

func (m *MockProductionOrderStore) CreateProductionOrder(ctx context.Context, q repositories.Querier, p repositories.CreateProductionOrderParams) (models.ProductionOrder, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.ProductionOrder), args.Error(1)
}

func (m *MockProductionOrderStore) CreateProductionOrderLineItem(ctx context.Context, q repositories.Querier, p repositories.CreateProductionOrderLineItemParams) (models.ProductionOrderLineItem, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.ProductionOrderLineItem), args.Error(1)
}

func (m *MockProductionOrderStore) GetProductionOrderByID(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.ProductionOrder, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.ProductionOrder), args.Error(1)
}

func (m *MockProductionOrderStore) GetProductionOrderByIDForUpdate(ctx context.Context, q repositories.Querier, id uuid.UUID) (models.ProductionOrder, error) {
	args := m.Called(ctx, q, id)
	return args.Get(0).(models.ProductionOrder), args.Error(1)
}

func (m *MockProductionOrderStore) GetProductionOrderLineItemsByOrderID(ctx context.Context, q repositories.Querier, orderID uuid.UUID) ([]models.ProductionOrderLineItem, error) {
	args := m.Called(ctx, q, orderID)
	return args.Get(0).([]models.ProductionOrderLineItem), args.Error(1)
}

func (m *MockProductionOrderStore) ListProductionOrders(ctx context.Context, q repositories.Querier, p repositories.ListProductionOrderParams) ([]models.ProductionOrder, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).([]models.ProductionOrder), args.Error(1)
}

func (m *MockProductionOrderStore) UpdateProductionOrderStatus(ctx context.Context, q repositories.Querier, orderID uuid.UUID, status models.ProductionOrderStatusType) (models.ProductionOrder, error) {
	args := m.Called(ctx, q, orderID, status)
	return args.Get(0).(models.ProductionOrder), args.Error(1)
}

// MockStockTransactionStore is a testify mock of StockTransactionStore.
type MockStockTransactionStore struct{ mock.Mock }

func (m *MockStockTransactionStore) CreateStockTransaction(ctx context.Context, q repositories.Querier, p *repositories.CreateStockTransactionParams) (models.StockTransaction, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).(models.StockTransaction), args.Error(1)
}

func (m *MockStockTransactionStore) ListStockTransaction(ctx context.Context, q repositories.Querier, p repositories.ListStockTransactionParams) ([]models.StockTransaction, error) {
	args := m.Called(ctx, q, p)
	return args.Get(0).([]models.StockTransaction), args.Error(1)
}
