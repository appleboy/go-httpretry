package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestClient_DelayLoggingAccuracy verifies that the logger.Warn "next_delay" value is correct
func TestClient_DelayLoggingAccuracy(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mockLogger := &MockLogger{}
	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithRetryDelayMultiple(2.0),
		WithMaxRetries(3),
		WithJitter(false), // Disable jitter for predictable values
		WithLogger(mockLogger),
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
		t.Fatal("expected error after exhausting retries")
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// Should have 4 attempts (1 initial + 3 retries)
	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts.Load())
	}

	// Should have 3 warn logs (one for each retry decision)
	if len(mockLogger.WarnLogs) != 3 {
		t.Fatalf("expected 3 warn logs, got %d", len(mockLogger.WarnLogs))
	}

	// Verify that next_delay values are correct
	// Attempt 0 fails → next_delay=100ms
	// Attempt 1 fails → next_delay=200ms
	// Attempt 2 fails → next_delay=400ms
	expectedDelays := []time.Duration{
		100 * time.Millisecond, // After attempt 0
		200 * time.Millisecond, // After attempt 1
		400 * time.Millisecond, // After attempt 2
	}

	for i, warnLog := range mockLogger.WarnLogs {
		if warnLog.Message != "request failed, will retry" {
			t.Errorf("warn log %d: expected message 'request failed, will retry', got %q",
				i, warnLog.Message)
		}

		// Extract next_delay from args
		var nextDelay time.Duration
		for j := 0; j < len(warnLog.Args); j += 2 {
			if warnLog.Args[j] == "next_delay" {
				nextDelay = warnLog.Args[j+1].(time.Duration)
				break
			}
		}

		if nextDelay != expectedDelays[i] {
			t.Errorf("warn log %d: expected next_delay=%v, got %v",
				i, expectedDelays[i], nextDelay)
		}
	}

	// Verify that Info logs have matching delay values
	// Should have 3 info logs (one for each retry execution)
	if len(mockLogger.InfoLogs) != 3 {
		t.Fatalf("expected 3 info logs, got %d", len(mockLogger.InfoLogs))
	}

	for i, infoLog := range mockLogger.InfoLogs {
		if infoLog.Message != "retrying request" {
			t.Errorf("info log %d: expected message 'retrying request', got %q",
				i, infoLog.Message)
		}

		// Extract delay from args
		var delay time.Duration
		for j := 0; j < len(infoLog.Args); j += 2 {
			if infoLog.Args[j] == "delay" {
				delay = infoLog.Args[j+1].(time.Duration)
				break
			}
		}

		// The delay in Info log should match the next_delay from previous Warn log
		if delay != expectedDelays[i] {
			t.Errorf("info log %d: expected delay=%v, got %v",
				i, expectedDelays[i], delay)
		}
	}
}

// TestClient_RetryInfoDelayAccuracy verifies that RetryInfo.Delay is correct
// and that the actual wait time matches the recorded delay
func TestClient_RetryInfoDelayAccuracy(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo
	var attemptTimes []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		count := attempts.Add(1)
		if count < 4 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(50*time.Millisecond),
		WithRetryDelayMultiple(2.0),
		WithMaxRetries(3),
		WithJitter(false), // Disable jitter for predictable timing
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

	// Should have 4 attempts (1 initial + 3 retries)
	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts.Load())
	}

	// Should have 3 retry callbacks
	if len(retryInfos) != 3 {
		t.Fatalf("expected 3 retry callbacks, got %d", len(retryInfos))
	}

	// Expected delays: 50ms, 100ms, 200ms
	expectedDelays := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
	}

	// Verify RetryInfo.Delay values
	for i, info := range retryInfos {
		if info.Delay != expectedDelays[i] {
			t.Errorf("retry %d: expected delay %v, got %v", i, expectedDelays[i], info.Delay)
		}
	}

	// Verify actual wait times match the recorded delays
	if len(attemptTimes) != 4 {
		t.Fatalf("expected 4 attempt times, got %d", len(attemptTimes))
	}

	for i := 0; i < 3; i++ {
		actualDelay := attemptTimes[i+1].Sub(attemptTimes[i])
		expectedDelay := expectedDelays[i]

		// Allow 20ms tolerance for timing variations
		tolerance := 20 * time.Millisecond
		if actualDelay < expectedDelay-tolerance || actualDelay > expectedDelay+tolerance {
			t.Errorf("retry %d: expected actual delay ~%v, got %v",
				i, expectedDelay, actualDelay)
		}
	}
}

// TestClient_RetryAfterDelayLogging verifies that when Retry-After header is present,
// it is correctly logged and used instead of the configured delay
func TestClient_RetryAfterDelayLogging(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 2 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockLogger := &MockLogger{}
	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond), // Would normally use 100ms
		WithMaxRetries(2),
		WithRespectRetryAfter(true),
		WithJitter(false), // Disable jitter for predictable values
		WithLogger(mockLogger),
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

	// Should have 1 warn log (retry decision)
	if len(mockLogger.WarnLogs) != 1 {
		t.Fatalf("expected 1 warn log, got %d", len(mockLogger.WarnLogs))
	}

	warnLog := mockLogger.WarnLogs[0]

	// Extract next_delay from args
	var nextDelay time.Duration
	for i := 0; i < len(warnLog.Args); i += 2 {
		if warnLog.Args[i] == "next_delay" {
			nextDelay = warnLog.Args[i+1].(time.Duration)
			break
		}
	}

	// Should use Retry-After value (1 second) instead of initialRetryDelay (100ms)
	expectedDelay := 1 * time.Second
	if nextDelay != expectedDelay {
		t.Errorf("expected next_delay=%v (from Retry-After), got %v", expectedDelay, nextDelay)
	}

	// Verify Info log has matching delay
	if len(mockLogger.InfoLogs) != 1 {
		t.Fatalf("expected 1 info log, got %d", len(mockLogger.InfoLogs))
	}

	infoLog := mockLogger.InfoLogs[0]
	var actualDelay time.Duration
	for i := 0; i < len(infoLog.Args); i += 2 {
		if infoLog.Args[i] == "delay" {
			actualDelay = infoLog.Args[i+1].(time.Duration)
			break
		}
	}

	if actualDelay != expectedDelay {
		t.Errorf("expected delay=%v, got %v", expectedDelay, actualDelay)
	}
}

// TestClient_TracingDelayAccuracy verifies that tracing span events contain correct delay values
func TestClient_TracingDelayAccuracy(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mockTracer := &MockTracer{}
	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithRetryDelayMultiple(2.0),
		WithMaxRetries(3),
		WithJitter(false), // Disable jitter for predictable values
		WithTracer(mockTracer),
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
		t.Fatal("expected error after exhausting retries")
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// Should have spans: 1 request span + 4 attempt spans
	if len(mockTracer.Spans) != 5 {
		t.Fatalf("expected 5 spans, got %d", len(mockTracer.Spans))
	}

	// Get the request span (first span)
	requestSpan := mockTracer.Spans[0]
	if requestSpan.Name != "http.retry.request" {
		t.Errorf("expected request span, got %s", requestSpan.Name)
	}

	// Should have 3 retry events (one for each retry decision)
	if len(requestSpan.Events) != 3 {
		t.Fatalf("expected 3 retry events, got %d", len(requestSpan.Events))
	}

	// Expected delays in milliseconds: 100ms, 200ms, 400ms
	expectedDelaysMS := []int64{100, 200, 400}

	for i, event := range requestSpan.Events {
		if event.Name != "retry" {
			t.Errorf("event %d: expected name 'retry', got %q", i, event.Name)
		}

		// Find retry.delay_ms attribute
		var delayMS int64
		found := false
		for _, attr := range event.Attributes {
			if attr.Key == "retry.delay_ms" {
				delayMS = attr.Value.(int64)
				found = true
				break
			}
		}

		if !found {
			t.Errorf("event %d: retry.delay_ms attribute not found", i)
			continue
		}

		if delayMS != expectedDelaysMS[i] {
			t.Errorf("event %d: expected retry.delay_ms=%d, got %d",
				i, expectedDelaysMS[i], delayMS)
		}
	}
}
