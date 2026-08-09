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

// GetProductByID fetches a product by its primary key.
func (r *ProductRepository) GetProductByID(ctx context.Context, q Querier, id uuid.UUID) (models.Product, error) {
	row := q.QueryRow(ctx, `
		SELECT id, sku, name, unit_of_measure, product_type, is_purchasable, is_sellable, created_at, updated_at
		FROM products
		WHERE id = $1`,
		id,
	)
	return scanProduct(row)
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
