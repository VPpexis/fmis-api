# AGENTS.md

## Repo state (do not be fooled by the README)
- This is a **scaffold**: `cmd/`, `internal/`, `migrations/`, `tests/`, `atlas.hcl`, `sqlc.yaml` do not exist yet, though README.md and docs/PROJECT_DESIGN.md describe them.
- `docker compose up api` fails to build — there is no `cmd/api/main.go` yet. Only `docker compose up db` works today.
- `docs/PROJECT_DESIGN.md` is the authoritative spec (layering, endpoints, RBAC matrix, entity schema, FEFO rules). Read it before implementing any feature; README is a summary.

## Commands
- Lint: `golangci-lint run ./...` (v2.12 per CI). `.golangci.yml` lists `revivie` — a typo for `revive`; the run fails on the unknown linter until that is corrected.
- Vet: `go vet ./...`. Tests: `go test ./... -v -race`.
- CI also enforces `go mod tidy && git diff --exit-code` — commit with tidy go.mod/go.sum. All deps are currently marked `// indirect` (no code yet); `go mod tidy` rewrites them once code lands.
- Local dev DB: `docker compose up db` (compose reads `.env.local`, which is gitignored). Its `DATABASE_URL` points at host `db` — only resolvable inside the compose network; for host-side `go run`, override with `localhost:5432`.
- Migrations use the Atlas CLI (not a Go dep; not installed here). `atlas.hcl` + `migrations/` still need to be created; README/design doc reference `atlas migrate diff|apply --env local`.
- Toolchain: Go 1.26+, entry point is fixed at `cmd/api/main.go` (Dockerfile prod stage and `.air.toml` both reference it).

## Architecture rules (enforced by design doc)
- Layering: routers → schemas (validator tags) → services (owns transactions and `SELECT ... FOR UPDATE`) → repositories (pgx SQL). Routers contain zero raw SQL.
- Domain invariants: quantities are `DECIMAL(10,4)`, PKs are UUID v4, FEFO ordering is `ORDER BY expiration_date ASC NULLS LAST`, production output inherits `MIN(expiration_date)` of inputs, `WHITE_LABEL` products are never sold directly (only via production orders).
- RBAC roles ADMIN / OPERATOR / VIEWER; per-endpoint matrix is in the design doc.
- Integration tests must run against real PostgreSQL — SQLite cannot validate `SELECT ... FOR UPDATE` semantics.
- Auth: JWT access token (15m) + refresh token (7d, SHA-256 hashed in DB), bcrypt password hashes.
