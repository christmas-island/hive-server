package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/christmas-island/hive-server/internal/log"
)

// bufResponseWriter buffers the response body and defers WriteHeader so that
// the timing middleware can inject headers before flushing to the wire.
type bufResponseWriter struct {
	w      http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (b *bufResponseWriter) Header() http.Header {
	return b.w.Header()
}

func (b *bufResponseWriter) WriteHeader(status int) {
	b.status = status
}

func (b *bufResponseWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.buf.Write(p)
}

// flush sets timing headers then writes the buffered status + body to the wire.
func (b *bufResponseWriter) flush(totalMs int64) {
	b.w.Header().Set("X-Total-Ms", fmt.Sprintf("%d", totalMs))
	b.w.Header().Set("X-Processing-Ms", fmt.Sprintf("%d", totalMs))
	if b.status != 0 {
		b.w.WriteHeader(b.status)
	}
	_, _ = b.w.Write(b.buf.Bytes())
}

// timingMiddleware records wall-clock request duration and sets timing headers
// on every response:
//
//   - X-Total-Ms      — total elapsed milliseconds for this request
//   - X-Processing-Ms — same as X-Total-Ms at this layer (no upstream calls from hive-server)
//
// It also emits a structured log line with method, path, status, and duration.
// Responses are buffered so that headers can be set before the response is
// written to the wire.
func timingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		bw := &bufResponseWriter{w: w}
		next.ServeHTTP(bw, r)

		totalMs := time.Since(start).Milliseconds()
		bw.flush(totalMs)

		status := bw.status
		if status == 0 {
			status = http.StatusOK
		}

		log.Info(fmt.Sprintf(
			"request method=%s path=%s status=%d total_ms=%d",
			r.Method, r.URL.Path, status, totalMs,
		))
	})
}
