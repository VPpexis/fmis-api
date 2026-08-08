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
	"fmis-api/internal/services"
)

// Pinger reports whether the backing data store is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// New builds the chi router with base middleware and routes.
func New(auth *services.AuthService, pinger Pinger, jwtSecret string, logger *slog.Logger) http.Handler {
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

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(jwtSecret))
	})

	r.Route("/api/v1/auth", func(r chi.Router) {
		NewAuthRouter(auth, logger).Routes(r)
	})

	return r
}
