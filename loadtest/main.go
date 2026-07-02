// Command loadtest is a small concurrent HTTP load generator used to measure
// gateway/service throughput and latency. It is intentionally dependency-free.
//
//	go run ./loadtest -url http://localhost:8080/healthz -c 50 -d 15s
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080/healthz", "target URL")
	concurrency := flag.Int("c", 50, "concurrent workers")
	duration := flag.Duration("d", 15*time.Second, "test duration")
	token := flag.String("token", "", "optional bearer token")
	flag.Parse()

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	var (
		ok        atomic.Int64
		failed    atomic.Int64
		latMu     sync.Mutex
		latencies []time.Duration
	)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, 4096)
			for ctx.Err() == nil {
				t0 := time.Now()
				code := do(client, *url, *token)
				lat := time.Since(t0)
				if code >= 200 && code < 400 {
					ok.Add(1)
					local = append(local, lat)
				} else {
					failed.Add(1)
				}
			}
			latMu.Lock()
			latencies = append(latencies, local...)
			latMu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	report(elapsed, ok.Load(), failed.Load(), latencies, *concurrency)
}

func do(client *http.Client, url, token string) int {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func report(elapsed time.Duration, ok, failed int64, lat []time.Duration, c int) {
	total := ok + failed
	rps := float64(ok) / elapsed.Seconds()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })

	fmt.Printf("duration:     %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("concurrency:  %d\n", c)
	fmt.Printf("requests:     %d (ok=%d failed=%d)\n", total, ok, failed)
	fmt.Printf("throughput:   %.0f req/s\n", rps)
	if len(lat) > 0 {
		fmt.Printf("latency p50:  %s\n", pct(lat, 50).Round(time.Microsecond))
		fmt.Printf("latency p90:  %s\n", pct(lat, 90).Round(time.Microsecond))
		fmt.Printf("latency p99:  %s\n", pct(lat, 99).Round(time.Microsecond))
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "note: %d requests failed (non-2xx/3xx)\n", failed)
	}
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
