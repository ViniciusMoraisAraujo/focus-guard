package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/policy"
)

// benchStatusResponse builds a realistic status response: one block per domain
// with the same shape the daemon ships in the status IPC (the ~1.4 MB payload
// at 1000 blocks that motivated the P1 gzip recommendation).
func benchStatusResponse(numDomains int) *ipc.Response {
	now := time.Now()
	blocks := make([]policy.Block, 0, numDomains)
	for i := 0; i < numDomains; i++ {
		domain := fmt.Sprintf("domain-%d.test", i)
		blocks = append(blocks, policy.Block{
			Domain:      domain,
			StartedAt:   now,
			ExpiresAt:   now.Add(24 * time.Hour),
			ResolvedIPs: []string{"10.0.0.1"},
		})
	}
	return &ipc.Response{Success: true, Blocks: blocks}
}

// BenchmarkHTTPActionGzip measures the /api/action round trip with
// Accept-Encoding: gzip and reports both the per-request cost and the payload
// compression ratio (compressed bytes vs plain JSON) for the state sizes the
// real daemon ships.
func BenchmarkHTTPActionGzip(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			resp := benchStatusResponse(numDomains)
			plain, err := json.Marshal(resp)
			if err != nil {
				b.Fatalf("marshal: %v", err)
			}

			stub := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
				return resp, nil
			}}
			h := newTestServer(stub, uiFS())
			_ = plain

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest("POST", "/api/action", strings.NewReader(`{"action":"status"}`))
				req.Host = "127.0.0.1:48902"
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept-Encoding", "gzip")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Header().Get("Content-Encoding") != "gzip" {
					b.Fatal("gzip não aplicado")
				}
			}
		})
	}
}

// BenchmarkHTTPActionGzipRatio is a one-shot probe that pins the compression
// ratio for the status payload sizes (same input as the ipc StatusWithManyBlocks
// benchmark) so the P1 gzip gain is comparable across runs.
func BenchmarkHTTPActionGzipRatio(b *testing.B) {
	for _, numDomains := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("domains=%d", numDomains), func(b *testing.B) {
			resp := benchStatusResponse(numDomains)
			plain, err := json.Marshal(resp)
			if err != nil {
				b.Fatalf("marshal: %v", err)
			}

			stub := &stubClient{fn: func(ipc.Request) (*ipc.Response, error) {
				return resp, nil
			}}
			h := newTestServer(stub, uiFS())

			req := httptest.NewRequest("POST", "/api/action", strings.NewReader(`{"action":"status"}`))
			req.Host = "127.0.0.1:48902"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			compressed := rec.Body.Len()

			gz, err := gzip.NewReader(rec.Body)
			if err != nil {
				b.Fatalf("gzip reader: %v", err)
			}
			dec, err := io.ReadAll(gz)
			if err != nil {
				b.Fatalf("decompress: %v", err)
			}
			ratio := 1 - float64(compressed)/float64(len(plain))
			b.Logf("plain=%d compressed=%d ratio=%.1f%% decoded=%d", len(plain), compressed, ratio*100, len(dec))
		})
	}
}
