package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkHashFile measures the cost every watcher pays per event: the hosts
// and state watchers read the whole file and SHA-256 it to detect whether the
// write was their own. The sizes model a hosts file grown with many blocked
// domains.
func BenchmarkHashFile(b *testing.B) {
	for _, lines := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("lines=%d", lines), func(b *testing.B) {
			var sb strings.Builder
			for i := 0; i < lines; i++ {
				fmt.Fprintf(&sb, "127.0.0.1 host-%d.example # FOCUSGUARD: host-%d.example\n", i, i)
			}
			path := filepath.Join(b.TempDir(), "hosts")
			if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := HashFile(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
