package retry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should return the last response with 500 status
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
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

	resp, err := client.Do(ctx, req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected context cancellation error")
	}

	// Should only have 1 attempt before context is cancelled during retry delay
	if attempts.Load() > 2 {
		t.Errorf("expected at most 2 attempts before cancellation, got %d", attempts.Load())
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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
	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(context.Background(), req)
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
