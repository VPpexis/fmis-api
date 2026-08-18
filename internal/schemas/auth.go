// Package schemas for auth.
package schemas

// RegisterRequest is the payload for POST /api/v1/auth/register
type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=100"`
	Email    string `json:"email" validate:"required,email,min=3,max=100"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// LoginRequest is the payload for POST /api/v1/auth/login
// Identifier accepts either a username or an email.
type LoginRequest struct {
	Identifier string `json:"identifier" validate:"required,max=255"`
	Password   string `json:"password" validate:"required"`
}

// RefreshRequest is the payload for POST /api/v1/auth/refresh
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// LogoutRequest is the payload for POST /api/v1/auth/logout
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// MeResponse is returned by GET /api/v1/auth/me
type MeResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

// TokenResponse is returned by register and login.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}
