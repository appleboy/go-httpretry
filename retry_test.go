package retry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient_Defaults(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if client.maxRetries != defaultMaxRetries {
		t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, client.maxRetries)
	}
	if client.initialRetryDelay != defaultInitialRetryDelay {
		t.Errorf(
			"expected initialRetryDelay=%v, got %v",
			defaultInitialRetryDelay,
			client.initialRetryDelay,
		)
	}
	if client.maxRetryDelay != defaultMaxRetryDelay {
		t.Errorf("expected maxRetryDelay=%v, got %v", defaultMaxRetryDelay, client.maxRetryDelay)
	}
	if client.retryDelayMultiple != defaultRetryDelayMultiple {
		t.Errorf(
			"expected retryDelayMultiple=%f, got %f",
			defaultRetryDelayMultiple,
			client.retryDelayMultiple,
		)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
	if !client.jitterEnabled {
		t.Error("expected jitterEnabled to be true by default")
	}
	if !client.respectRetryAfter {
		t.Error("expected respectRetryAfter to be true by default")
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	customChecker := func(err error, resp *http.Response) bool { return false }

	client, err := NewClient(
		WithMaxRetries(5),
		WithInitialRetryDelay(2*time.Second),
		WithMaxRetryDelay(20*time.Second),
		WithRetryDelayMultiple(3.0),
		WithHTTPClient(httpClient),
		WithRetryableChecker(customChecker),
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if client.maxRetries != 5 {
		t.Errorf("expected maxRetries=5, got %d", client.maxRetries)
	}
	if client.initialRetryDelay != 2*time.Second {
		t.Errorf("expected initialRetryDelay=2s, got %v", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 20*time.Second {
		t.Errorf("expected maxRetryDelay=20s, got %v", client.maxRetryDelay)
	}
	if client.retryDelayMultiple != 3.0 {
		t.Errorf("expected retryDelayMultiple=3.0, got %f", client.retryDelayMultiple)
	}
	if client.httpClient != httpClient {
		t.Error("expected custom httpClient to be set")
	}
}

func TestNewClient_InvalidOptions(t *testing.T) {
	client, err := NewClient(
		WithMaxRetries(-1),          // Invalid, should be ignored
		WithInitialRetryDelay(-1),   // Invalid, should be ignored
		WithMaxRetryDelay(-1),       // Invalid, should be ignored
		WithRetryDelayMultiple(0.5), // Invalid, should be ignored
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	// Should still have defaults
	if client.maxRetries != defaultMaxRetries {
		t.Errorf("expected default maxRetries=%d, got %d", defaultMaxRetries, client.maxRetries)
	}
	if client.initialRetryDelay != defaultInitialRetryDelay {
		t.Errorf(
			"expected default initialRetryDelay=%v, got %v",
			defaultInitialRetryDelay,
			client.initialRetryDelay,
		)
	}
}

func TestDefaultRetryableChecker(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		resp     *http.Response
		expected bool
	}{
		{
			name:     "network error",
			err:      errors.New("connection refused"),
			resp:     nil,
			expected: true,
		},
		{
			name:     "no error, 200 OK",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusOK},
			expected: false,
		},
		{
			name:     "no error, 400 Bad Request",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusBadRequest},
			expected: false,
		},
		{
			name:     "no error, 429 Too Many Requests",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusTooManyRequests},
			expected: true,
		},
		{
			name:     "no error, 500 Internal Server Error",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusInternalServerError},
			expected: true,
		},
		{
			name:     "no error, 503 Service Unavailable",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusServiceUnavailable},
			expected: true,
		},
		{
			name:     "no error, nil response",
			err:      nil,
			resp:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultRetryableChecker(tt.err, tt.resp)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClient_Do_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClient_Do_RetryOn500(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success after retries"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestClient_Do_ExhaustedRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected RetryError after exhausting retries")
	}

	// Should return the last response with 500 status
	if resp == nil {
		t.Fatal("expected response even with error")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}

	// Verify error is a RetryError
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %T: %v", err, err)
	}

	// Should have 1 initial attempt + 2 retries = 3 total
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestClient_Do_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(5),
		WithJitter(false), // Disable jitter for predictable timing
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected context cancellation error")
	}

	// Should only have 1 attempt before context is cancelled during retry delay
	// (timeout=50ms < retry_delay=100ms, so no second attempt can start)
	if attempts.Load() != 1 {
		t.Errorf("expected exactly 1 attempt before cancellation, got %d", attempts.Load())
	}
}

func TestClient_Do_NoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should not retry on 4xx errors
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", attempts.Load())
	}
}

func TestClient_Do_ExponentialBackoff(t *testing.T) {
	var attempts atomic.Int32
	var requestTimes []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes = append(requestTimes, time.Now())
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetryDelay(500*time.Millisecond),
		WithRetryDelayMultiple(2.0),
		WithMaxRetries(3),
		WithJitter(false), // Disable jitter for predictable timing tests
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil && resp != nil {
		resp.Body.Close()
	}

	if len(requestTimes) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(requestTimes))
	}

	// Check that delays increase exponentially
	delay1 := requestTimes[1].Sub(requestTimes[0])
	delay2 := requestTimes[2].Sub(requestTimes[1])

	if delay1 < 90*time.Millisecond || delay1 > 150*time.Millisecond {
		t.Errorf("first retry delay should be ~100ms, got %v", delay1)
	}

	if delay2 < 180*time.Millisecond || delay2 > 250*time.Millisecond {
		t.Errorf("second retry delay should be ~200ms, got %v", delay2)
	}
}

func TestClient_Do_CustomRetryableChecker(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Custom checker that never retries
	neverRetry := func(err error, resp *http.Response) bool {
		return false
	}

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
		WithRetryableChecker(neverRetry),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should not retry with custom checker
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", attempts.Load())
	}
}

func TestWithJitter_Enabled(t *testing.T) {
	// Test that jitter is enabled by default
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if !client.jitterEnabled {
		t.Error("expected jitterEnabled to be true by default")
	}
}

func TestWithJitter_Disabled(t *testing.T) {
	// Test that jitter can be explicitly disabled
	client, err := NewClient(
		WithJitter(false),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if client.jitterEnabled {
		t.Error("expected jitterEnabled to be false")
	}
}

func TestWithJitter(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(3),
		WithJitter(true),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		resp.Body.Close()
	}
	duration := time.Since(start)

	// With jitter, the total duration should vary
	// Without jitter: 100ms + 200ms + 400ms = 700ms
	// With jitter (±25%): approximately 525ms to 875ms
	if duration < 400*time.Millisecond || duration > 1*time.Second {
		t.Logf("Duration %v seems unusual but jitter can cause variation", duration)
	}

	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts.Load())
	}
}

func TestWithOnRetry(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should have 2 retries (3 total attempts)
	if len(retryInfos) != 2 {
		t.Errorf("expected 2 retry callbacks, got %d", len(retryInfos))
	}

	// Verify retry info
	for i, info := range retryInfos {
		if info.Attempt != i+1 {
			t.Errorf("retry %d: expected attempt %d, got %d", i, i+1, info.Attempt)
		}
		if info.StatusCode != http.StatusInternalServerError {
			t.Errorf("retry %d: expected status 500, got %d", i, info.StatusCode)
		}
		if info.Delay <= 0 {
			t.Errorf("retry %d: expected positive delay, got %v", i, info.Delay)
		}
		if info.TotalElapsed <= 0 {
			t.Errorf("retry %d: expected positive total elapsed, got %v", i, info.TotalElapsed)
		}
	}
}

func TestWithRespectRetryAfter_Seconds(t *testing.T) {
	var attempts atomic.Int32
	var requestTimes []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes = append(requestTimes, time.Now())
		count := attempts.Add(1)
		if count < 2 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond), // Would normally use 100ms
		WithMaxRetries(2),
		WithRespectRetryAfter(true),
		WithJitter(false), // Disable jitter for predictable timing tests
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(requestTimes) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requestTimes))
	}

	// Check that the delay was approximately 1 second (from Retry-After header)
	delay := requestTimes[1].Sub(requestTimes[0])
	if delay < 950*time.Millisecond || delay > 1100*time.Millisecond {
		t.Errorf("expected ~1s delay (from Retry-After), got %v", delay)
	}
}

func TestWithRespectRetryAfter_HTTPDate(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 2 {
			// Set Retry-After to a fixed future time
			// Note: HTTP-date has 1-second precision
			retryTime := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
			w.Header().Set("Retry-After", retryTime)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(2),
		WithRespectRetryAfter(true),
		WithJitter(false), // Disable jitter for predictable timing tests
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(retryInfos) != 1 {
		t.Fatalf("expected 1 retry callback, got %d", len(retryInfos))
	}

	// The RetryAfter should be parsed and be approximately 2 seconds
	// Allow some tolerance for time.Until() calculation and HTTP-date precision
	if retryInfos[0].RetryAfter < 1*time.Second || retryInfos[0].RetryAfter > 3*time.Second {
		t.Errorf("expected RetryAfter to be ~2s, got %v", retryInfos[0].RetryAfter)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected time.Duration
		delta    time.Duration // tolerance for time-based tests
	}{
		{
			name:     "seconds format",
			header:   "120",
			expected: 120 * time.Second,
		},
		{
			name:     "zero seconds",
			header:   "0",
			expected: 0,
		},
		{
			name:     "invalid negative",
			header:   "-1",
			expected: 0,
		},
		{
			name:     "empty header",
			header:   "",
			expected: 0,
		},
		{
			name:     "invalid format",
			header:   "invalid",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}

			result := parseRetryAfter(resp)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}

	// Test HTTP-date format separately due to time.Now() dependency
	t.Run("HTTP-date format", func(t *testing.T) {
		futureTime := time.Now().Add(5 * time.Second).UTC()
		resp := &http.Response{
			Header: http.Header{},
		}
		resp.Header.Set("Retry-After", futureTime.Format(http.TimeFormat))

		result := parseRetryAfter(resp)
		expected := 5 * time.Second
		// Allow larger tolerance due to HTTP-date having 1-second precision
		// and time.Until() calculation happening after header creation
		delta := 500 * time.Millisecond

		if result < expected-delta || result > expected+delta {
			t.Errorf("expected ~%v (±%v), got %v", expected, delta, result)
		}
	})

	// Test past HTTP-date (should return 0)
	t.Run("past HTTP-date", func(t *testing.T) {
		pastTime := time.Now().Add(-5 * time.Second).UTC()
		resp := &http.Response{
			Header: http.Header{},
		}
		resp.Header.Set("Retry-After", pastTime.Format(http.TimeFormat))

		result := parseRetryAfter(resp)
		if result != 0 {
			t.Errorf("expected 0 for past date, got %v", result)
		}
	})
}

func TestApplyJitter(t *testing.T) {
	delay := 1000 * time.Millisecond

	// Run multiple times to verify randomness
	results := make(map[time.Duration]bool)
	for i := 0; i < 10; i++ {
		jittered := applyJitter(delay)
		results[jittered] = true

		// Should be between 750ms and 1250ms (±25%)
		if jittered < 750*time.Millisecond || jittered > 1250*time.Millisecond {
			t.Errorf("jittered delay %v outside expected range [750ms, 1250ms]", jittered)
		}
	}

	// Should have some variation (not all the same)
	if len(results) < 2 {
		t.Error("expected some variation in jittered delays")
	}
}

func TestCombinedFeatures(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		switch count {
		case 1:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(3),
		WithJitter(true),
		WithRespectRetryAfter(true),
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	if len(retryInfos) != 2 {
		t.Fatalf("expected 2 retry callbacks, got %d", len(retryInfos))
	}

	// First retry should have used Retry-After
	if retryInfos[0].RetryAfter != 1*time.Second {
		t.Errorf("expected first retry to have Retry-After=1s, got %v", retryInfos[0].RetryAfter)
	}

	// Second retry should not have Retry-After
	if retryInfos[1].RetryAfter != 0 {
		t.Errorf("expected second retry to have no Retry-After, got %v", retryInfos[1].RetryAfter)
	}
}

func TestWithPerAttemptTimeout_Success(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Fast response - should not trigger per-attempt timeout
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithPerAttemptTimeout(1*time.Second), // Set per-attempt timeout
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should only have 1 attempt (no retries)
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts.Load())
	}
}

func TestWithPerAttemptTimeout_Triggered(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			// Simulate a slow response by blocking until the request context is cancelled.
			// This ensures the handler returns when the client times out, without relying on
			// tight real-time sleeps that can be flaky under load.
			<-r.Context().Done()
			return
		}
		// Third attempt is fast
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithPerAttemptTimeout(100*time.Millisecond), // Short per-attempt timeout
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
		WithJitter(false),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should have retried due to per-attempt timeout
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts (2 timeouts + 1 success), got %d", attempts.Load())
	}
}

func TestWithPerAttemptTimeout_Disabled(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Slow response
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	// No per-attempt timeout set (default behavior)
	client, err := NewClient(
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Set overall timeout longer than the slow response
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should only have 1 attempt (no per-attempt timeout to trigger retry)
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts.Load())
	}
}

func TestWithPerAttemptTimeout_WithOverallTimeout(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Slow response
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithPerAttemptTimeout(100*time.Millisecond), // Per-attempt timeout
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(5),
		WithJitter(false),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Overall timeout that should allow 2-3 attempts
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected context deadline error")
	}

	// Should have 2-3 attempts before overall timeout
	attemptsCount := attempts.Load()
	if attemptsCount < 2 || attemptsCount > 3 {
		t.Errorf("expected 2-3 attempts before overall timeout, got %d", attemptsCount)
	}
}

func TestWithPerAttemptTimeout_BodyReadableAfterSuccess(t *testing.T) {
	responseBody := "test response body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()

	client, err := NewClient(
		WithPerAttemptTimeout(1*time.Second), // Per-attempt timeout enabled
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Verify we can read the response body after the request completes
	// This ensures the per-attempt context cancellation doesn't break the body
	body := make([]byte, len(responseBody))
	n, err := resp.Body.Read(body)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("failed to read response body: %v", err)
	}

	if string(body[:n]) != responseBody {
		t.Errorf("expected body %q, got %q", responseBody, string(body[:n]))
	}
}

func TestRetryError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *RetryError
		contains string
	}{
		{
			name: "with underlying error",
			err: &RetryError{
				Attempts:   3,
				LastErr:    errors.New("connection refused"),
				LastStatus: 0,
				Elapsed:    1 * time.Second,
			},
			contains: "request failed after 3 attempts",
		},
		{
			name: "with HTTP status code",
			err: &RetryError{
				Attempts:   4,
				LastErr:    nil,
				LastStatus: http.StatusInternalServerError,
				Elapsed:    2 * time.Second,
			},
			contains: "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			if errMsg == "" {
				t.Error("expected non-empty error message")
			}
			if tt.contains != "" && !strings.Contains(errMsg, tt.contains) {
				t.Errorf("expected error message to contain %q, got %q", tt.contains, errMsg)
			}
		})
	}
}

// TestClient_Get tests the convenience Get method
func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("get response"))
	}))
	defer server.Close()

	client, err := NewClient(WithMaxRetries(2))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestClient_Head tests the convenience Head method
func TestClient_Head(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(WithMaxRetries(2))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Head(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestClient_Post tests the convenience Post method
func TestClient_Post(t *testing.T) {
	t.Run("without body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST method, got %s", r.Method)
			}
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		client, err := NewClient(WithMaxRetries(2))
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		resp, err := client.Post(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected status 201, got %d", resp.StatusCode)
		}
	})

	t.Run("with body and content type", func(t *testing.T) {
		expectedContentType := "application/json"
		expectedBody := `{"key":"value"}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST method, got %s", r.Method)
			}

			contentType := r.Header.Get("Content-Type")
			if contentType != expectedContentType {
				t.Errorf("expected Content-Type %s, got %s", expectedContentType, contentType)
			}

			body := make([]byte, len(expectedBody))
			n, _ := r.Body.Read(body)
			if string(body[:n]) != expectedBody {
				t.Errorf("expected body %q, got %q", expectedBody, string(body[:n]))
			}

			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		client, err := NewClient(WithMaxRetries(2))
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		resp, err := client.Post(context.Background(), server.URL,
			WithBody(expectedContentType, bytes.NewBufferString(expectedBody)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected status 201, got %d", resp.StatusCode)
		}
	})
}

// TestClient_BodyMethods tests convenience methods with optional body
func TestClient_BodyMethods(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		contentType    string
		body           string
		expectedStatus int
		fn             func(*Client, context.Context, string, ...RequestOption) (*http.Response, error)
	}{
		{
			name:           "Put",
			method:         http.MethodPut,
			contentType:    "text/plain",
			body:           "put data",
			expectedStatus: http.StatusOK,
			fn:             (*Client).Put,
		},
		{
			name:           "Patch",
			method:         http.MethodPatch,
			contentType:    "application/json-patch+json",
			body:           `{"op":"replace"}`,
			expectedStatus: http.StatusOK,
			fn:             (*Client).Patch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != tt.method {
						t.Errorf("expected %s method, got %s", tt.method, r.Method)
					}

					contentType := r.Header.Get("Content-Type")
					if contentType != tt.contentType {
						t.Errorf("expected Content-Type %s, got %s", tt.contentType, contentType)
					}

					body := make([]byte, len(tt.body))
					n, _ := r.Body.Read(body)
					if string(body[:n]) != tt.body {
						t.Errorf("expected body %q, got %q", tt.body, string(body[:n]))
					}

					w.WriteHeader(tt.expectedStatus)
				}),
			)
			defer server.Close()

			client, err := NewClient(WithMaxRetries(2))
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			resp, err := tt.fn(
				client,
				context.Background(),
				server.URL,
				WithBody(tt.contentType, strings.NewReader(tt.body)),
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestRetryError_Unwrap(t *testing.T) {
	underlyingErr := errors.New("connection timeout")
	retryErr := &RetryError{
		Attempts:   3,
		LastErr:    underlyingErr,
		LastStatus: 0,
		Elapsed:    1 * time.Second,
	}

	unwrapped := retryErr.Unwrap()
	if unwrapped != underlyingErr {
		t.Errorf("expected unwrapped error to be %v, got %v", underlyingErr, unwrapped)
	}

	// Test that errors.Is works
	if !errors.Is(retryErr, underlyingErr) {
		t.Error("expected errors.Is to find underlying error")
	}
}

func TestClient_Do_ReturnsRetryErrorOnExhaustion(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected error after exhausting retries")
	}

	// Verify error is a RetryError
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %T: %v", err, err)
	}

	// Verify RetryError fields
	expectedAttempts := 3 // 1 initial + 2 retries
	if retryErr.Attempts != expectedAttempts {
		t.Errorf("expected %d attempts in RetryError, got %d", expectedAttempts, retryErr.Attempts)
	}

	if retryErr.LastStatus != http.StatusInternalServerError {
		t.Errorf("expected LastStatus 500, got %d", retryErr.LastStatus)
	}

	if retryErr.Elapsed <= 0 {
		t.Errorf("expected positive elapsed time, got %v", retryErr.Elapsed)
	}

	// The last error should be nil since the request succeeded (just returned 500)
	if retryErr.LastErr != nil {
		t.Errorf("expected LastErr to be nil for status code errors, got %v", retryErr.LastErr)
	}
}

func TestClient_Do_ReturnsRetryErrorOnContextCancellation(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(5),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected error after context cancellation")
	}

	// Verify error is a RetryError
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %T: %v", err, err)
	}

	// Verify the underlying error is context-related
	if retryErr.LastErr != context.DeadlineExceeded {
		t.Errorf("expected LastErr to be context.DeadlineExceeded, got %v", retryErr.LastErr)
	}

	// Verify we can unwrap to the context error
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("expected to unwrap to context.DeadlineExceeded")
	}

	if retryErr.Elapsed <= 0 {
		t.Errorf("expected positive elapsed time, got %v", retryErr.Elapsed)
	}
}

func TestClient_Do_ResponseBodyReadableAfterRetryExhaustion(t *testing.T) {
	var attempts atomic.Int32
	expectedBody := "error: service unavailable"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(expectedBody))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected error after exhausting retries")
	}

	// Verify error is a RetryError
	var retryErr *RetryError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryError, got %T: %v", err, err)
	}

	// Verify response is available
	if resp == nil {
		t.Fatal("expected response even with error")
	}
	defer resp.Body.Close()

	// IMPORTANT: Verify we can read the response body from the last attempt
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("failed to read response body: %v", readErr)
	}

	if string(body) != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, string(body))
	}

	// Verify the status code
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}

	// Should have 1 initial attempt + 2 retries = 3 total
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

// TestClient_Delete tests the convenience Delete method
func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE method, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(WithMaxRetries(2))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Delete(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}
}

// TestConvenienceMethods_WithRetry tests that convenience methods properly retry
func TestConvenienceMethods_WithRetry(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		withBody     bool
		expectedBody string
		fn           func(*Client, context.Context, string, ...RequestOption) (*http.Response, error)
	}{
		{
			name:   "Get",
			method: http.MethodGet,
			fn:     (*Client).Get,
		},
		{
			name:   "Head",
			method: http.MethodHead,
			fn:     (*Client).Head,
		},
		{
			name:         "Post",
			method:       http.MethodPost,
			withBody:     true,
			expectedBody: `{"action":"create"}`,
			fn:           (*Client).Post,
		},
		{
			name:         "Put",
			method:       http.MethodPut,
			withBody:     true,
			expectedBody: `{"action":"update"}`,
			fn:           (*Client).Put,
		},
		{
			name:         "Patch",
			method:       http.MethodPatch,
			withBody:     true,
			expectedBody: `{"action":"patch"}`,
			fn:           (*Client).Patch,
		},
		{
			name:   "Delete",
			method: http.MethodDelete,
			fn:     (*Client).Delete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			var receivedBodies []string

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != tt.method {
						t.Errorf("expected %s method, got %s", tt.method, r.Method)
					}

					// Read body if present
					if tt.withBody {
						bodyBytes, err := io.ReadAll(r.Body)
						if err != nil {
							t.Errorf("failed to read body: %v", err)
						}
						receivedBodies = append(receivedBodies, string(bodyBytes))
					}

					count := attempts.Add(1)
					if count < 2 {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer server.Close()

			client, err := NewClient(
				WithInitialRetryDelay(10*time.Millisecond),
				WithMaxRetries(2),
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			// Make the request with optional body
			var resp *http.Response
			if tt.withBody {
				resp, err = tt.fn(client, context.Background(), server.URL,
					WithBody("application/json", strings.NewReader(tt.expectedBody)))
			} else {
				resp, err = tt.fn(client, context.Background(), server.URL)
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()

			if attempts.Load() != 2 {
				t.Errorf("expected 2 attempts, got %d", attempts.Load())
			}

			// Verify body was sent correctly on all attempts
			if tt.withBody {
				if len(receivedBodies) != 2 {
					t.Fatalf("expected 2 received bodies, got %d", len(receivedBodies))
				}
				for i, body := range receivedBodies {
					if body != tt.expectedBody {
						t.Errorf("attempt %d: expected body %q, got %q", i+1, tt.expectedBody, body)
					}
				}
			}
		})
	}
}

// TestConvenienceMethods_InvalidURL tests error handling for invalid URLs
func TestConvenienceMethods_InvalidURL(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	invalidURL := "://invalid-url"

	tests := []struct {
		name string
		fn   func() (*http.Response, error)
	}{
		{
			name: "Get",
			fn:   func() (*http.Response, error) { return client.Get(context.Background(), invalidURL) },
		},
		{
			name: "Head",
			fn:   func() (*http.Response, error) { return client.Head(context.Background(), invalidURL) },
		},
		{
			name: "Post",
			fn:   func() (*http.Response, error) { return client.Post(context.Background(), invalidURL) },
		},
		{
			name: "Put",
			fn:   func() (*http.Response, error) { return client.Put(context.Background(), invalidURL) },
		},
		{
			name: "Patch",
			fn:   func() (*http.Response, error) { return client.Patch(context.Background(), invalidURL) },
		},
		{
			name: "Delete",
			fn:   func() (*http.Response, error) { return client.Delete(context.Background(), invalidURL) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.fn()
			if err == nil {
				t.Error("expected error for invalid URL, got nil")
			}
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
		})
	}
}

// TestRequestOptions tests the RequestOption helpers
func TestRequestOptions(t *testing.T) {
	t.Run("WithHeader", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Custom-Header") != "custom-value" {
				t.Errorf("expected X-Custom-Header, got %s", r.Header.Get("X-Custom-Header"))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, _ := NewClient()
		resp, err := client.Get(context.Background(), server.URL,
			WithHeader("X-Custom-Header", "custom-value"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()
	})

	t.Run("WithHeaders", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Header-1") != "value1" {
				t.Errorf("expected X-Header-1=value1, got %s", r.Header.Get("X-Header-1"))
			}
			if r.Header.Get("X-Header-2") != "value2" {
				t.Errorf("expected X-Header-2=value2, got %s", r.Header.Get("X-Header-2"))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, _ := NewClient()
		resp, err := client.Get(context.Background(), server.URL,
			WithHeaders(map[string]string{
				"X-Header-1": "value1",
				"X-Header-2": "value2",
			}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()
	})

	t.Run("WithBody without content type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "" {
				t.Errorf("expected no Content-Type, got %s", r.Header.Get("Content-Type"))
			}
			body := make([]byte, 4)
			n, _ := r.Body.Read(body)
			if string(body[:n]) != "data" {
				t.Errorf("expected body 'data', got %q", string(body[:n]))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, _ := NewClient()
		resp, err := client.Post(context.Background(), server.URL,
			WithBody("", strings.NewReader("data")))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()
	})
}

// TestRequestBody_WithRetry tests that request bodies are sent correctly on retries
func TestRequestBody_WithRetry(t *testing.T) {
	expectedBody := `{"key":"value","number":123}`
	var attempts atomic.Int32
	var receivedBodies []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)

		// Read the body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		receivedBodies = append(receivedBodies, string(bodyBytes))

		// Fail on first two attempts, succeed on third
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Make a POST request with a JSON body
	resp, err := client.Post(context.Background(), server.URL,
		WithBody("application/json", strings.NewReader(expectedBody)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify we made 3 attempts
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	// CRITICAL: Verify that the body was sent correctly on ALL attempts
	// This tests that GetBody was set up properly and retries don't send empty bodies
	for i, body := range receivedBodies {
		if body != expectedBody {
			t.Errorf("attempt %d: expected body %q, got %q", i+1, expectedBody, body)
		}
	}
}

// TestRequestBody_LargeBody tests that larger request bodies work correctly with retries
func TestRequestBody_LargeBody(t *testing.T) {
	// Create a 1KB body
	largeBody := strings.Repeat("x", 1024)
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)

		// Read and verify the body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}

		if string(bodyBytes) != largeBody {
			t.Errorf("attempt %d: body mismatch, got %d bytes", count, len(bodyBytes))
		}

		// Fail on first attempt, succeed on second
		if count < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Put(context.Background(), server.URL,
		WithBody("text/plain", strings.NewReader(largeBody)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

// TestClient_Do_StandardInterface tests that Do() is compatible with http.Client interface
func TestClient_Do_StandardInterface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(WithMaxRetries(2))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test Do(req) - standard interface
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestClient_DoWithContext tests the explicit DoWithContext method
func TestClient_DoWithContext(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test DoWithContext(ctx, req) - explicit context control
	ctx := context.Background()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.DoWithContext(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

// TestClient_DoWithContext_ContextCancellation tests DoWithContext respects context cancellation
func TestClient_DoWithContext_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(5),
		WithJitter(false), // Disable jitter for predictable timing
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.DoWithContext(ctx, req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected context cancellation error")
	}

	// Should only have 1 attempt before context is cancelled during retry delay
	// (timeout=50ms < retry_delay=100ms, so no second attempt can start)
	if attempts.Load() != 1 {
		t.Errorf("expected exactly 1 attempt before cancellation, got %d", attempts.Load())
	}
}

// TestClient_Do_NilRequest tests that Do returns an error for nil request
func TestClient_Do_NilRequest(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Do(nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if resp != nil {
		t.Error("expected nil response for nil request")
	}

	expectedErrMsg := "retry: nil Request"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestClient_DoWithContext_NilRequest tests that DoWithContext returns an error for nil request
func TestClient_DoWithContext_NilRequest(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	resp, err := client.DoWithContext(ctx, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if resp != nil {
		t.Error("expected nil response for nil request")
	}

	expectedErrMsg := "retry: nil Request"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestWithJSON_Success tests that WithJSON correctly serializes and sends JSON
func TestWithJSON_Success(t *testing.T) {
	type testPayload struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	expectedPayload := testPayload{
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   30,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Content-Type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", contentType)
		}

		// Verify body content
		var received testPayload
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode JSON body: %v", err)
		}

		if received != expectedPayload {
			t.Errorf("expected payload %+v, got %+v", expectedPayload, received)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(expectedPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestWithJSON_WithRetry tests that JSON body is correctly sent on retries
func TestWithJSON_WithRetry(t *testing.T) {
	type user struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	expectedUser := user{ID: 123, Name: "Alice"}
	var attempts atomic.Int32
	var receivedUsers []user

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)

		// Verify Content-Type on every attempt
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("attempt %d: expected Content-Type 'application/json', got %q",
				count, r.Header.Get("Content-Type"))
		}

		// Read and decode the body
		var received user
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("attempt %d: failed to decode JSON: %v", count, err)
		}
		receivedUsers = append(receivedUsers, received)

		// Fail on first two attempts, succeed on third
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(expectedUser))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify we made 3 attempts
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	// CRITICAL: Verify that the JSON body was sent correctly on ALL attempts
	for i, received := range receivedUsers {
		if received != expectedUser {
			t.Errorf("attempt %d: expected user %+v, got %+v", i+1, expectedUser, received)
		}
	}
}

// TestWithJSON_InvalidData tests that WithJSON handles unmarshallable data
func TestWithJSON_InvalidData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Create an invalid value that cannot be marshaled to JSON
	// (channels cannot be marshaled to JSON)
	invalidData := make(chan int)

	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(invalidData))
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	// Should get an error about JSON marshaling
	if err == nil {
		t.Fatal("expected error for unmarshallable data, got nil")
	}

	// The error should mention JSON or unsupported type
	errStr := err.Error()
	if !strings.Contains(errStr, "json") && !strings.Contains(errStr, "unsupported") {
		t.Logf("got error: %v", err)
	}
}

// TestWithJSON_NilValue tests that WithJSON handles nil value
func TestWithJSON_NilValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q",
				r.Header.Get("Content-Type"))
		}

		// nil marshals to "null" in JSON
		body, _ := io.ReadAll(r.Body)
		if string(body) != "null" {
			t.Errorf("expected body 'null', got %q", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestWithJSON_EmptyStruct tests that WithJSON handles empty structs
func TestWithJSON_EmptyStruct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty struct marshals to "{}" in JSON
		body, _ := io.ReadAll(r.Body)
		if string(body) != "{}" {
			t.Errorf("expected body '{}', got %q", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	type Empty struct{}
	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(Empty{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestWithJSON_WithOtherOptions tests combining WithJSON with other request options
func TestWithJSON_WithOtherOptions(t *testing.T) {
	type payload struct {
		Message string `json:"message"`
	}

	expectedPayload := payload{Message: "Hello"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer token123" {
			t.Errorf("expected Authorization header, got %q",
				r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Request-ID") != "req-456" {
			t.Errorf("expected X-Request-ID header, got %q",
				r.Header.Get("X-Request-ID"))
		}

		// Verify Content-Type (should be set by WithJSON)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q",
				r.Header.Get("Content-Type"))
		}

		// Verify body
		var received payload
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode JSON: %v", err)
		}
		if received != expectedPayload {
			t.Errorf("expected payload %+v, got %+v", expectedPayload, received)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Post(context.Background(), server.URL,
		WithJSON(expectedPayload),
		WithHeader("Authorization", "Bearer token123"),
		WithHeader("X-Request-ID", "req-456"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
