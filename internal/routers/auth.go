// Package routers for auth.
package routers

import (
	"errors"
	"fmis-api/internal/middleware"
	"fmis-api/internal/schemas"
	"fmis-api/internal/services"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// AuthRouter serves the /api/v1/auth endpoints.
type AuthRouter struct {
	svc      *services.AuthService
	validate *validator.Validate
	logger   *slog.Logger
}

// NewAuthRouter constructs the auth HTTP handlers
func NewAuthRouter(svc *services.AuthService, logger *slog.Logger) *AuthRouter {
	return &AuthRouter{svc: svc, validate: validator.New(), logger: logger}
}

// Routes mounts the auth endpoints under r.
func (a *AuthRouter) Routes(r chi.Router) {
	r.Post("/register", a.register)
	r.Post("/login", a.login)
	r.Post("/refresh", a.refresh)
	r.Post("/logout", a.logout)
}

// refresh handles POST /api/v1/auth/refresh
func (a *AuthRouter) refresh(w http.ResponseWriter, r *http.Request) {
	var req schemas.RefreshRequest
	if err := decodeAndValidate(a.validate, w, r, &req); err != nil {
		return
	}
	resp, err := a.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// logout handles POST /api/v1/auth/logout
func (a *AuthRouter) logout(w http.ResponseWriter, r *http.Request) {
	var req schemas.LogoutRequest
	if err := decodeAndValidate(a.validate, w, r, &req); err != nil {
		return
	}
	if err := a.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// me handles GET /api/v1/auth/me
func (a *AuthRouter) me(w http.ResponseWriter, r *http.Request) {
	resp, err := a.svc.Me(r.Context(), middleware.UserIDFromContext(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// register handles POST /api/v1/auth/reg
func (a *AuthRouter) register(w http.ResponseWriter, r *http.Request) {
	var req schemas.RegisterRequest
	if err := decodeAndValidate(a.validate, w, r, &req); err != nil {
		return
	}

	resp, err := a.svc.Register(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// login handles POST /api/v1/auth/login
func (a *AuthRouter) login(w http.ResponseWriter, r *http.Request) {
	var req schemas.LoginRequest
	if err := decodeAndValidate(a.validate, w, r, &req); err != nil {
		return
	}

	resp, err := a.svc.Login(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrDuplicate):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidCredentials):
		writeErrorJSON(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, services.ErrProductNotFound), errors.Is(err, services.ErrBatchNotFound):
		writeErrorJSON(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrExpirationRequired):
		writeErrorJSON(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrInvalidBatchState):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidRequest):
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrDuplicateSKU):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrProductHasActiveBatches):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidRefreshToken):
		writeErrorJSON(w, http.StatusUnauthorized, err.Error())
	default:
		slog.Error("unhandled service error", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal server error")
	}
}
