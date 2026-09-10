# AGENTS.md

## Repo state
- **Implemented:** full stack — `cmd/api/main.go` (config, pgx pool, graceful shutdown); `internal/{config,database,middleware,models,repositories,schemas,services,routers}`; JWT auth (register/login/refresh/logout/me); products CRUD; batches receive/quarantine/FEFO list; inventory adjust/consume/transactions; production create/start/complete/cancel/list/detail; OpenAPI at `GET /docs`; Atlas migrations; CI (`linting.yml`, `test.yml`).
- **Tests:** unit tests (middleware, services with testify mocks) plus integration tests in `tests/integration/` (real HTTP via `httptest.Server`). `internal/testutil` gives each test a throwaway schema with every migration applied; fixtures live in `tests/testdata/seed.sql`.
- **Not yet implemented:** `sqlc.yaml` (repositories are handwritten).
- `docs/PROJECT_DESIGN.md` is the authoritative spec (layering, endpoints, RBAC matrix, entity schema, FEFO rules). Read it before implementing any feature; README is a summary.

## Commands
- Lint: `golangci-lint run ./...` (v2.12 per CI).
- Vet: `go vet ./...`. Tests: `go test ./... -v -race`.
- CI also enforces `go mod tidy && git diff --exit-code` — commit with tidy go.mod/go.sum. Direct deps: `caarlos0/env/v11`, `go-chi/chi/v5`, `jackc/pgx/v5`, `golang-jwt/jwt/v5`, `go-playground/validator/v10`, `stretchr/testify`, `swaggo/swag` + `swaggo/http-swagger/v2`.
- Local dev DB: `docker compose up db` (compose reads `.env.local`, which is gitignored). Its `DATABASE_URL` points at host `db` — only resolvable inside the compose network; for host-side `go run`, override with `localhost:5432`.
- Integration tests (incl. `tests/integration/`) use `DATABASE_URL` or default to the compose DB (`localhost:5432/fmis_db`); start it with `docker compose up db` first. Each test creates and drops its own schema, so it never touches dev data.
- Migrations use the Atlas CLI (not a Go dep; not installed here). `atlas.hcl` + `migrations/` exist; use `atlas migrate diff|apply --env local`.
- Toolchain: Go 1.26+, entry point is fixed at `cmd/api/main.go` (Dockerfile prod stage and `.air.toml` both reference it).

## Architecture rules (enforced by design doc)
- Layering: routers → schemas (validator tags) → services (owns transactions and `SELECT ... FOR UPDATE`) → repositories (pgx SQL). Routers contain zero raw SQL.
- Domain invariants: quantities are `DECIMAL(10,4)`, PKs are UUID v4, FEFO ordering is `ORDER BY expiration_date ASC NULLS LAST`, production output inherits `MIN(expiration_date)` of inputs, `WHITE_LABEL` products are never sold directly (only via production orders).
- RBAC roles ADMIN / OPERATOR / VIEWER; per-endpoint matrix is in the design doc.
- Integration tests must run against real PostgreSQL — SQLite cannot validate `SELECT ... FOR UPDATE` semantics.
- Auth: JWT access token (15m) + refresh token (7d, SHA-256 hashed in DB), bcrypt password hashes.
