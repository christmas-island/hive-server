package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/christmas-island/hive-server/internal/log"
	"github.com/christmas-island/hive-server/internal/timing"
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
func (b *bufResponseWriter) flush(totalMs, dbMs int64) {
	processingMs := totalMs - dbMs
	if processingMs < 0 {
		processingMs = 0
	}
	b.w.Header().Set("X-Total-Ms", fmt.Sprintf("%d", totalMs))
	b.w.Header().Set("X-Processing-Ms", fmt.Sprintf("%d", processingMs))
	b.w.Header().Set("X-DB-Ms", fmt.Sprintf("%d", dbMs))
	if b.status != 0 {
		b.w.WriteHeader(b.status)
	}
	_, _ = b.w.Write(b.buf.Bytes())
}

// timingMiddleware records wall-clock request duration and sets timing headers
// on every response:
//
//   - X-Total-Ms      — total elapsed milliseconds for this request
//   - X-Processing-Ms — total minus DB time (CPU + network overhead at this layer)
//   - X-DB-Ms         — cumulative time spent in database queries for this request
//
// It also emits a structured JSON log line with method, path, status, and
// duration breakdown for easy parsing by log aggregators (e.g. Loki/Grafana).
//
// A *timing.Accumulator is injected into the request context so that store
// methods can record per-query latency via timing.TrackDB.
func timingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ctx, acc := timing.NewContext(r.Context())

		bw := &bufResponseWriter{w: w}
		next.ServeHTTP(bw, r.WithContext(ctx))

		totalMs := time.Since(start).Milliseconds()
		dbMs := acc.DBMs()
		processingMs := totalMs - dbMs
		if processingMs < 0 {
			processingMs = 0
		}
		bw.flush(totalMs, dbMs)

		status := bw.status
		if status == 0 {
			status = http.StatusOK
		}

		// Structured JSON log for aggregators (Loki, SigNoz, etc.).
		entry := map[string]any{
			"method":        r.Method,
			"path":          r.URL.Path,
			"status":        status,
			"total_ms":      totalMs,
			"processing_ms": processingMs,
			"db_ms":         dbMs,
		}
		b, _ := json.Marshal(entry)
		log.Info(fmt.Sprintf("request %s", string(b)))
	})
}
