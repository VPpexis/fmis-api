// Package testutil provides shared helpers for PostgreSQL integration tests.
package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"testing"
	"time"

	"fmis-api/internal/models"
	"fmis-api/internal/repositories"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultURL points at the local dev database created by `docker compose up db`.
// CI overrides it via DATABASE_URL. Tests isolate themselves in throwaway
// schemas, so pointing at the dev database never touches developer data.
//
//nolint:gosec // localhost-only dev DB; credentials already public in atlas.hcl, CI overrides via DATABASE_URL
const defaultURL = "postgres://fmis:fmis_dev@localhost:5432/fmis_db?sslmode=disable"

// databaseURL returns the integration test database URL.
func databaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return defaultURL
}

// repoRoot resolves the repository root from this file's path so that tests
// find migrations regardless of the working directory (go test runs each
// package binary from its own directory).
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// createSchema creates a fresh, uniquely named schema in the shared database
// and applies every migration inside it. Because the schema (and therefore
// every table) belongs exclusively to the calling test, concurrently running
// test binaries can never observe or truncate one another's rows.
func createSchema(ctx context.Context, url string) (string, error) {
	schema := "it_" + uuid.NewString()[:8]
	admin, connErr := pgx.Connect(ctx, url)
	if connErr != nil {
		return "", connErr
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		return "", fmt.Errorf("create schema %s: %w", schema, err)
	}
	if err := applyMigrations(ctx, admin, schema); err != nil {
		_, _ = admin.Exec(ctx, `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		return "", err
	}
	return schema, nil
}

// applyMigrations executes every SQL migration inside the given schema.
func applyMigrations(ctx context.Context, admin *pgx.Conn, schema string) error {
	files, err := filepath.Glob(filepath.Join(repoRoot(), "migrations", "*.sql"))
	if err != nil {
		return fmt.Errorf("find migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		//nolint:gosec // file comes from the repository's own migrations dir
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		// Atlas generates migrations for the public schema; redirect every
		// schema reference (e.g. "public"."products" or ::public.transaction_type)
		// into this test's schema. Word boundaries leave other identifiers
		// containing "public" (like is_public) untouched.
		sql := regexp.MustCompile(`\bpublic\b`).ReplaceAllString(string(content), schema)
		if _, err := admin.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
	}
	return nil
}

// Pool connects to the integration test database, skipping the test when
// PostgreSQL is unreachable so DB-less environments stay green.
//
// Each call to Pool creates a fresh schema owned by the calling test: all
// tables created through the returned pool live in that schema, and the schema
// is dropped (with its rows) when the test finishes. Concurrent package
// binaries get their own schemas, so no test can truncate or read data that
// another package created.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := databaseURL()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	schema, err := createSchema(ctx, url)
	if err != nil {
		t.Skipf("integration test skipped: no PostgreSQL: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Skipf("integration test skipped: bad DATABASE_URL: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, execErr := conn.Exec(ctx, `SET search_path TO "`+schema+`"`)
		return execErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("integration test skipped: no PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)

	// Drop the schema over a fresh connection so cleanup still works when the
	// test closed pool itself.
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		admin, connErr := pgx.Connect(dropCtx, url)
		if connErr != nil {
			return
		}
		defer func() { _ = admin.Close(dropCtx) }()
		_, _ = admin.Exec(dropCtx, `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
	})

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("integration test skipped no PostgreSQL: %v", err)
	}
	return pool
}

// ResetDB empties every domain table in the caller's own schema so each test
// starts from a clean slate. The truncation is scoped to the schema created
// for the calling test and can never touch another package's rows.
func ResetDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		TRUNCATE stock_transactions, production_order_line_items, production_orders,
			inventory_batches, products, users CASCADE`); err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

// LoadSeed executes the SQL fixture file at relPath (relative to the repository
// root) inside the caller's schema. The pool's search_path already points at
// that schema, so unqualified table names in the seed resolve there.
func LoadSeed(t *testing.T, pool *pgxpool.Pool, relPath string) {
	t.Helper()
	path := filepath.Join(repoRoot(), filepath.FromSlash(relPath))
	//nolint:gosec // path comes from the repository's own test tree
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed %s: %v", relPath, err)
	}
	if _, err := pool.Exec(context.Background(), string(content)); err != nil {
		t.Fatalf("load seed %s: %v", relPath, err)
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
