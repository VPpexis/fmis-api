// Package middleware provides cross-cutting HTTP concerns: JWT authentication, and future RBAC/logging.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload we put inside an access token.
type Claims struct {
	UserID               string `json:"user_id"`
	Role                 string `json:"role"`
	jwt.RegisteredClaims        // embeds exp, iat, sub, etc.
}

type ctxKey string

const (
	ctxKeyUserID ctxKey = "user_id"
	ctxKeyRole   ctxKey = "role"
)

// Auth returns a middleware that validates a JWT access token from the
// Authorization header and injects its user_id and role into the request context.
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			claims := &Claims{}
			parsed, err := jwt.ParseWithClaims(token, claims,
				func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return []byte(secret), nil
				},
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithExpirationRequired(),
			)
			if err != nil || !parsed.Valid {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxKeyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext returns the authenticated user's ID, or "" if absent.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

// RoleFromContext returns the authenticated user's role, or "" if absent.
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole).(string)
	return v
}
