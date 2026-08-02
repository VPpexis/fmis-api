# Food Manufacturing Inventory System API
## Architectural Design & Production Blueprint

---

## Executive Summary & Objectives

This document serves as the master technical blueprint for designing, implementing, and deploying a production-grade **Inventory System API** tailored for food manufacturing businesses, built in **Go**.

### Project Goals
* **API Design Excellence:** Enforce RESTful design patterns, strict request validation via `go-playground/validator`, and a disciplined layered architecture (routers → schemas → services → repositories).
* **Manufacturing Business Domain:** Solve complex domain challenges including batch/lot tracking, expiration management, FEFO (First-Expired, First-Out) stock consumption, Bill of Materials (BOM) assembly, and immutable audit logs.
* **Data Integrity & Concurrency:** Implement pessimistic row-level locking (`SELECT ... FOR UPDATE`) for inventory deductions to prevent race conditions and overselling in multi-worker environments.
* **Security:** Enforce JWT-based authentication with role-based access control (RBAC) — Admin, Operator, Viewer.
* **Production Engineering Standards:** Enforce strict environment configuration via `caarlos0/env`, database migration pipelines using `atlasgo/atlas`, multi-stage Docker containerization, and comprehensive automated testing with `testify`.
* **CI/CD & Cloud Infrastructure:** Implement automated GitHub Actions workflows and deploy containerized workloads to AWS (ECR, ECS Fargate / App Runner, EC2 + EBS for PostgreSQL).

---

## Roadmap Overview (4 Core Phases)

```text
┌─────────────────────────────────────────────────────────────────┐
│              PHASE 1: API & DOMAIN DESIGN                       │
│  - Entity Schema (Product, Batch, Transaction, Production,      │
│    Line Items, Users)                                           │
│  - Product Classifications (Raw, Packaging, Finished,           │
│    White-Label)                                                 │
│  - FEFO Logic, Expiration Rules & Concurrency Specs             │
│  - JWT Authentication & RBAC Authorization Model                │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│           PHASE 2: PRODUCTION BEST PRACTICES                    │
│  - Configuration via caarlos0/env (.env)                        │
│  - Database Migrations with atlasgo/atlas                       │
│  - Multi-stage Docker Containerization & Docker Compose         │
│  - Go Library Stack & Testing with testify + httptest           │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│             PHASE 3: CI/CD PIPELINE (GITHUB ACTIONS)            │
│  - Linting (golangci-lint), Automated Tests (go test)           │
│  - Ephemeral PostgreSQL Service Container for Integration Tests │
│  - Image Build & Push to AWS ECR, Deployment Triggers           │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│          PHASE 4: AWS CLOUD ARCHITECTURE (PoC)                  │
│  - ECS Fargate / App Runner, EC2 + EBS (PostgreSQL), CloudWatch │
└─────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: API & Domain Design

### 1. Product Classification Matrix

In food production, tracking packaging (labels, jars) is as critical as raw ingredients. White-label items represent pre-bought products that serve as production inputs; after assembly (stickering/rebranding), a new `FINISHED_GOOD` batch is created for sale.

| Product Category | Description | Purchasable? | Sellable? | Expiration Date Required? |
| --- | --- | --- | --- | --- |
| **`RAW_MATERIAL`** | Raw ingredients (Flour, Sugar, Milk) | Yes | No | Yes |
| **`PACKAGING`** | Jars, boxes, printed sticker rolls | Yes | No | No |
| **`FINISHED_GOOD`** | In-house manufactured food items | No | Yes | Yes |
| **`WHITE_LABEL`** | Pre-made products bought as production inputs for rebranding/assembly | Yes | **No** | Yes |

> **Note:** `WHITE_LABEL` products are **not directly sellable**. They must go through a Production Order to be assembled (combined with packaging) into a `FINISHED_GOOD` output batch before sale.

### 2. White-Label & Assembly Handling Strategy

White-label items require a "Production Order" workflow to combine the base product with packaging (stickers) to create the final sellable good.

* **Receiving:** White-label items are received as an `INCOMING` batch under a `WHITE_LABEL` product. Branded sticker rolls are received as an `INCOMING` batch under a `PACKAGING` product.
* **Production Order Creation:** A `PRODUCTION_ORDER` is created with its line items defining the target output batch (a `FINISHED_GOOD` product) and the required input batches with their consumed quantities.
* **Execution & Consumption:** Completing the production order triggers a single atomic transaction that:
  1. Acquires pessimistic locks on all input batches (`SELECT ... FOR UPDATE`).
  2. Deducts the `WHITE_LABEL` batch quantity (Transaction: `USED_IN_PRODUCTION`).
  3. Deducts the `PACKAGING` batch quantity (Transaction: `USED_IN_PRODUCTION`).
  4. Creates/Activates the `FINISHED_GOOD` output batch with the calculated expiration date.
  5. Links all transactions to the production order ID for audit trail.

#### Expiration Date Rule for White-Label Assembly

The output `FINISHED_GOOD` batch inherits the **earliest expiration date** among all input batches that have one:

```
output_expiration_date = MIN(white_label_batch.expiration_date, packaging_batch.expiration_date_if_present)
```

Since `PACKAGING` items typically have no expiration date (`NULL`), this effectively means the output batch takes the white-label item's original manufacturer expiration date. The rule can be expressed formally as:

- If **all** input batches have `NULL` expiration dates → the output batch expiration date is `NULL` (should not happen for food items; raises a validation warning).
- Otherwise → `output_expiration_date = MIN(all_non_null_input_expiration_dates)`.

### 3. Entity-Relationship Schema Chart

All quantity fields use `DECIMAL(10,4)` for exact numerical precision — no floating-point rounding errors.

```text
  +----------------------------------------------------------+
  |                        PRODUCTS                          |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  |     | sku              | VARCHAR (Unique, Indexed)      |
  |     | name             | VARCHAR                        |
  |     | unit_of_measure  | VARCHAR ("kg", "units", "pcs") |
  |     | product_type     | ENUM (RAW_MATERIAL, PACKAGING, |
  |     |                  |       FINISHED_GOOD, WHITE_    |
  |     |                  |       LABEL)                   |
  |     | is_purchasable   | BOOLEAN                        |
  |     | is_sellable      | BOOLEAN                        |
  |     | created_at       | TIMESTAMP                      |
  |     | updated_at       | TIMESTAMP                      |
  +----------------------------------------------------------+
                            | 1
                            | HAS MANY
                            v N
  +----------------------------------------------------------+
  |                   INVENTORY_BATCHES                      |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  | FK  | product_id       | UUID -> products.id            |
  |     | batch_number     | VARCHAR (Indexed)              |
  |     | quantity_initial | DECIMAL(10,4) (Original qty)   |
  |     | quantity_current | DECIMAL(10,4) (Available qty)  |
  |     | status           | ENUM (ACTIVE, DEPLETED,        |
  |     |                  |       QUARANTINED, EXPIRED)    |
  |     | expiration_date  | TIMESTAMP (NOT NULL for        |
  |     |                  | RAW_MATERIAL, FINISHED_GOOD,   |
  |     |                  | WHITE_LABEL; NULLABLE for      |
  |     |                  | PACKAGING)                     |
  |     | created_at       | TIMESTAMP                      |
  |     | updated_at       | TIMESTAMP                      |
  +----------------------------------------------------------+
                            | 1
                            | HAS MANY
                            v N
  +----------------------------------------------------------+
  |                   STOCK_TRANSACTIONS                     |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  | FK  | batch_id         | UUID -> batches.id             |
  | FK  | production_order | UUID -> production_orders.id   |
  |     |                  | (Nullable)                     |
  |     | quantity_change  | DECIMAL(10,4) (+ / -)          |
  |     | transaction_type | ENUM (INCOMING, OUTGOING,      |
  |     |                  |       USED_IN_PRODUCTION,       |
  |     |                  |       WASTE, ADJUSTMENT)        |
  | FK  | performed_by     | UUID -> users.id               |
  |     | reference_note   | VARCHAR                        |
  |     | created_at       | TIMESTAMP                      |
  +----------------------------------------------------------+

  +----------------------------------------------------------+
  |                   PRODUCTION_ORDERS                      |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  | FK  | output_batch_id  | UUID -> batches.id (the result)|
  |     | status           | ENUM (PLANNED, IN_PROGRESS,    |
  |     |                  |       COMPLETED, CANCELLED)     |
  | FK  | created_by       | UUID -> users.id               |
  |     | created_at       | TIMESTAMP                      |
  |     | completed_at     | TIMESTAMP (Nullable)           |
  +----------------------------------------------------------+
                            | 1
                            | HAS MANY
                            v N
  +----------------------------------------------------------+
  |              PRODUCTION_ORDER_LINE_ITEMS                 |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  | FK  | production_order | UUID -> production_orders.id   |
  |     | id               |                                |
  | FK  | input_batch_id   | UUID -> inventory_batches.id   |
  |     | quantity_consumed| DECIMAL(10,4)                  |
  |     | created_at       | TIMESTAMP                      |
  +----------------------------------------------------------+

  +----------------------------------------------------------+
  |                         USERS                            |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  |     | username         | VARCHAR (Unique, Indexed)      |
  |     | email            | VARCHAR (Unique, Indexed)      |
  |     | password_hash    | VARCHAR (bcrypt)               |
  |     | role             | ENUM (ADMIN, OPERATOR, VIEWER) |
  |     | is_active        | BOOLEAN (default: true)        |
  |     | created_at       | TIMESTAMP                      |
  |     | updated_at       | TIMESTAMP                      |
  +----------------------------------------------------------+

  +----------------------------------------------------------+
  |                    REFRESH_TOKENS                        |
  +----------------------------------------------------------+
  | PK  | id               | UUID (PK)                      |
  | FK  | user_id          | UUID -> users.id               |
  |     | token_hash       | VARCHAR (SHA-256 hashed token) |
  |     | expires_at       | TIMESTAMP                      |
  |     | revoked          | BOOLEAN (default: false)       |
  |     | created_at       | TIMESTAMP                      |
  +----------------------------------------------------------+
```

### 4. Application Layering Principles (Go-Specific)

* **`cmd/api/` (Application Entry Point):** Bootstraps configuration, database connection pool, dependency injection, and starts the HTTP server.
* **`internal/routers/` (API Layer):** Defines `chi` route groups. Handles HTTP request ingestion, parameter parsing, validation error formatting, and delegates to services. Contains zero raw SQL.
* **`internal/schemas/` (Validation Layer):** Go structs with `go-playground/validator` tags enforcing payload structure, business rules, and Unit of Measure (UOM) consistency. Each schema maps to a request or response DTO.
* **`internal/services/` (Service Layer):** Contains all domain calculations. Enforces **pessimistic locking** (`SELECT ... FOR UPDATE`) on batches during FEFO consumption and production execution. Begins and commits/rollbacks database transactions.
* **`internal/repositories/` (Data Access):** Executes parameterized SQL queries via `pgx` directly (or `sqlc`-generated queries). Each method receives a `*sql.Tx` or `*pgxpool.Pool` from the service layer.
* **`internal/middleware/` (Cross-Cutting):** JWT authentication middleware, RBAC authorization guard, request logging, CORS, panic recovery.

### 5. Core Endpoint Specifications

#### A. Authentication & Authorization Domain (`/api/v1/auth`)
* **`POST /api/v1/auth/register`**: Registers a new user. Returns access + refresh tokens.
* **`POST /api/v1/auth/login`**: Authenticates credentials, returns access + refresh JWT tokens.
* **`POST /api/v1/auth/refresh`**: Exchanges a valid refresh token for a new access token.
* **`GET /api/v1/auth/me`**: Returns the authenticated user's profile (requires valid access token).
* **`POST /api/v1/auth/logout`**: Revokes the refresh token.

#### B. Products Catalog Domain (`/api/v1/products`)
* **`POST /api/v1/products/`** `[ADMIN, OPERATOR]`: Creates a new product catalog item.
* **`GET /api/v1/products/`** `[ALL]`: Paginated listing with optional filtering by `product_type`.
* **`GET /api/v1/products/{product_id}`** `[ALL]`: Retrieves metadata for a single catalog item.
* **`PATCH /api/v1/products/{product_id}`** `[ADMIN]`: Updates product fields (except type).
* **`DELETE /api/v1/products/{product_id}`** `[ADMIN]`: Soft-deletes a product if no active batches exist.

#### C. Batch Management Domain (`/api/v1/batches`)
* **`POST /api/v1/batches/`** `[ADMIN, OPERATOR]`: Logs a newly received lot. Validates mandatory expiration date for `RAW_MATERIAL`, `FINISHED_GOOD`, and `WHITE_LABEL` product types (rejects `NULL`). Sets `status = ACTIVE`, writes an initial `INCOMING` stock transaction.
* **`GET /api/v1/batches/product/{product_id}`** `[ALL]`: Lists active batches sorted by `expiration_date ASC NULLS LAST` (FEFO order).
* **`PATCH /api/v1/batches/{batch_id}/quarantine`** `[ADMIN, OPERATOR]`: Moves a batch to `QUARANTINED` status.

#### D. Inventory Ledger Domain (`/api/v1/inventory`)
* **`POST /api/v1/inventory/consume`** `[ADMIN, OPERATOR]`: Executes FEFO stock consumption across active batches using `SELECT ... FOR UPDATE`. Logs immutable `OUTGOING` transaction entries. Rejects if insufficient stock.
* **`POST /api/v1/inventory/adjust`** `[ADMIN, OPERATOR]`: Records stock adjustments (spoilage, damaged labels) as `WASTE` or `ADJUSTMENT`.
* **`GET /api/v1/inventory/transactions/`** `[ALL]`: Queries historical transaction audit trails with optional filters by batch, type, date range.

#### E. Production & Assembly Domain (`/api/v1/production`)
* **`POST /api/v1/production/`** `[ADMIN, OPERATOR]`: Creates a production order with status `PLANNED`. Accepts an array of line items specifying input batches and consumed quantities, plus the target output batch details (product ID, quantity, expiration date).
* **`POST /api/v1/production/{id}/start`** `[ADMIN, OPERATOR]`: Transitions status to `IN_PROGRESS`.
* **`POST /api/v1/production/{id}/complete`** `[ADMIN, OPERATOR]`: Atomically locks all input batches, deducts quantities (`USED_IN_PRODUCTION`), finalizes the output batch, sets status to `COMPLETED`, and links all transactions to the production order ID. Full rollback on any failure.
* **`POST /api/v1/production/{id}/cancel`** `[ADMIN]`: Cancels a `PLANNED` or `IN_PROGRESS` order. No inventory impact.
* **`GET /api/v1/production/`** `[ALL]`: Lists production orders with optional status filter.
* **`GET /api/v1/production/{id}`** `[ALL]`: Retrieves a single production order with its line items.

#### F. Infrastructure Domain
* **`GET /health`**: Unauthenticated health check for AWS load balancer probes. Returns DB connectivity status.
* **`GET /docs`**: Interactive OpenAPI/Swagger UI (generated via `swaggo/swag` annotations).

#### G. RBAC Summary

| Role | Create/Update Products | Manage Batches | Inventory Ops | Production Ops | User Mgmt |
| --- | --- | --- | --- | --- | --- |
| **ADMIN** | Full | Full | Full | Full | Full |
| **OPERATOR** | Create only | Full | Full | Full | Read self |
| **VIEWER** | Read only | Read only | Read only | Read only | Read self |

---

## Phase 2: Production Best Practices & Setup

### 1. Go Library Stack

| Concern | Library | Rationale |
| --- | --- | --- |
| **HTTP Router** | `go-chi/chi/v5` | Idiomatic, stdlib-compatible, middleware-friendly, lightweight |
| **Database Driver** | `jackc/pgx/v5` | High-performance PostgreSQL driver with connection pooling |
| **SQL Code Generation** | `sqlc-dev/sqlc` | Generates type-safe Go code from SQL; eliminates ORM overhead |
| **Request Validation** | `go-playground/validator/v10` | Struct-tag-based validation with custom rule support |
| **Configuration** | `caarlos0/env/v11` | Parses env vars into Go structs, 12-factor compliant |
| **Database Migrations** | `atlasgo/atlas` | Declarative schema management, auto-generates migration diffs |
| **Authentication** | `golang-jwt/jwt/v5` | JWT signing and verification |
| **Password Hashing** | `golang.org/x/crypto/bcrypt` | Industry-standard password hashing |
| **JSON/API** | `encoding/json` + `go-chi/render` | Stdlib JSON + chi's response helpers |
| **Logging** | `log/slog` (stdlib) | Structured logging, no external dependency |
| **Testing** | `stretchr/testify` + `net/http/httptest` | Assertions, mocks, HTTP test server |
| **Linting** | `golangci-lint` | Aggregated Go linters in a single tool |
| **API Documentation** | `swaggo/swag` + `swaggo/http-swagger` | Generates OpenAPI spec from code annotations |
| **UUID** | `google/uuid` | UUID v4 generation for primary keys |

### 2. Twelve-Factor Configuration Management

* Uses `caarlos0/env` to parse environment variables into a typed `Config` struct.
* Reads `.env` file locally; overridden by real environment variables in AWS (ECS task definition or Secrets Manager).
* Missing required variables (e.g., `DATABASE_URL`, `JWT_SECRET`) cause the application to fail fast at startup with a clear error message.

```go
// internal/config/config.go
type Config struct {
    Port           int           `env:"PORT" envDefault:"8080"`
    DatabaseURL    string        `env:"DATABASE_URL,required"`
    JWTSecret      string        `env:"JWT_SECRET,required"`
    AccessTokenTTL time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
    RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"7d"`
    LogLevel       string        `env:"LOG_LEVEL" envDefault:"info"`
}
```

### 3. Database Schema Migrations (Atlas)

* All schema changes are defined declaratively in an Atlas HCL file (`schema.hcl`) or SQL files.
* Atlas inspects the target database, computes the diff against the desired schema, and generates migration scripts.
* Migrations are applied automatically by the CI/CD pipeline before new application containers go live.
* Rollbacks are supported via Atlas's versioned migration directory.

```bash
# Generate migration from declarative schema
atlas migrate diff --env local

# Apply migrations
atlas migrate apply --env production
```

### 4. Concurrency & Data Integrity

* **Pessimistic Locking:** Inventory and production services execute `SELECT ... FOR UPDATE` when querying batches for deduction. This serializes concurrent access to the same batch rows, preventing race conditions.
* **Atomic Transactions:** All multi-table writes (e.g., deducting 3 batches for a production order) are wrapped in a single `pgx` transaction with explicit `tx.Rollback()` on error.
* **Idempotency:** Production order completion checks current status before executing; duplicate "complete" calls are rejected.

```go
// Example: FEFO consumption with pessimistic lock
tx, _ := pool.Begin(ctx)
defer tx.Rollback(ctx)

rows, _ := tx.Query(ctx,
    `SELECT id, quantity_current FROM inventory_batches
     WHERE product_id = $1 AND status = 'ACTIVE'
     ORDER BY expiration_date ASC NULLS LAST
     FOR UPDATE`, productID)
// ... deduct in FEFO order ...
tx.Commit(ctx)
```

### 5. Authentication & Authorization Flow

1. **Registration / Login** → Server returns `access_token` (short-lived, 15 min) and `refresh_token` (long-lived, 7 days, stored hashed in DB).
2. **Every subsequent request** → Client sends `Authorization: Bearer <access_token>`.
3. **Auth Middleware** → Validates JWT signature and expiry, extracts `user_id` and `role` into request context.
4. **RBAC Middleware** → Checks the required role against the user's role before allowing the handler to execute.
5. **Token Refresh** → When `access_token` expires, client calls `/auth/refresh` with the `refresh_token` to get a new pair.

### 6. Containerization (Multi-Stage Dockerfile)

* **Builder Stage:** Compiles the Go binary inside a full Go image (`golang:1.23-alpine`), downloading dependencies and running `go build -ldflags="-s -w"`.
* **Runtime Stage:** Copies the statically-linked binary into a **scratch** (distroless) or `alpine:3.21` image. Runs under an unprivileged non-root user.
* **Health Checks:** Exposes a `/health` endpoint for Docker and AWS load balancer probes.

### 7. Repository Structure

```text
food-inventory-api/
├── .github/workflows/
│   └── deploy.yml
├── cmd/
│   └── api/
│       └── main.go                  # Entry point, DI wiring, server start
├── internal/
│   ├── config/
│   │   └── config.go                # Env-based config struct
│   ├── database/
│   │   ├── postgres.go              # pgx pool initialization
│   │   └── queries/                 # sqlc-generated query code
│   ├── middleware/
│   │   ├── auth.go                  # JWT verification middleware
│   │   ├── rbac.go                  # Role-based access guard
│   │   └── logging.go               # Request/response logging
│   ├── models/
│   │   └── models.go                # Domain structs (Product, Batch, etc.)
│   ├── repositories/
│   │   ├── products.go
│   │   ├── batches.go
│   │   ├── transactions.go
│   │   ├── production.go
│   │   └── users.go
│   ├── routers/
│   │   ├── router.go                # chi router setup, route registration
│   │   ├── auth.go
│   │   ├── products.go
│   │   ├── batches.go
│   │   ├── inventory.go
│   │   └── production.go
│   ├── schemas/
│   │   ├── auth.go                  # Login/Register request/response DTOs
│   │   ├── products.go
│   │   ├── batches.go
│   │   ├── inventory.go
│   │   └── production.go
│   └── services/
│       ├── auth.go                  # Token generation, password hashing
│       ├── inventory.go             # FEFO consumption logic
│       ├── production.go            # Production order execution
│       └── fefo.go                  # FEFO sorting/selection algorithm
├── migrations/
│   └── schema.hcl                    # Atlas declarative schema definition
├── docs/
├── tests/
│   ├── integration/
│   │   ├── test_products.go
│   │   ├── test_inventory.go
│   │   └── test_production.go
│   └── testdata/
│       └── seed.sql                 # Test database seed data
├── docker-compose.yml               # Local dev: API + PostgreSQL
├── Dockerfile                        # Multi-stage production build
├── .env.example                      # Template for local env vars
├── atlas.hcl                         # Atlas project configuration
├── go.mod
├── go.sum
├── sqlc.yaml                         # sqlc code generation config
└── .golangci.yml                     # golangci-lint configuration
```

### 8. Docker Compose (Local Development)

```yaml
# docker-compose.yml
version: "3.9"

services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: fmis
      POSTGRES_PASSWORD: fmis_dev
      POSTGRES_DB: fmis_db
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U fmis -d fmis_db"]
      interval: 5s
      timeout: 5s
      retries: 5

  api:
    build:
      context: .
      dockerfile: Dockerfile
      target: dev                         # Dev stage with air/delve for hot reload
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: "postgres://fmis:fmis_dev@db:5432/fmis_db?sslmode=disable"
      JWT_SECRET: "dev-secret-change-in-production"
      PORT: "8080"
    depends_on:
      db:
        condition: service_healthy
    volumes:
      - .:/app                            # Live reload mount

volumes:
  pgdata:
```

---

## Phase 3: CI/CD Pipeline (GitHub Actions)

### 1. Linting & Quality
* **`golangci-lint`** runs against all Go source files.
* **`go vet`** checks for suspicious constructs.
* **`go mod tidy`** diff check ensures dependency files are up to date.
* **`git diff`** check ensures generated code is committed or matches expected state.

### 2. Automated Testing with Ephemeral PostgreSQL

* GitHub Actions spins up a **PostgreSQL service container** alongside the test runner.
* Integration tests connect to this ephemeral database (separate from any shared environment).
* Atlas migrations are applied to the test database before tests run.
* Tests execute via `go test ./... -v -race`:
  * **Unit tests:** Service-level logic using mocked repositories (testify mocks).
  * **Integration tests:** Full HTTP lifecycle tests using `httptest.Server` against a real PostgreSQL instance, verifying transactional locking and FEFO logic.

```yaml
# In GitHub Actions workflow
services:
  postgres:
    image: postgres:16-alpine
    env:
      POSTGRES_USER: test
      POSTGRES_PASSWORD: test
      POSTGRES_DB: fmis_test
    ports:
      - 5432:5432
    options: >-
      --health-cmd pg_isready
      --health-interval 10s
      --health-timeout 5s
      --health-retries 5
```

> **Important:** The test database must use PostgreSQL (not SQLite). SQLite's locking behavior differs from PostgreSQL and cannot validate `SELECT ... FOR UPDATE` semantics correctly.

### 3. Build & Package
* **Docker Build:** Multi-stage `docker build` targeting the production stage.
* **Image Tagging:** Tags images with `git sha` and `latest`, then pushes to **AWS ECR**.

### 4. Deployment
* **AWS App Runner** or **ECS Fargate** triggers a zero-downtime rolling update using the new image tag.
* Before deployment, CI runs Atlas migrations against the target RDS/EC2 PostgreSQL instance.

---

## Phase 4: AWS Cloud Architecture (PoC)

### Target Stack

| Component | AWS Service | Purpose |
| --- | --- | --- |
| **Compute** | **AWS App Runner / ECS Fargate** | Serverless container execution with auto-scaling. App Runner for simplicity; Fargate for finer control. |
| **Database** | **EC2 + EBS (PostgreSQL)** | Self-managed PostgreSQL on EC2 with EBS volumes. Lower cost than RDS for PoC/early-stage workloads. Snapshot backups via EBS snapshots. |
| **Container Registry** | **AWS ECR** | Private Docker image registry with vulnerability scanning. |
| **Secret Management** | **AWS Secrets Manager** | Encrypted storage for `DATABASE_URL`, `JWT_SECRET`, and other credentials. Injected as env vars at container startup. |
| **Monitoring** | **AWS CloudWatch** | Centralized application logging (structured JSON via `slog`) and container CPU/memory metrics. |
| **Networking** | **VPC + ALB** | Application Load Balancer routes traffic to ECS/App Runner. Security groups restrict database access to the application tier only. |
| **DNS** | **Route 53** | (Optional, future) Custom domain routing. |
| **CI/CD** | **GitHub Actions** | Builds, tests, pushes images, triggers deployment. |

---

## Appendix: Key Technology Decisions

| Decision | Choice | Reasoning |
| --- | --- | --- |
| **Language** | Go | Single-binary deployment, excellent concurrency, fast cold starts, strong standard library |
| **DB Driver** | pgx v5 | Fastest PostgreSQL driver for Go, native connection pooling |
| **API Style** | REST + chi | Lightweight, idiomatic, no magic; middleware ecosystem mature |
| **PK Type** | UUID v4 | Avoids sequential ID guessing; safe for distributed systems |
| **Quantity Type** | DECIMAL(10,4) | Prevents IEEE 754 floating-point rounding errors in inventory math |
| **Auth** | JWT (stateless) + refresh tokens | Scalable, no server-side session store needed for access tokens |
| **CI DB** | Ephemeral PostgreSQL | Real PostgreSQL locking behavior for integration tests |
