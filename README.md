# FMIS API - Food Manufacturing Inventory System

A product-grade REST API for food manufacturing inventory management, built in Go.

## Features

- **Batch/Lot Trackings** - Manage raw materials, packaging, finished goods, and white-label products.
- **FEFO Consumption** - First-Expired, First-Out stock deduction logic
- **Production Orders** - White-label assembly with atomic batch consumption
- **RBAC** - `SELECT ... FOR UPDATE` for race-condition-safe inventory deductions
- **Immutable Audit Logs** - All stock movements recorded as transactions.

## Implementation Status

| Area | Status |
|---|---|
| API entry point (`cmd/api/main.go`) | Done |
| Config (`internal/config`) | Done |
| pgx pool (`internal/database`) | Done |
| chi router + `/health` (`internal/routers`) | Done |
| Atlas schema + migrations | Done |
| JWT auth middleware | Next (issue #16) |
| Auth / products / batches / inventory / production API | Planned |
| Models, repositories, schemas, services | Planned |
| Tests (`tests/`) | Planned |
| `sqlc.yaml` | Planned |

See `docs/PROJECT_DESIGN.md` for the authoritative spec.
  
## Tech Stack
| Concern | Library | Status |
|---------|---------|--------|
| HTTP Router | `go-chi/chi/v5` | Implemented |
| Database Driver | `jackc/pgx/v5` | Implemented |
| Configuration | `caarlos0/env/v11` | Implemented |
| Migrations | `atlasgo/atlas` | Implemented |
| Auth | `golang-jwt/jwt/v5` + bcrypt | Planned |
| Validation | `go-playground/validator/v10` | Planned |
| Testing | `stretchr/testify` | Planned |
| Linting | `golangci-lint` | Implemented |
| Hot Reload | `air` | Implemented |

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
│   ├── routers/              # chi route groups
│   ├── middleware/           # JWT auth, RBAC, logging (planned)
│   ├── models/               # Domain structs (planned)
│   ├── repositories/         # Data access layer (planned)
│   ├── schemas/              # Request/response DTOs (planned)
│   └── services/             # Business logic (planned)
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

See `docs/PROJECT_DESIGN.md` for the full API specification.

| Domain | Base Path |
|---|---|
| Auth | `/api/v1/auth` |
| Products | `/api/v1/products` |
| Batches | `/api/v1/batches` |
| Inventory | `/api/v1/inventory` |
| Production | `/api/v1/production` |
| Health | `/health` |

## Deployment

The production Docker image is built from the default stage (scratch-based, ~15 MB):

```bash
docker build -t fmis-api .
```

Deployment targets AWS (ECS Fargate / App Runner). Environment variables are injected via ECS task definition (Secrets Manager for sensitive values).

## License

MIT