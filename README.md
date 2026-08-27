# FMIS API - Food Manufacturing Inventory System

A product-grade REST API for food manufacturing inventory management, built in Go.

## Features

- **Batch/Lot Trackings** - Manage raw materials, packaging, finished goods, and white-label products.
- **FEFO Consumption** - First-Expired, First-Out stock deduction logic
- **Production Orders** - White-label assembly with atomic batch consumption
- **RBAC** - `SELECT ... FOR UPDATE` for race-condition-safe inventory deductions
- **Immutable Audit Logs** - All stock movements recorded as transactions.
- **JWT Authentication** - bcrypt-hashed passwords, 15m access tokens + 7d refresh tokens (SHA-256 stored)

## Implementation Status

| Area | Status |
|---|---|
| API entry point (`cmd/api/main.go`) | Done |
| Config (`internal/config`) | Done |
| pgx pool (`internal/database`) | Done |
| chi router + `/health` (`internal/routers`) | Done |
| Atlas schema + migrations | Done |
| Middleware: JWT auth, RBAC, logging, CORS, recovery | Done (issue #16) |
| Domain models (`internal/models`) | Done (issue #19) |
| Auth endpoints: register + login (issue #22) | Done |
| Repositories / schemas / services | In progress (auth domain done) |
| Products / batches / inventory API | Done |
| Production: create + start (issue #46) | Done |
| Production: complete / cancel / list (issues #45, #47) | Planned |
| Unit tests (middleware, auth service) | Done |
| Integration tests (`tests/`, real PostgreSQL) | Planned |
| `sqlc.yaml` | Planned (repositories handwritten for now) |

See `docs/PROJECT_DESIGN.md` for the authoritative spec.
  
## Tech Stack
| Concern | Library | Status |
|---------|---------|--------|
| HTTP Router | `go-chi/chi/v5` | Implemented |
| Database Driver | `jackc/pgx/v5` | Implemented |
| Configuration | `caarlos0/env/v11` | Implemented |
| Migrations | `atlasgo/atlas` | Implemented |
| Auth | `golang-jwt/jwt/v5` + `golang.org/x/crypto/bcrypt` | Implemented |
| Validation | `go-playground/validator/v10` | Implemented |
| Testing | stdlib `testing` (table-driven) | Implemented |
| Linting | `golangci-lint` | Implemented |
| Hot Reload | `air` | Implemented |
| API Docs | `swaggo/swag` + `swaggo/http-swagger` | Implemented (issue #52) |

## Prerequisites

- Go 1.26+
- Docker & Docker Compose
- Atlas CLI (`brew install ariga/tap/atlas`)

## Getting Started

### 1. Clone and env setup

```bash
git clone <repo-url> && cd fmis-api
cp .env.example .env.local # edit as needed
```

### 2. Start the database

```bash
docker compose up db
```

### 3. Run migrations

```bash
atlas migrate apply --env local
```

### 4. Start the API (with hot reload)

```bash
docker compose up api
```

API will be available at `http://localhost:8080`

## Project Structure

```
.
├── .github/workflows/        # CI/CD pipelines
├── cmd/api/                  # Application entry point
├── internal/
│   ├── config/               # Env-based config struct
│   ├── database/             # pgx pool
│   ├── routers/              # chi route groups (auth, helpers)
│   ├── middleware/           # JWT auth, RBAC, logging, CORS, recovery
│   ├── models/               # Domain structs mirroring the DB schema
│   ├── repositories/         # Data access layer (pgx SQL, Querier interface)
│   ├── schemas/              # Request/response DTOs with validator tags
│   └── services/             # Business logic + transactions (auth)
├── migrations/               # Atlas migration files
├── atlas.hcl                 # Atlas config
├── docker-compose.yml        # Local dev environment
├── Dockerfile                # Multi-stage build (dev + prod)
└── .golangci.yml             # Linter configuration
```

## Available Commands

```bash
# Lint
golangci-lint run ./...

# Vet
go vet ./...

# Tests
go test ./... -v -race

# Regenerate OpenAPI docs from swag annotations (run after changing router handlers)
swag init -g cmd/api/main.go -o docs/api --parseDependency

# Verify dependencies are tidy
go mod tidy && git diff --exit-code
```

## Environment Variables

| Variable | Required | Default | Description |
| `PORT` | No | `8080` | HTTP server port |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `JWT_SECRET` | Yes | - | JWT signing secret |
| `ACCESS_TOKEN_TTL` | No | `15m` | Access token lifetime |
| `REFRESH_TOKEN_TTL` | No | `7d` | Refresh token lifetime |
| `LOG_LEVEL` | No | `info` | Logging level |

## API Endpoints

### Implemented

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Create user (bcrypt hash, default `VIEWER` role), returns access + refresh tokens. Duplicate username/email → `409` |
| `POST` | `/api/v1/auth/login` | Verify credentials by username or email, returns token pair. Any failure → `401` |
| `GET` | `/health` | DB connectivity probe (load balancer) |
| `GET` | `/docs` | Interactive OpenAPI/Swagger UI (generated from `swag` annotations) |

Auth flow: access token is a 15m HS256 JWT (`Authorization: Bearer <token>`); the refresh token is 7d, stored as a SHA-256 hash in `refresh_tokens`.

### Planned

| Domain | Base Path |
|---|---|
| Products | `/api/v1/products` |
| Batches | `/api/v1/batches` |
| Inventory | `/api/v1/inventory` |
| Production | `/api/v1/production` |
| Auth (refresh / me / logout) | `/api/v1/auth` |

See `docs/PROJECT_DESIGN.md` for the full API specification.

## Deployment

The production Docker image is built from the default stage (scratch-based, ~15 MB):

```bash
docker build -t fmis-api .
```

Deployment targets AWS (ECS Fargate / App Runner). Environment variables are injected via ECS task definition (Secrets Manager for sensitive values).

## License

MIT