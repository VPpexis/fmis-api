package middleware

import (
	"encoding/json"
	"net/http"
)

// WriteErrorJSON writes a uniform {"error": "..."} response body.
// Every error response across the API uses this envelope.
func WriteErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
