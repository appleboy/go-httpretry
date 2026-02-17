package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPerAttemptMiddlewareExecutionCount verifies per-attempt middleware runs N times for N attempts
func TestPerAttemptMiddlewareExecutionCount(t *testing.T) {
	var attemptCount int32

	// Server that fails twice, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Middleware that counts executions
	var middlewareCount int32
	countingMiddleware := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&middlewareCount, 1)
			return next.RoundTrip(req)
		})
	}

	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithPerAttemptMiddleware(countingMiddleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should succeed after 3 attempts
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Middleware should execute 3 times (once per attempt)
	if middlewareCount != 3 {
		t.Errorf("Expected middleware to execute 3 times, got %d", middlewareCount)
	}
}

// TestRequestMiddlewareExecutionCount verifies request-level middleware runs exactly once
func TestRequestMiddlewareExecutionCount(t *testing.T) {
	var attemptCount int32

	// Server that fails twice, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Request middleware that counts executions
	var middlewareCount int32
	countingMiddleware := func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&middlewareCount, 1)
			return next(ctx, req)
		}
	}

	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithRequestMiddleware(countingMiddleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Middleware should execute exactly once (request-level)
	if middlewareCount != 1 {
		t.Errorf("Expected middleware to execute once, got %d", middlewareCount)
	}
}

// TestMiddlewareOrdering verifies first middleware is outermost
func TestMiddlewareOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var executionOrder []string
	var mu sync.Mutex

	middleware1 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "m1-before")
			mu.Unlock()

			resp, err := next.RoundTrip(req)

			mu.Lock()
			executionOrder = append(executionOrder, "m1-after")
			mu.Unlock()

			return resp, err
		})
	}

	middleware2 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "m2-before")
			mu.Unlock()

			resp, err := next.RoundTrip(req)

			mu.Lock()
			executionOrder = append(executionOrder, "m2-after")
			mu.Unlock()

			return resp, err
		})
	}

	client, err := NewClient(
		WithPerAttemptMiddleware(middleware1, middleware2),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// First middleware should be outermost
	expected := []string{"m1-before", "m2-before", "m2-after", "m1-after"}
	if len(executionOrder) != len(expected) {
		t.Fatalf("Expected %d execution steps, got %d", len(expected), len(executionOrder))
	}

	for i, exp := range expected {
		if executionOrder[i] != exp {
			t.Errorf("Step %d: expected %s, got %s", i, exp, executionOrder[i])
		}
	}
}

// TestRequestMiddlewareOrdering verifies request middleware ordering
func TestRequestMiddlewareOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var executionOrder []string
	var mu sync.Mutex

	middleware1 := func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "rm1-before")
			mu.Unlock()

			resp, err := next(ctx, req)

			mu.Lock()
			executionOrder = append(executionOrder, "rm1-after")
			mu.Unlock()

			return resp, err
		}
	}

	middleware2 := func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "rm2-before")
			mu.Unlock()

			resp, err := next(ctx, req)

			mu.Lock()
			executionOrder = append(executionOrder, "rm2-after")
			mu.Unlock()

			return resp, err
		}
	}

	client, err := NewClient(
		WithRequestMiddleware(middleware1, middleware2),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// First middleware should be outermost
	expected := []string{"rm1-before", "rm2-before", "rm2-after", "rm1-after"}
	if len(executionOrder) != len(expected) {
		t.Fatalf("Expected %d execution steps, got %d", len(expected), len(executionOrder))
	}

	for i, exp := range expected {
		if executionOrder[i] != exp {
			t.Errorf("Step %d: expected %s, got %s", i, exp, executionOrder[i])
		}
	}
}

// TestMultipleMiddlewareWithOptions verifies multiple middleware calls chain correctly
func TestMultipleMiddlewareWithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var count1, count2 int32

	middleware1 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&count1, 1)
			return next.RoundTrip(req)
		})
	}

	middleware2 := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&count2, 1)
			return next.RoundTrip(req)
		})
	}

	// Add middleware via multiple WithPerAttemptMiddleware calls
	client, err := NewClient(
		WithPerAttemptMiddleware(middleware1),
		WithPerAttemptMiddleware(middleware2),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if count1 != 1 || count2 != 1 {
		t.Errorf(
			"Expected both middleware to execute once, got count1=%d, count2=%d",
			count1,
			count2,
		)
	}
}

// TestMiddlewareWithCustomHTTPClient verifies middleware works with custom http.Client
func TestMiddlewareWithCustomHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Custom http.Client with custom Transport
	customTransport := &http.Transport{
		MaxIdleConns: 10,
	}
	customClient := &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	var middlewareExecuted bool
	middleware := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			middlewareExecuted = true
			return next.RoundTrip(req)
		})
	}

	client, err := NewClient(
		WithHTTPClient(customClient),
		WithPerAttemptMiddleware(middleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if !middlewareExecuted {
		t.Error("Middleware was not executed")
	}

	// Verify custom client is preserved (timeout should match)
	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("Expected custom timeout to be preserved, got %v", client.httpClient.Timeout)
	}
}

// TestRequestMiddlewareShortCircuit verifies request middleware can skip retry loop
func TestRequestMiddlewareShortCircuit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Server should not be called when middleware short-circuits")
	}))
	defer server.Close()

	// Middleware that short-circuits
	shortCircuitErr := errors.New("short circuit")
	middleware := func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			// Don't call next - return immediately
			return nil, shortCircuitErr
		}
	}

	client, err := NewClient(
		WithRequestMiddleware(middleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	if err == nil {
		t.Fatal("Expected error from short-circuit, got nil")
	}

	if !errors.Is(err, shortCircuitErr) {
		t.Errorf("Expected short circuit error, got: %v", err)
	}
}

// TestContextPropagation verifies context flows through middleware
func TestContextPropagation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	type contextKey string
	const testKey contextKey = "test"

	// Middleware that checks context value
	var contextValue string
	middleware := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if val := req.Context().Value(testKey); val != nil {
				contextValue = val.(string)
			}
			return next.RoundTrip(req)
		})
	}

	client, err := NewClient(
		WithPerAttemptMiddleware(middleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.WithValue(context.Background(), testKey, "test-value")
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if contextValue != "test-value" {
		t.Errorf("Expected context value 'test-value', got '%s'", contextValue)
	}
}

// TestLoggingMiddleware verifies LoggingMiddleware functionality
func TestLoggingMiddleware(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use test logger
	logger := &middlewareTestLogger{}

	client, err := NewClient(
		WithPerAttemptMiddleware(LoggingMiddleware(logger)),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should have logged at least 2 messages (start and complete)
	if len(logger.messages) < 2 {
		t.Errorf("Expected at least 2 log messages, got %d", len(logger.messages))
	}
}

// TestHeaderMiddleware verifies HeaderMiddleware adds headers
func TestHeaderMiddleware(t *testing.T) {
	receivedHeaders := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders["X-Custom-1"] = r.Header.Get("X-Custom-1")
		receivedHeaders["X-Custom-2"] = r.Header.Get("X-Custom-2")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom-1": "value1",
		"X-Custom-2": "value2",
	}

	client, err := NewClient(
		WithPerAttemptMiddleware(HeaderMiddleware(headers)),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedHeaders["X-Custom-1"] != "value1" {
		t.Errorf("Expected X-Custom-1=value1, got %s", receivedHeaders["X-Custom-1"])
	}
	if receivedHeaders["X-Custom-2"] != "value2" {
		t.Errorf("Expected X-Custom-2=value2, got %s", receivedHeaders["X-Custom-2"])
	}
}

// TestRateLimitMiddleware verifies rate limiting behavior
func TestRateLimitMiddleware(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	limiter := &testRateLimiter{delay: 50 * time.Millisecond}

	client, err := NewClient(
		WithRequestMiddleware(RateLimitMiddleware(limiter)),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	start := time.Now()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	elapsed := time.Since(start)

	// Should have waited at least the rate limit delay
	if elapsed < 50*time.Millisecond {
		t.Errorf("Expected delay >= 50ms, got %v", elapsed)
	}

	if limiter.waitCalled != 1 {
		t.Errorf("Expected Wait to be called once, got %d", limiter.waitCalled)
	}
}

// TestCircuitBreakerMiddleware verifies circuit breaker behavior
func TestCircuitBreakerMiddleware(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := &testCircuitBreaker{open: true}

	client, err := NewClient(
		WithRequestMiddleware(CircuitBreakerMiddleware(cb)),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	// Should fail because circuit breaker is open
	if err == nil {
		t.Fatal("Expected error when circuit breaker is open, got nil")
	}

	if cb.allowCalled != 1 {
		t.Errorf("Expected Allow to be called once, got %d", cb.allowCalled)
	}
}

// TestTracingRequestMiddleware verifies tracing middleware
func TestTracingRequestMiddleware(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tracer := &middlewareTestTracer{}

	client, err := NewClient(
		WithRequestMiddleware(TracingRequestMiddleware(tracer)),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should have created exactly one span
	if tracer.spanCount != 1 {
		t.Errorf("Expected 1 span, got %d", tracer.spanCount)
	}
}

// TestCombinedMiddleware verifies both middleware levels work together
func TestCombinedMiddleware(t *testing.T) {
	var attemptCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var perAttemptCount, requestCount int32

	perAttemptMiddleware := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&perAttemptCount, 1)
			return next.RoundTrip(req)
		})
	}

	requestMiddleware := func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&requestCount, 1)
			return next(ctx, req)
		}
	}

	client, err := NewClient(
		WithMaxRetries(2),
		WithInitialRetryDelay(10*time.Millisecond),
		WithPerAttemptMiddleware(perAttemptMiddleware),
		WithRequestMiddleware(requestMiddleware),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Request middleware should execute once
	if requestCount != 1 {
		t.Errorf("Expected request middleware to execute once, got %d", requestCount)
	}

	// Per-attempt middleware should execute twice (initial + 1 retry)
	if perAttemptCount != 2 {
		t.Errorf("Expected per-attempt middleware to execute twice, got %d", perAttemptCount)
	}
}

// Test helpers

type middlewareTestLogger struct {
	messages []string
}

func (l *middlewareTestLogger) Debug(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, msg)
}

func (l *middlewareTestLogger) Info(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, msg)
}

func (l *middlewareTestLogger) Warn(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, msg)
}

func (l *middlewareTestLogger) Error(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, msg)
}

type testRateLimiter struct {
	delay      time.Duration
	waitCalled int32
}

func (r *testRateLimiter) Wait(ctx context.Context) error {
	atomic.AddInt32(&r.waitCalled, 1)
	select {
	case <-time.After(r.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type testCircuitBreaker struct {
	open         bool
	allowCalled  int32
	successCount int32
	failureCount int32
}

func (cb *testCircuitBreaker) Allow() error {
	atomic.AddInt32(&cb.allowCalled, 1)
	if cb.open {
		return fmt.Errorf("circuit breaker open")
	}
	return nil
}

func (cb *testCircuitBreaker) RecordSuccess() {
	atomic.AddInt32(&cb.successCount, 1)
}

func (cb *testCircuitBreaker) RecordFailure() {
	atomic.AddInt32(&cb.failureCount, 1)
}

type middlewareTestTracer struct {
	spanCount int32
}

func (t *middlewareTestTracer) StartSpan(
	ctx context.Context,
	name string,
	attrs ...Attribute,
) (context.Context, Span) {
	atomic.AddInt32(&t.spanCount, 1)
	return ctx, &middlewareTestSpan{}
}

type middlewareTestSpan struct{}

func (s *middlewareTestSpan) End()                                     {}
func (s *middlewareTestSpan) SetAttributes(attrs ...Attribute)         {}
func (s *middlewareTestSpan) SetStatus(status, message string)         {}
func (s *middlewareTestSpan) AddEvent(name string, attrs ...Attribute) {}
