// Package repositories executes parametrized SQL queries against PostgreSQL.
package repositories

import (
	"context"
	"fmis-api/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ProductRepository reads and writes the products table.
type ProductRepository struct{}

// CreateProductParams carries the values needed to insert a new product.
type CreateProductParams struct {
	SKU, Name, UnitOfMeasure  string
	ProductType               models.ProductType
	IsPurchasable, IsSellable bool
}

// UpdateProductParams carries the values needed to update a product.
type UpdateProductParams struct {
	SKU, Name, UnitOfMeasure  *string
	IsPurchasable, IsSellable *bool
}

// ListProductParams is the payload for GET /api/v1/products.
type ListProductParams struct {
	ProductType *models.ProductType
	Limit       int
	Offset      int
}

// ListProducts returns all products.
func (r *ProductRepository) ListProducts(ctx context.Context, q Querier, p ListProductParams) ([]models.Product, error) {
	rows, err := q.Query(ctx, `
		SELECT id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable, created_at, updated_at 
		FROM products
		WHERE deleted_at IS NULL
			AND ($1::product_type IS NULL OR product_type = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		p.ProductType, p.Limit, p.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]models.Product, 0)
	for rows.Next() {
		pr, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, pr)
	}
	return products, rows.Err()
}

// GetProductByID fetches a product by its primary key.
func (r *ProductRepository) GetProductByID(ctx context.Context, q Querier, id uuid.UUID) (models.Product, error) {
	row := q.QueryRow(ctx, `
		SELECT id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable, created_at, updated_at
		FROM products
		WHERE id = $1
		AND deleted_at IS NULL`,
		id,
	)
	return scanProduct(row)
}

// CreateProduct creates a new product.
func (r *ProductRepository) CreateProduct(ctx context.Context, q Querier, p CreateProductParams) (models.Product, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO products (sku, name, unit_of_measure, product_type, is_purchasable, is_sellable)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable, created_at, updated_at`,
		p.SKU, p.Name, p.UnitOfMeasure, p.ProductType, p.IsPurchasable, p.IsSellable,
	)
	return scanProduct(row)
}

// UpdateProduct updates a product by its primary key.
func (r *ProductRepository) UpdateProduct(ctx context.Context, q Querier, id uuid.UUID, p UpdateProductParams) (models.Product, error) {
	row := q.QueryRow(ctx, `
		UPDATE products SET
			sku = COALESCE($2, sku),
			name = COALESCE($3, name),
			unit_of_measure = COALESCE($4, unit_of_measure),
			is_purchasable = COALESCE($5, is_purchasable),
			is_sellable = COALESCE($6, is_sellable),
			updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable, created_at, updated_at`,
		id, p.SKU, p.Name, p.UnitOfMeasure, p.IsPurchasable, p.IsSellable,
	)
	return scanProduct(row)
}

// SoftDeleteProduct soft-deletes a product by ID.
func (r *ProductRepository) SoftDeleteProduct(ctx context.Context, q Querier, id uuid.UUID) (bool, error) {
	ct, err := q.Exec(ctx, `
		UPDATE products
		SET deleted_at = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// scanProduct maps one products row into a models.Product.
func scanProduct(row pgx.Row) (models.Product, error) {
	var p models.Product
	err := row.Scan(
		&p.ID, &p.SKU, &p.Name, &p.UnitOfMeasure, &p.ProductType,
		&p.IsPurchasable, &p.IsSellable, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}
