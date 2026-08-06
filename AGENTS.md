# AGENTS.md

## Repo state
- **Implemented:** `cmd/api/main.go` entry point (config load, pgx pool, `/health`, graceful shutdown); `internal/config`, `internal/database` (pgx pool init), `internal/routers` (chi router + health check); Atlas setup (`atlas.hcl`, `migrations/schema.hcl`, `migrations/20260802033509_create_initial_schema.sql`); CI (`linting.yml`, `test.yml`).
- **Not yet implemented:** `internal/middleware`, `internal/models`, `internal/repositories`, `internal/schemas`, `internal/services`, `tests/`, `sqlc.yaml`. JWT auth (issue #16) and all `/api/v1/*` routes are next.
- `docs/PROJECT_DESIGN.md` is the authoritative spec (layering, endpoints, RBAC matrix, entity schema, FEFO rules). Read it before implementing any feature; README is a summary.

## Commands
- Lint: `golangci-lint run ./...` (v2.12 per CI).
- Vet: `go vet ./...`. Tests: `go test ./... -v -race`.
- CI also enforces `go mod tidy && git diff --exit-code` — commit with tidy go.mod/go.sum. Direct deps today: `caarlos0/env/v11`, `go-chi/chi/v5`, `jackc/pgx/v5`. `golang-jwt/jwt/v5`, `go-playground/validator/v10`, `testify` are still to be added.
- Local dev DB: `docker compose up db` (compose reads `.env.local`, which is gitignored). Its `DATABASE_URL` points at host `db` — only resolvable inside the compose network; for host-side `go run`, override with `localhost:5432`.
- Migrations use the Atlas CLI (not a Go dep; not installed here). `atlas.hcl` + `migrations/` exist; use `atlas migrate diff|apply --env local`.
- Toolchain: Go 1.26+, entry point is fixed at `cmd/api/main.go` (Dockerfile prod stage and `.air.toml` both reference it).

## Architecture rules (enforced by design doc)
- Layering: routers → schemas (validator tags) → services (owns transactions and `SELECT ... FOR UPDATE`) → repositories (pgx SQL). Routers contain zero raw SQL.
- Domain invariants: quantities are `DECIMAL(10,4)`, PKs are UUID v4, FEFO ordering is `ORDER BY expiration_date ASC NULLS LAST`, production output inherits `MIN(expiration_date)` of inputs, `WHITE_LABEL` products are never sold directly (only via production orders).
- RBAC roles ADMIN / OPERATOR / VIEWER; per-endpoint matrix is in the design doc.
- Integration tests must run against real PostgreSQL — SQLite cannot validate `SELECT ... FOR UPDATE` semantics.
- Auth: JWT access token (15m) + refresh token (7d, SHA-256 hashed in DB), bcrypt password hashes.
