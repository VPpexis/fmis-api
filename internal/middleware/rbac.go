// Package middleware for RBAC.
package middleware

import (
	"net/http"

	"fmis-api/internal/models"
)

// RequireRole rejets the request with 403 unless authenticated.
func RequireRole(allowed ...models.UserRoleType) func(http.Handler) http.Handler {
	set := make(map[models.UserRoleType]struct{}, len(allowed))
	for _, r := range allowed {
		set[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := models.UserRoleType(RoleFromContext(r.Context()))
			if _, ok := set[role]; !ok {
				WriteErrorJSON(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
