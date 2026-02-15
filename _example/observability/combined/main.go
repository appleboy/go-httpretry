package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

// SimpleMetricsCollector demonstrates a basic metrics implementation
type SimpleMetricsCollector struct {
	mu               sync.Mutex
	totalAttempts    int
	totalRetries     int
	successfulReqs   int
	failedReqs       int
	totalDuration    time.Duration
	attemptDurations []time.Duration
}

func (m *SimpleMetricsCollector) RecordAttempt(
	method string,
	statusCode int,
	duration time.Duration,
	err error,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalAttempts++
	m.attemptDurations = append(m.attemptDurations, duration)
	fmt.Printf("[METRIC] Attempt recorded: method=%s status=%d duration=%v err=%v\n",
		method, statusCode, duration, err)
}

func (m *SimpleMetricsCollector) RecordRetry(method string, reason string, attemptNumber int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRetries++
	fmt.Printf("[METRIC] Retry recorded: method=%s reason=%s attempt=%d\n",
		method, reason, attemptNumber)
}

func (m *SimpleMetricsCollector) RecordRequestComplete(
	method string,
	statusCode int,
	totalDuration time.Duration,
	totalAttempts int,
	success bool,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalDuration += totalDuration
	if success {
		m.successfulReqs++
	} else {
		m.failedReqs++
	}
	fmt.Printf(
		"[METRIC] Request complete: method=%s status=%d duration=%v attempts=%d success=%v\n",
		method,
		statusCode,
		totalDuration,
		totalAttempts,
		success,
	)
}

func (m *SimpleMetricsCollector) PrintSummary() {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Println("\n=== METRICS SUMMARY ===")
	fmt.Printf("Total Attempts: %d\n", m.totalAttempts)
	fmt.Printf("Total Retries: %d\n", m.totalRetries)
	fmt.Printf("Successful Requests: %d\n", m.successfulReqs)
	fmt.Printf("Failed Requests: %d\n", m.failedReqs)
	fmt.Printf("Total Duration: %v\n", m.totalDuration)
	if len(m.attemptDurations) > 0 {
		var sum time.Duration
		for _, d := range m.attemptDurations {
			sum += d
		}
		avg := sum / time.Duration(len(m.attemptDurations))
		fmt.Printf("Average Attempt Duration: %v\n", avg)
	}
}

// SimpleTracer demonstrates a basic tracing implementation
type SimpleTracer struct {
	mu    sync.Mutex
	spans []*SimpleSpan
}

type SimpleSpan struct {
	mu          sync.Mutex
	name        string
	startTime   time.Time
	endTime     time.Time
	attributes  []retry.Attribute
	status      string
	description string
	events      []SimpleEvent
}

type SimpleEvent struct {
	name       string
	timestamp  time.Time
	attributes []retry.Attribute
}

func (t *SimpleTracer) StartSpan(
	ctx context.Context,
	operationName string,
	attrs ...retry.Attribute,
) (context.Context, retry.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()

	span := &SimpleSpan{
		name:       operationName,
		startTime:  time.Now(),
		attributes: attrs,
	}
	t.spans = append(t.spans, span)

	fmt.Printf("[TRACE] Span started: name=%s\n", operationName)
	return ctx, span
}

func (s *SimpleSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endTime = time.Now()
	duration := s.endTime.Sub(s.startTime)
	fmt.Printf("[TRACE] Span ended: name=%s duration=%v status=%s\n",
		s.name, duration, s.status)
}

func (s *SimpleSpan) SetAttributes(attrs ...retry.Attribute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attributes = append(s.attributes, attrs...)
}

func (s *SimpleSpan) SetStatus(code string, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
	s.description = description
}

func (s *SimpleSpan) AddEvent(name string, attrs ...retry.Attribute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event := SimpleEvent{
		name:       name,
		timestamp:  time.Now(),
		attributes: attrs,
	}
	s.events = append(s.events, event)
	fmt.Printf("[TRACE] Event added: span=%s event=%s\n", s.name, name)
}

func main() {
	// Create observability components
	metrics := &SimpleMetricsCollector{}
	tracer := &SimpleTracer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Create a test server that fails twice then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Simulate processing time
		time.Sleep(10 * time.Millisecond)

		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Temporary failure (attempt %d)", attempts)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Success!")
	}))
	defer server.Close()

	// Create retry client with all observability features
	client, err := retry.NewClient(
		retry.WithMaxRetries(5),
		retry.WithInitialRetryDelay(50*time.Millisecond),
		retry.WithJitter(false), // Disable for predictable demo output
		retry.WithMetrics(metrics),
		retry.WithTracer(tracer),
		retry.WithLogger(retry.NewSlogAdapter(logger)),
	)
	if err != nil {
		logger.Error("failed to create client", "error", err)
		os.Exit(1)
	}

	// Make request
	ctx := context.Background()
	fmt.Println("=== Starting HTTP request with full observability ===\n")

	resp, err := client.Get(ctx, server.URL)
	if err != nil {
		logger.Error("request failed", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Println("\n=== Request completed successfully ===")
	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Total Attempts: %d\n", attempts)

	// Print metrics summary
	metrics.PrintSummary()

	fmt.Println("\n=== TRACE SUMMARY ===")
	fmt.Printf("Total Spans Created: %d\n", len(tracer.spans))
}
