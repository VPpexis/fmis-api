// Package routers wires the HTTP routing tree.
package routers

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"

	"fmis-api/internal/middleware"
)

// decodeAndValidate decodes a JSON body into dst and runs validator
// tags on it, writing a 400 response itself when either step fails.
func decodeAndValidate(v *validator.Validate, w http.ResponseWriter, r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid JSON Body")
		return err
	}
	if err := v.Struct(dst); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return err
	}
	return nil
}

// writeJSON writes v as a Json response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrorJSON writes the shared {"error": "..."} envelope.
func writeErrorJSON(w http.ResponseWriter, status int, msg string) {
	middleware.WriteErrorJSON(w, status, msg)
}
