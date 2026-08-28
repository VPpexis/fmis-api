// Package services production domain unit tests: completion rules (MIN input
// expiration, insufficient stock, duplicate complete) against mocked
// repositories. No database required.
package services

import (
	"context"
	"testing"
	"time"

	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newMockProductionService builds a ProductionOrderService with mocked stores.
func newMockProductionService(products ProductStore, batches BatchStore, productionOrder ProductionOrderStore, stockTx StockTransactionStore) *ProductionOrderService {
	return NewProductionOrderServiceWithDeps(stubQuerier{}, fakeTx, products, batches, productionOrder, stockTx)
}

// batchWithExpiration builds an ACTIVE batch with a quantity and optional expiration.
func batchWithExpiration(t *testing.T, quantity string, expiration *time.Time) models.InventoryBatch {
	b := batchWithQuantity(t, quantity)
	if expiration != nil {
		b.ExpirationDate = pgtype.Timestamptz{Time: *expiration, Valid: true}
	}
	return b
}

func testOrder(orderID, outputBatchID uuid.UUID, status models.ProductionOrderStatusType) models.ProductionOrder {
	return models.ProductionOrder{ID: orderID, OutputBatchID: outputBatchID, Status: status}
}

func testLineItem(inputBatchID uuid.UUID, quantity string) models.ProductionOrderLineItem {
	return models.ProductionOrderLineItem{
		ID:               uuid.New(),
		ProductionOrderID: uuid.New(),
		InputBatchID:     inputBatchID,
		QuantityConsumed: mustNumeric(quantity),
	}
}

// mustNumeric is the non-testing variant of numeric for model construction.
func mustNumeric(value string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(value)
	return n
}

func TestCompleteUnitMinExpiration(t *testing.T) {
	orderID := uuid.New()
	outputBatchID := uuid.New()
	order := testOrder(orderID, outputBatchID, models.ProductionOrderStatusTypeInProgress)
	completed := order
	completed.Status = models.ProductionOrderStatusTypeCompleted

	inputA, inputB := uuid.New(), uuid.New()
	expA := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	expB := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	items := []models.ProductionOrderLineItem{testLineItem(inputA, "2.5000"), testLineItem(inputB, "1.0000")}
	batches := []models.InventoryBatch{
		batchWithExpiration(t, "10.0000", &expA),
		batchWithExpiration(t, "5.0000", &expB),
	}

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()
	prod.On("GetProductionOrderLineItemsByOrderID", mock.Anything, mock.Anything, orderID).Return(items, nil).Once()
	for i, item := range items {
		batchesMock.On("GetBatchByIDForUpdate", mock.Anything, mock.Anything, item.InputBatchID).Return(batches[i], nil).Once()
		batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, item.InputBatchID, mock.Anything).Return(models.InventoryBatch{}, nil).Once()
		stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Once()
	}
	batchesMock.On("ActivateProductionOutput", mock.Anything, mock.Anything, outputBatchID, mock.MatchedBy(func(e *time.Time) bool {
		return e != nil && e.Equal(expB)
	})).Return(models.InventoryBatch{}, nil).Once()
	prod.On("UpdateProductionOrderStatus", mock.Anything, mock.Anything, orderID, models.ProductionOrderStatusTypeCompleted).Return(completed, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	got, gotItems, err := svc.Complete(ctx, orderID.String())
	require.NoError(t, err)
	assert.Equal(t, models.ProductionOrderStatusTypeCompleted, got.Status)
	assert.Equal(t, orderID, got.ID)
	assert.Len(t, gotItems, 2)
	batchesMock.AssertExpectations(t)
	prod.AssertExpectations(t)
	stock.AssertExpectations(t)
}

func TestCompleteUnitMinExpirationSkipsNullInputs(t *testing.T) {
	orderID := uuid.New()
	outputBatchID := uuid.New()
	order := testOrder(orderID, outputBatchID, models.ProductionOrderStatusTypeInProgress)
	completed := order
	completed.Status = models.ProductionOrderStatusTypeCompleted

	inputA, inputB := uuid.New(), uuid.New()
	expB := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	items := []models.ProductionOrderLineItem{testLineItem(inputA, "2.5000"), testLineItem(inputB, "1.0000")}
	batches := []models.InventoryBatch{
		batchWithExpiration(t, "10.0000", nil), // NULL expiration must not clobber the MIN
		batchWithExpiration(t, "5.0000", &expB),
	}

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()
	prod.On("GetProductionOrderLineItemsByOrderID", mock.Anything, mock.Anything, orderID).Return(items, nil).Once()
	for i, item := range items {
		batchesMock.On("GetBatchByIDForUpdate", mock.Anything, mock.Anything, item.InputBatchID).Return(batches[i], nil).Once()
		batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, item.InputBatchID, mock.Anything).Return(models.InventoryBatch{}, nil).Once()
		stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Once()
	}
	batchesMock.On("ActivateProductionOutput", mock.Anything, mock.Anything, outputBatchID, mock.MatchedBy(func(e *time.Time) bool {
		return e != nil && e.Equal(expB)
	})).Return(models.InventoryBatch{}, nil).Once()
	prod.On("UpdateProductionOrderStatus", mock.Anything, mock.Anything, orderID, models.ProductionOrderStatusTypeCompleted).Return(completed, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, orderID.String())
	require.NoError(t, err)
	batchesMock.AssertExpectations(t)
}

func TestCompleteUnitAllNullExpirations(t *testing.T) {
	orderID := uuid.New()
	outputBatchID := uuid.New()
	order := testOrder(orderID, outputBatchID, models.ProductionOrderStatusTypeInProgress)
	completed := order
	completed.Status = models.ProductionOrderStatusTypeCompleted

	inputA, inputB := uuid.New(), uuid.New()
	items := []models.ProductionOrderLineItem{testLineItem(inputA, "2.5000"), testLineItem(inputB, "1.0000")}
	batches := []models.InventoryBatch{
		batchWithExpiration(t, "10.0000", nil),
		batchWithExpiration(t, "5.0000", nil),
	}

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()
	prod.On("GetProductionOrderLineItemsByOrderID", mock.Anything, mock.Anything, orderID).Return(items, nil).Once()
	for i, item := range items {
		batchesMock.On("GetBatchByIDForUpdate", mock.Anything, mock.Anything, item.InputBatchID).Return(batches[i], nil).Once()
		batchesMock.On("UpdateBatchQuantity", mock.Anything, mock.Anything, item.InputBatchID, mock.Anything).Return(models.InventoryBatch{}, nil).Once()
		stock.On("CreateStockTransaction", mock.Anything, mock.Anything, mock.Anything).Return(models.StockTransaction{}, nil).Once()
	}
	batchesMock.On("ActivateProductionOutput", mock.Anything, mock.Anything, outputBatchID, mock.MatchedBy(func(e *time.Time) bool {
		return e == nil
	})).Return(models.InventoryBatch{}, nil).Once()
	prod.On("UpdateProductionOrderStatus", mock.Anything, mock.Anything, orderID, models.ProductionOrderStatusTypeCompleted).Return(completed, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, orderID.String())
	require.NoError(t, err)
	batchesMock.AssertExpectations(t)
}

func TestCompleteUnitInsufficientStock(t *testing.T) {
	orderID := uuid.New()
	order := testOrder(orderID, uuid.New(), models.ProductionOrderStatusTypeInProgress)

	inputA := uuid.New()
	items := []models.ProductionOrderLineItem{testLineItem(inputA, "8.0000")}
	batches := []models.InventoryBatch{
		batchWithExpiration(t, "5.0000", nil), // current < consumed
	}

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()
	prod.On("GetProductionOrderLineItemsByOrderID", mock.Anything, mock.Anything, orderID).Return(items, nil).Once()
	batchesMock.On("GetBatchByIDForUpdate", mock.Anything, mock.Anything, inputA).Return(batches[0], nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, orderID.String())
	assert.ErrorIs(t, err, ErrInsufficientStock)
	batchesMock.AssertNotCalled(t, "UpdateBatchQuantity", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	batchesMock.AssertNotCalled(t, "ActivateProductionOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	prod.AssertNotCalled(t, "UpdateProductionOrderStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestCompleteUnitDuplicateCompleteRejected(t *testing.T) {
	orderID := uuid.New()
	order := testOrder(orderID, uuid.New(), models.ProductionOrderStatusTypeCompleted) // already COMPLETED

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, orderID.String())
	assert.ErrorIs(t, err, ErrInvalidProductionOrderState)
	prod.AssertNotCalled(t, "GetProductionOrderLineItemsByOrderID", mock.Anything, mock.Anything, mock.Anything)
	batchesMock.AssertNotCalled(t, "GetBatchByIDForUpdate", mock.Anything, mock.Anything, mock.Anything)
}

func TestCompleteUnitNotFound(t *testing.T) {
	orderID := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(models.ProductionOrder{}, pgx.ErrNoRows).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, orderID.String())
	assert.ErrorIs(t, err, ErrProductionOrderNotFound)
}

func TestCompleteUnitMalformedID(t *testing.T) {
	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), uuid.New().String())

	_, _, err := svc.Complete(ctx, "not-a-uuid")
	assert.ErrorIs(t, err, ErrInvalidRequest)
	prod.AssertNotCalled(t, "GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUnitHappyPath(t *testing.T) {
	orderID := uuid.New()
	planned := testOrder(orderID, uuid.New(), models.ProductionOrderStatusTypePlanned)
	inProgress := planned
	inProgress.Status = models.ProductionOrderStatusTypeInProgress

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(planned, nil).Once()
	prod.On("UpdateProductionOrderStatus", mock.Anything, mock.Anything, orderID, models.ProductionOrderStatusTypeInProgress).Return(inProgress, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	got, err := svc.Start(context.Background(), orderID.String())
	require.NoError(t, err)
	assert.Equal(t, models.ProductionOrderStatusTypeInProgress, got.Status)
	prod.AssertExpectations(t)
}

func TestStartUnitRejectsNonPlannedOrder(t *testing.T) {
	orderID := uuid.New()
	order := testOrder(orderID, uuid.New(), models.ProductionOrderStatusTypeInProgress)

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(order, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	_, err := svc.Start(context.Background(), orderID.String())
	assert.ErrorIs(t, err, ErrInvalidProductionOrderState)
	prod.AssertNotCalled(t, "UpdateProductionOrderStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUnitNotFound(t *testing.T) {
	orderID := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	prod.On("GetProductionOrderByIDForUpdate", mock.Anything, mock.Anything, orderID).Return(models.ProductionOrder{}, pgx.ErrNoRows).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	_, err := svc.Start(context.Background(), orderID.String())
	assert.ErrorIs(t, err, ErrProductionOrderNotFound)
}

func TestCreateUnitHappyPath(t *testing.T) {
	outputProductID := uuid.New()
	inputBatchID := uuid.New()
	createdBy := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	products.On("GetProductByID", mock.Anything, mock.Anything, outputProductID).
		Return(models.Product{ID: outputProductID, ProductType: models.ProductTypeFinishedGood}, nil).Once()
	batchesMock.On("GetBatchByID", mock.Anything, mock.Anything, inputBatchID).
		Return(models.InventoryBatch{ID: inputBatchID, ProductID: uuid.New()}, nil).Once()
	products.On("GetProductByID", mock.Anything, mock.Anything, mock.Anything).
		Return(models.Product{ID: uuid.New(), ProductType: models.ProductTypeWhiteLabel}, nil).Once()

	outputBatch := models.InventoryBatch{ID: uuid.New()}
	batchesMock.On("CreateBatch", mock.Anything, mock.Anything, mock.MatchedBy(func(p repositories.CreateBatchParams) bool {
		return p.ProductID == outputProductID && p.Status == models.BatchStatusTypeReserved
	})).Return(outputBatch, nil).Once()

	order := testOrder(uuid.New(), outputBatch.ID, models.ProductionOrderStatusTypePlanned)
	prod.On("CreateProductionOrder", mock.Anything, mock.Anything, mock.MatchedBy(func(p repositories.CreateProductionOrderParams) bool {
		return p.OutputBatchID == outputBatch.ID && p.CreatedBy == createdBy
	})).Return(order, nil).Once()

	lineItem := models.ProductionOrderLineItem{ID: uuid.New()}
	prod.On("CreateProductionOrderLineItem", mock.Anything, mock.Anything, mock.MatchedBy(func(p repositories.CreateProductionOrderLineItemParams) bool {
		return p.ProductionOrderID == order.ID && p.InputBatchID == inputBatchID
	})).Return(lineItem, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), createdBy.String())

	gotOrder, gotItems, err := svc.Create(ctx, &schemas.CreateProductionOrderRequest{
		OutputProductID:   outputProductID.String(),
		OutputBatchNumber: "B-001",
		OutputQuantity:    "10.0000",
		LineItems: []schemas.ProductionLineItemRequest{{
			InputBatchID:     inputBatchID.String(),
			QuantityConsumed: "2.0000",
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, order.ID, gotOrder.ID)
	require.Len(t, gotItems, 1)
	assert.Equal(t, lineItem.ID, gotItems[0].ID)
	batchesMock.AssertExpectations(t)
	prod.AssertExpectations(t)
}

func TestCreateUnitRejectsNonFinishedGoodOutput(t *testing.T) {
	outputProductID := uuid.New()
	createdBy := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	products.On("GetProductByID", mock.Anything, mock.Anything, outputProductID).
		Return(models.Product{ID: outputProductID, ProductType: models.ProductTypeRawMaterial}, nil).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), createdBy.String())

	_, _, err := svc.Create(ctx, &schemas.CreateProductionOrderRequest{
		OutputProductID: outputProductID.String(),
		LineItems:       []schemas.ProductionLineItemRequest{},
	})
	assert.ErrorIs(t, err, ErrInvalidProductionInput)
	batchesMock.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything, mock.Anything)
}

func TestCreateUnitUnknownOutputProduct(t *testing.T) {
	outputProductID := uuid.New()
	createdBy := uuid.New()

	products := new(MockProductStore)
	batchesMock := new(MockBatchStore)
	prod := new(MockProductionOrderStore)
	stock := new(MockStockTransactionStore)

	products.On("GetProductByID", mock.Anything, mock.Anything, outputProductID).Return(models.Product{}, pgx.ErrNoRows).Once()

	svc := newMockProductionService(products, batchesMock, prod, stock)
	ctx := middleware.WithUserID(context.Background(), createdBy.String())

	_, _, err := svc.Create(ctx, &schemas.CreateProductionOrderRequest{
		OutputProductID: outputProductID.String(),
		LineItems:       []schemas.ProductionLineItemRequest{},
	})
	assert.ErrorIs(t, err, ErrProductNotFound)
}
