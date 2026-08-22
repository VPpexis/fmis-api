// Package routers wires the HTTP routing tree.
package routers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/services"
)

// Pinger reports whether the backing data store is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// New builds the chi router with base middleware and routes.
func New(auth *services.AuthService,
	batches *services.BatchService,
	products *services.ProductService,
	inventory *services.InventoryService,
	pinger Pinger,
	jwtSecret string,
	logger *slog.Logger,
) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.CORS("*"))
	r.Use(chimw.RequestID)
	r.Use(middleware.Logging(logger))
	r.Use(middleware.Recoverer(logger))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pinger.Ping(ctx); err != nil {
			slog.Error("health check failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1/auth", func(r chi.Router) {
		ar := NewAuthRouter(auth, logger)
		ar.Routes(r)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(jwtSecret))
			r.Get("/me", ar.me)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(jwtSecret))

		br := NewBatchRouter(batches, logger)
		r.Route("/api/v1/batches", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.UserRoleTypeAdmin, models.UserRoleTypeOperator))
				r.Post("/", br.receive)
				r.Patch("/{batch_id}/quarantine", br.quarantine)
			})
			r.Get("/product/{product_id}", br.listByProduct)
		})
		pr := NewProductRouter(products, logger)
		r.Route("/api/v1/products", func(r chi.Router) {
			r.Get("/", pr.list)
			r.Get("/{product_id}", pr.getByID)
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.UserRoleTypeAdmin, models.UserRoleTypeOperator))
				r.Post("/", pr.create)
			})
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.UserRoleTypeAdmin))
				r.Patch("/{product_id}", pr.update)
				r.Delete("/{product_id}", pr.delete)
			})
		})
		ir := NewInventoryRouter(inventory, logger)
		r.Route("/api/v1/inventory", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(models.UserRoleTypeAdmin, models.UserRoleTypeOperator))
				r.Post("/adjust", ir.adjust)
			})
			r.Get("/transactions/", ir.listTransactions)
		})
	})

	return r
}
