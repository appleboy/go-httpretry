package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

// PrometheusCollector implements retry.MetricsCollector for Prometheus
// This is a simplified example showing the interface implementation.
// In production, you would use github.com/prometheus/client_golang
type PrometheusCollector struct {
	mu sync.Mutex

	// In production, these would be prometheus.Counter/Histogram
	attemptsTotal    map[string]int            // key: method
	retriesTotal     map[string]map[string]int // key: method, then reason
	requestsTotal    map[string]map[bool]int   // key: method, then success
	attemptDurations []float64                 // Would be prometheus.Histogram
	requestDurations []float64                 // Would be prometheus.Histogram
}

func NewPrometheusCollector() *PrometheusCollector {
	return &PrometheusCollector{
		attemptsTotal: make(map[string]int),
		retriesTotal:  make(map[string]map[string]int),
		requestsTotal: make(map[string]map[bool]int),
	}
}

func (p *PrometheusCollector) RecordAttempt(
	method string,
	statusCode int,
	duration time.Duration,
	err error,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// In production: httpRetryAttempts.WithLabelValues(method, fmt.Sprint(statusCode)).Inc()
	p.attemptsTotal[method]++
	p.attemptDurations = append(p.attemptDurations, duration.Seconds())

	fmt.Printf(
		"[Prometheus] http_retry_attempts_total{method=%q,status=%d} +1\n",
		method,
		statusCode,
	)
	fmt.Printf(
		"[Prometheus] http_retry_attempt_duration_seconds{method=%q} observe %.3fs\n",
		method,
		duration.Seconds(),
	)
}

func (p *PrometheusCollector) RecordRetry(method string, reason string, attemptNumber int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// In production: httpRetryRetries.WithLabelValues(method, reason).Inc()
	if p.retriesTotal[method] == nil {
		p.retriesTotal[method] = make(map[string]int)
	}
	p.retriesTotal[method][reason]++

	fmt.Printf("[Prometheus] http_retry_retries_total{method=%q,reason=%q} +1\n", method, reason)
}

func (p *PrometheusCollector) RecordRequestComplete(
	method string,
	statusCode int,
	totalDuration time.Duration,
	totalAttempts int,
	success bool,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// In production:
	// httpRetryRequests.WithLabelValues(method, fmt.Sprint(success)).Inc()
	// httpRetryRequestDuration.WithLabelValues(method).Observe(totalDuration.Seconds())
	// httpRetryRequestAttempts.WithLabelValues(method).Observe(float64(totalAttempts))

	if p.requestsTotal[method] == nil {
		p.requestsTotal[method] = make(map[bool]int)
	}
	p.requestsTotal[method][success]++
	p.requestDurations = append(p.requestDurations, totalDuration.Seconds())

	fmt.Printf("[Prometheus] http_retry_requests_total{method=%q,success=%v} +1\n", method, success)
	fmt.Printf(
		"[Prometheus] http_retry_request_duration_seconds{method=%q} observe %.3fs\n",
		method,
		totalDuration.Seconds(),
	)
	fmt.Printf(
		"[Prometheus] http_retry_request_attempts{method=%q} observe %d\n",
		method,
		totalAttempts,
	)
}

func (p *PrometheusCollector) PrintMetrics() {
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Println("\n=== Prometheus Metrics Summary ===")
	fmt.Println("# HELP http_retry_attempts_total Total number of HTTP attempts")
	fmt.Println("# TYPE http_retry_attempts_total counter")
	for method, count := range p.attemptsTotal {
		fmt.Printf("http_retry_attempts_total{method=%q} %d\n", method, count)
	}

	fmt.Println("\n# HELP http_retry_retries_total Total number of retries by reason")
	fmt.Println("# TYPE http_retry_retries_total counter")
	for method, reasons := range p.retriesTotal {
		for reason, count := range reasons {
			fmt.Printf("http_retry_retries_total{method=%q,reason=%q} %d\n", method, reason, count)
		}
	}

	fmt.Println("\n# HELP http_retry_requests_total Total number of completed requests")
	fmt.Println("# TYPE http_retry_requests_total counter")
	for method, outcomes := range p.requestsTotal {
		for success, count := range outcomes {
			fmt.Printf(
				"http_retry_requests_total{method=%q,success=%v} %d\n",
				method,
				success,
				count,
			)
		}
	}
}

func main() {
	// Create Prometheus collector
	promCollector := NewPrometheusCollector()

	// Create test server that fails twice then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create retry client with Prometheus metrics
	client, err := retry.NewClient(
		retry.WithMaxRetries(5),
		retry.WithInitialRetryDelay(50*time.Millisecond),
		retry.WithJitter(false),
		retry.WithMetrics(promCollector),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("=== Making HTTP request with Prometheus metrics ===\n")

	// Make request
	ctx := context.Background()
	resp, err := client.Get(ctx, server.URL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Printf("\n=== Request completed: status=%d, attempts=%d ===\n", resp.StatusCode, attempts)

	// Print metrics (in production, these would be exposed on /metrics endpoint)
	promCollector.PrintMetrics()

	fmt.Println(
		"\n// In production, these metrics would be available at http://localhost:9090/metrics",
	)
	fmt.Println("// and scraped by Prometheus for visualization in Grafana")
}
