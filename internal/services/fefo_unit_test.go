// Package services FEFO domain unit tests: consumption ordering and edge
// cases (NULL expirations, depletion mid-consumption) against mocked
// repositories. No database required.
package services

import (
	"context"
	"testing"
	"time"

	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/schemas"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newMockInventoryService builds an InventoryService with mocked stores.
func newMockInventoryService(batches BatchStore, stockTx StockTransactionStore) *InventoryService {
	return NewInventoryServiceWithDeps(stubQuerier{}, fakeTx, batches, stockTx)
}

func TestConsumeUnitFEFOOrder(t *testing.T) {
	productID := uuid.New()
	performedBy := uuid.New()
	first, second := uuid.New(), uuid.New()
	early := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// The mock simulates the repository's FEFO query:
	// ORDER BY expiration_date ASC NULLS LAST, so the dated batch comes first
	// and the NULL-expiration batch last.
	firstBatch := batchWithExpiration(t, "5.0000", &early)
	firstBatch.ID = first
	secondBatch := batchWithExpiration(t, "10.0000", nil)
	secondBatch.ID = second
	batches := []models.InventoryBatch{firstBatch, secondBatch}

	batchesMock := new(MockBatchStore)
	stock := new(MockStockTransactionStore)
	batchesMock.On("GetActiveBatchesByProductForUpdate", mock.Anything, mock.Anything, productID).Return(batches, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, first, "-5.0000").Return(models.InventoryBatch{}, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, second, "-3.0000").Return(models.InventoryBatch{}, nil).Once()
	stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Twice()

	svc := newMockInventoryService(batchesMock, stock)
	ctx := middleware.WithUserID(context.Background(), performedBy.String())

	transactions, err := svc.Consume(ctx, schemas.ConsumeStockRequest{
		ProductID: productID.String(),
		Quantity:  "8",
	})
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	batchesMock.AssertExpectations(t)
	stock.AssertExpectations(t)
}

func TestConsumeUnitFEFOOrderAcrossBatches(t *testing.T) {
	productID := uuid.New()
	performedBy := uuid.New()
	a, b, c := uuid.New(), uuid.New(), uuid.New()

	// FEFO order: the earliest-expiring batch is drained first, then the next.
	expA := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	expB := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	bA := batchWithExpiration(t, "3.0000", &expA)
	bA.ID = a
	bB := batchWithExpiration(t, "5.0000", &expB)
	bB.ID = b
	bC := batchWithExpiration(t, "9.0000", nil)
	bC.ID = c
	batches := []models.InventoryBatch{bA, bB, bC}

	batchesMock := new(MockBatchStore)
	stock := new(MockStockTransactionStore)
	batchesMock.On("GetActiveBatchesByProductForUpdate", mock.Anything, mock.Anything, productID).Return(batches, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, a, "-3.0000").Return(models.InventoryBatch{}, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, b, "-5.0000").Return(models.InventoryBatch{}, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, c, "-2.0000").Return(models.InventoryBatch{}, nil).Once()
	stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Times(3)

	svc := newMockInventoryService(batchesMock, stock)
	ctx := middleware.WithUserID(context.Background(), performedBy.String())

	transactions, err := svc.Consume(ctx, schemas.ConsumeStockRequest{
		ProductID: productID.String(),
		Quantity:  "10",
	})
	require.NoError(t, err)
	assert.Len(t, transactions, 3)
	batchesMock.AssertExpectations(t)
}

func TestConsumeUnitInsufficientStock(t *testing.T) {
	productID := uuid.New()
	performedBy := uuid.New()

	batches := []models.InventoryBatch{
		batchWithExpiration(t, "2.0000", nil),
		batchWithExpiration(t, "3.0000", nil),
	}

	batchesMock := new(MockBatchStore)
	stock := new(MockStockTransactionStore)
	batchesMock.On("GetActiveBatchesByProductForUpdate", mock.Anything, mock.Anything, productID).Return(batches, nil).Once()

	svc := newMockInventoryService(batchesMock, stock)
	ctx := middleware.WithUserID(context.Background(), performedBy.String())

	_, err := svc.Consume(ctx, schemas.ConsumeStockRequest{
		ProductID: productID.String(),
		Quantity:  "10",
	})
	assert.ErrorIs(t, err, ErrInsufficientStock)
	batchesMock.AssertNotCalled(t, "UpdateBatchQuantity", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	stock.AssertNotCalled(t, "CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything)
}

func TestConsumeUnitDepletionMidConsumption(t *testing.T) {
	productID := uuid.New()
	performedBy := uuid.New()
	first, depleted, third := uuid.New(), uuid.New(), uuid.New()

	// A fully depleted (zero-quantity) batch sits between two usable ones:
	// the algorithm must skip it and continue with the next batch.
	expA := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	expB := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	expC := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	bA := batchWithExpiration(t, "5.0000", &expA)
	bA.ID = first
	bB := batchWithExpiration(t, "0.0000", &expB)
	bB.ID = depleted
	bC := batchWithExpiration(t, "10.0000", &expC)
	bC.ID = third
	batches := []models.InventoryBatch{bA, bB, bC}

	batchesMock := new(MockBatchStore)
	stock := new(MockStockTransactionStore)
	batchesMock.On("GetActiveBatchesByProductForUpdate", mock.Anything, mock.Anything, productID).Return(batches, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, first, "-5.0000").Return(models.InventoryBatch{}, nil).Once()
	batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, third, "-3.0000").Return(models.InventoryBatch{}, nil).Once()
	stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Twice()

	svc := newMockInventoryService(batchesMock, stock)
	ctx := middleware.WithUserID(context.Background(), performedBy.String())

	transactions, err := svc.Consume(ctx, schemas.ConsumeStockRequest{
		ProductID: productID.String(),
		Quantity:  "8",
	})
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	batchesMock.AssertNotCalled(t, "UpdateBatchQuantity", mock.Anything, mock.Anything, depleted, mock.Anything)
	batchesMock.AssertExpectations(t)
}

func TestConsumeUnitNoBatches(t *testing.T) {
	productID := uuid.New()
	performedBy := uuid.New()

	batchesMock := new(MockBatchStore)
	stock := new(MockStockTransactionStore)
	batchesMock.On("GetActiveBatchesByProductForUpdate", mock.Anything, mock.Anything, productID).Return([]models.InventoryBatch{}, nil).Once()

	svc := newMockInventoryService(batchesMock, stock)
	ctx := middleware.WithUserID(context.Background(), performedBy.String())

	_, err := svc.Consume(ctx, schemas.ConsumeStockRequest{
		ProductID: productID.String(),
		Quantity:  "1",
	})
	assert.ErrorIs(t, err, ErrInsufficientStock)
}

func TestListActiveByProductUnitPassesThroughFEFOOrder(t *testing.T) {
	productID := uuid.New()

	early := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	want := []models.InventoryBatch{
		batchWithExpiration(t, "5.0000", &early),
		batchWithExpiration(t, "7.0000", nil), // NULL expiration sorts last per NULLS LAST
	}

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	products.On("GetProductByID", mock.Anything, mock.Anything, productID).Return(models.Product{ID: productID}, nil).Once()
	batchesMock.On("GetActiveBatchesByProduct", mock.Anything, mock.Anything, productID).Return(want, nil).Once()

	svc := NewBatchServiceWithDeps(stubQuerier{}, fakeTx, products, batchesMock, new(MockStockTransactionStore))

	got, err := svc.ListActiveByProduct(context.Background(), productID.String())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, want[0].ID, got[0].ID, "dated batch must stay first")
	assert.Equal(t, want[1].ID, got[1].ID, "NULL-expiration batch must stay last")
	batchesMock.AssertExpectations(t)
}

func TestListActiveByProductUnitUnknownProduct(t *testing.T) {
	productID := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	products.On("GetProductByID", mock.Anything, mock.Anything, productID).Return(models.Product{}, pgx.ErrNoRows).Once()

	svc := NewBatchServiceWithDeps(stubQuerier{}, fakeTx, products, batchesMock, new(MockStockTransactionStore))

	_, err := svc.ListActiveByProduct(context.Background(), productID.String())
	assert.ErrorIs(t, err, ErrProductNotFound)
	batchesMock.AssertNotCalled(t, "GetActiveBatchesByProduct", mock.Anything, mock.Anything, mock.Anything)
}
