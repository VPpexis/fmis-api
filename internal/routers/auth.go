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
// @Summary Refresh an access token
// @Description Exchanges a valid refresh token for a new access token + refresh token pair.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body schemas.RefreshRequest true "Refresh token payload"
// @Success 200 {object} schemas.TokenResponse
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Invalid or expired refresh token"
// @Router /auth/refresh [post]
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
// @Summary Log out
// @Description Revokes a refresh token, ending the session. The access token itself remains valid until it expires.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body schemas.LogoutRequest true "Refresh token to revoke"
// @Success 204 "No content"
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Invalid refresh token"
// @Router /auth/logout [post]
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
// @Summary Get current user
// @Description Returns the authenticated user's profile (id, username, email, role).
// @Tags auth
// @Produce json
// @Success 200 {object} schemas.MeResponse
// @Failure 401 {object} map[string]string "Missing or invalid token"
// @Router /auth/me [get]
// @Security BearerAuth
func (a *AuthRouter) me(w http.ResponseWriter, r *http.Request) {
	resp, err := a.svc.Me(r.Context(), middleware.UserIDFromContext(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// register handles POST /api/v1/auth/reg
// @Summary Register a new user
// @Description Creates a user with the default VIEWER role and returns a token pair. Requires a unique username and email.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body schemas.RegisterRequest true "Registration payload"
// @Success 201 {object} schemas.TokenResponse
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 409 {object} map[string]string "Username or email already taken"
// @Router /auth/register [post]
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
// @Summary Log in
// @Description Verifies credentials by username or email and returns a token pair.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body schemas.LoginRequest true "Login payload"
// @Success 200 {object} schemas.TokenResponse
// @Failure 400 {object} map[string]string "Invalid body"
// @Failure 401 {object} map[string]string "Invalid credentials"
// @Router /auth/login [post]
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
	case errors.Is(err, services.ErrProductNotFound), errors.Is(err, services.ErrBatchNotFound), errors.Is(err, services.ErrProductionOrderNotFound):
		writeErrorJSON(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrExpirationRequired), errors.Is(err, services.ErrInvalidProductionInput):
		writeErrorJSON(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrInvalidBatchState), errors.Is(err, services.ErrInvalidProductionOrderState):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidRequest):
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrDuplicateSKU):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrProductHasActiveBatches):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidRefreshToken):
		writeErrorJSON(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, services.ErrInsufficientStock):
		writeErrorJSON(w, http.StatusConflict, err.Error())
	default:
		slog.Error("unhandled service error", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal server error")
	}
}
