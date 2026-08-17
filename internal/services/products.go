package services

import (
	"context"
	"errors"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors that router maps to HTTP status code.
var (
	ErrProductHasActiveBatches = errors.New("product has active batches")
	ErrDuplicateSKU            = errors.New("product with this sku already exists")
)

// ProductService owns the business logic for inventory products.
type ProductService struct {
	pool     *pgxpool.Pool
	products *repositories.ProductRepository
	batches  *repositories.BatchRepository
}

// NewProductService creates a new ProductService (constructor).
func NewProductService(pool *pgxpool.Pool) *ProductService {
	return &ProductService{
		pool:     pool,
		products: &repositories.ProductRepository{},
		batches:  &repositories.BatchRepository{},
	}
}

// Create creates a new product.
func (p *ProductService) Create(ctx context.Context, req schemas.CreateProductRequest) (models.Product, error) {
	var product models.Product
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var getErr error
		product, getErr = p.products.CreateProduct(ctx, tx, repositories.CreateProductParams{
			SKU:           req.SKU,
			Name:          req.Name,
			UnitOfMeasure: req.UnitOfMeasure,
			ProductType:   models.ProductType(req.ProductType),
			IsPurchasable: req.IsPurchasable,
			IsSellable:    req.IsSellable,
		})
		if getErr != nil {
			if repositories.IsUniqueViolation(getErr) {
				return ErrDuplicateSKU
			}
			return fmt.Errorf("create product: %w", getErr)
		}
		return nil
	})
	if err != nil {
		return models.Product{}, err
	}
	return product, nil
}

// List returns a page of products.
func (p *ProductService) List(ctx context.Context, productTypeStr, limitStr, offsetStr string) ([]models.Product, error) {
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

	var productType *models.ProductType
	if productTypeStr != "" {
		pt := models.ProductType(productTypeStr)
		productType = &pt
	}

	products, err := p.products.ListProducts(ctx, p.pool, repositories.ListProductParams{
		ProductType: productType,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	return products, nil
}

// GetByID returns a single product by ID.
func (p *ProductService) GetByID(ctx context.Context, productIDStr string) (models.Product, error) {
	productID, err := uuid.Parse(productIDStr)
	if err != nil {
		return models.Product{}, ErrInvalidRequest
	}

	product, getErr := p.products.GetProductByID(ctx, p.pool, productID)
	if getErr != nil {
		if errors.Is(getErr, pgx.ErrNoRows) {
			return models.Product{}, ErrProductNotFound
		}
		return models.Product{}, fmt.Errorf("get product %w", getErr)
	}
	return product, nil
}

// Update updates a product.
func (p *ProductService) Update(ctx context.Context, productIDStr string, req schemas.UpdateProductRequest) (models.Product, error) {
	productID, err := uuid.Parse(productIDStr)
	if err != nil {
		return models.Product{}, ErrInvalidRequest
	}

	product, getErr := p.products.UpdateProduct(ctx, p.pool, productID, repositories.UpdateProductParams{
		SKU:           req.SKU,
		Name:          req.Name,
		UnitOfMeasure: req.UnitOfMeasure,
		IsSellable:    req.IsSellable,
		IsPurchasable: req.IsPurchasable,
	})
	if getErr != nil {
		if errors.Is(getErr, pgx.ErrNoRows) {
			return models.Product{}, ErrProductNotFound
		}
		return models.Product{}, fmt.Errorf("update product: %w", getErr)
	}
	return product, nil
}

// SoftDelete deletes a product if it has no active batches.
func (p *ProductService) SoftDelete(ctx context.Context, productIDStr string) error {
	productID, err := uuid.Parse(productIDStr)
	if err != nil {
		return ErrInvalidRequest
	}

	active, err := p.batches.CountActivateBatches(ctx, p.pool, productID)
	if err != nil {
		return fmt.Errorf("count active batches: %w", err)
	}
	if active > 0 {
		return ErrProductHasActiveBatches
	}

	deleted, getErr := p.products.SoftDeleteProduct(ctx, p.pool, productID)
	if getErr != nil {
		return fmt.Errorf("soft delete product: %w", getErr)
	}
	if !deleted {
		return ErrProductNotFound
	}
	return nil
}
