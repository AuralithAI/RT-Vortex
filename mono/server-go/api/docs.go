// Package apidocs provides the embedded OpenAPI specification for the RTVortex API.
package apidocs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var Spec []byte

// Handler serves the embedded OpenAPI 3.0 specification.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(Spec)
}
