package server

import (
	"net/http"
)

// versionHeaderMiddleware adds the X-Hive-Server-Version header to all responses.
func versionHeaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vi := GetVersionInfo()
		w.Header().Set("X-Hive-Server-Version", vi.Version)
		next.ServeHTTP(w, r)
	})
}
