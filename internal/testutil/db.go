// Package testutil provides shared helpers for PostgreSQL integration tests.
package testutil

import (
	"context"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool connects to the integration test databse, skipping the test when
// PostgreSQL is unreachable so DB-less environments staygreen.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		//nolint:gosec // localhost-only dev DB; credentials already public in atlas.hcl, CI overrides via DATABASE_URL
		url = "postgres://fmis:fmis_dev@localhost:5432/fmis_test?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("integration test skipped: no PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("integration test skipped no PostgreSQL: %v", err)
	}
	return pool
}

// ResetDB empties every domain table so each test starts from a clean slate.
func ResetDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		TRUNCATE stock_transactions, production_order_line_items, production_orders,
			inventory_batches, products, users CASCADE`); err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

// CreateUser inserts a uique user and returns it.
// TODO: Can upgrade to have custom role in parameters.
func CreateUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, role models.UserRoleType) models.User {
	t.Helper()
	user, err := (&repositories.UserRepository{}).CreateUser(ctx, pool, repositories.CreateUserParams{
		Username:     "tester-" + uuid.NewString()[:8],
		Email:        uuid.NewString()[:8] + "@example.com",
		PasswordHash: "unused",
		Role:         role,
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user
}

// CreateProduct inerst a product with a unique SKU and returns its ID and type.
func CreateProduct(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ptype models.ProductType) models.Product {
	t.Helper()
	var p models.Product
	err := pool.QueryRow(ctx, `
		INSERT INTO products (sku, name, unit_of_measure, product_type)
		VALUES ($1, $2, $3, $4)
		RETURNING id, product_type`,
		"SKU-"+uuid.NewString()[:8], "Test product", "kg", ptype,
	).Scan(&p.ID, &p.ProductType)
	if err != nil {
		t.Fatalf("create test product: %v", err)
	}
	return p
}
