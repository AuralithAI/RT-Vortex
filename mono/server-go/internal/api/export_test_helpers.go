package api

import "net/http"

// ParsePaginationExported is an exported wrapper for tests.
func ParsePaginationExported(r *http.Request) (int, int) {
	return parsePagination(r)
}

// WriteValidationErrorExported is an exported wrapper for tests.
func WriteValidationErrorExported(w http.ResponseWriter, ve interface{ StatusCode() int }) {
	writeValidationError(w, ve)
}
