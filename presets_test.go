package retry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRealtimeClient(t *testing.T) {
	client, err := NewRealtimeClient()
	if err != nil {
		t.Fatalf("NewRealtimeClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewRealtimeClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 2 {
		t.Errorf("maxRetries = %d, want 2", client.maxRetries)
	}
	if client.initialRetryDelay != 100*time.Millisecond {
		t.Errorf("initialRetryDelay = %v, want 100ms", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 1*time.Second {
		t.Errorf("maxRetryDelay = %v, want 1s", client.maxRetryDelay)
	}
	if client.perAttemptTimeout != 3*time.Second {
		t.Errorf("perAttemptTimeout = %v, want 3s", client.perAttemptTimeout)
	}
}

func TestNewRealtimeClient_WithOverride(t *testing.T) {
	// Test that we can override preset defaults
	client, err := NewRealtimeClient(WithMaxRetries(5))
	if err != nil {
		t.Fatalf("NewRealtimeClient() error = %v", err)
	}
	if client.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5 (overridden)", client.maxRetries)
	}
	// Other settings should remain from preset
	if client.initialRetryDelay != 100*time.Millisecond {
		t.Errorf("initialRetryDelay = %v, want 100ms", client.initialRetryDelay)
	}
}

func TestNewBackgroundClient(t *testing.T) {
	client, err := NewBackgroundClient()
	if err != nil {
		t.Fatalf("NewBackgroundClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewBackgroundClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 10 {
		t.Errorf("maxRetries = %d, want 10", client.maxRetries)
	}
	if client.initialRetryDelay != 5*time.Second {
		t.Errorf("initialRetryDelay = %v, want 5s", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 60*time.Second {
		t.Errorf("maxRetryDelay = %v, want 60s", client.maxRetryDelay)
	}
	if client.retryDelayMultiple != 3.0 {
		t.Errorf("retryDelayMultiple = %f, want 3.0", client.retryDelayMultiple)
	}
	if client.perAttemptTimeout != 30*time.Second {
		t.Errorf("perAttemptTimeout = %v, want 30s", client.perAttemptTimeout)
	}
}

func TestNewBackgroundClient_WithOverride(t *testing.T) {
	client, err := NewBackgroundClient(
		WithMaxRetries(15),
		WithInitialRetryDelay(10*time.Second),
	)
	if err != nil {
		t.Fatalf("NewBackgroundClient() error = %v", err)
	}
	if client.maxRetries != 15 {
		t.Errorf("maxRetries = %d, want 15 (overridden)", client.maxRetries)
	}
	if client.initialRetryDelay != 10*time.Second {
		t.Errorf("initialRetryDelay = %v, want 10s (overridden)", client.initialRetryDelay)
	}
}

func TestNewRateLimitedClient(t *testing.T) {
	client, err := NewRateLimitedClient()
	if err != nil {
		t.Fatalf("NewRateLimitedClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewRateLimitedClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", client.maxRetries)
	}
	if client.initialRetryDelay != 2*time.Second {
		t.Errorf("initialRetryDelay = %v, want 2s", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 30*time.Second {
		t.Errorf("maxRetryDelay = %v, want 30s", client.maxRetryDelay)
	}
	if !client.respectRetryAfter {
		t.Error("respectRetryAfter = false, want true")
	}
	if !client.jitterEnabled {
		t.Error("jitterEnabled = false, want true")
	}
}

func TestNewRateLimitedClient_RespectsRetryAfter(t *testing.T) {
	// Create a test server that returns 429 with Retry-After header
	var attemptCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewRateLimitedClient(
		WithJitter(false), // Disable jitter for predictable test
	)
	if err != nil {
		t.Fatalf("NewRateLimitedClient() error = %v", err)
	}

	ctx := context.Background()
	startTime := time.Now()
	resp, err := client.Get(ctx, server.URL)
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Should have waited approximately 1 second due to Retry-After
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 900ms (Retry-After should be respected)", elapsed)
	}
}

func TestNewMicroserviceClient(t *testing.T) {
	client, err := NewMicroserviceClient()
	if err != nil {
		t.Fatalf("NewMicroserviceClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewMicroserviceClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", client.maxRetries)
	}
	if client.initialRetryDelay != 50*time.Millisecond {
		t.Errorf("initialRetryDelay = %v, want 50ms", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 500*time.Millisecond {
		t.Errorf("maxRetryDelay = %v, want 500ms", client.maxRetryDelay)
	}
	if client.perAttemptTimeout != 2*time.Second {
		t.Errorf("perAttemptTimeout = %v, want 2s", client.perAttemptTimeout)
	}
	if !client.jitterEnabled {
		t.Error("jitterEnabled = false, want true")
	}
}

func TestNewAggressiveClient(t *testing.T) {
	client, err := NewAggressiveClient()
	if err != nil {
		t.Fatalf("NewAggressiveClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewAggressiveClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 10 {
		t.Errorf("maxRetries = %d, want 10", client.maxRetries)
	}
	if client.initialRetryDelay != 100*time.Millisecond {
		t.Errorf("initialRetryDelay = %v, want 100ms", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 5*time.Second {
		t.Errorf("maxRetryDelay = %v, want 5s", client.maxRetryDelay)
	}
}

func TestNewConservativeClient(t *testing.T) {
	client, err := NewConservativeClient()
	if err != nil {
		t.Fatalf("NewConservativeClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewConservativeClient() returned nil client")
	}

	// Verify configuration
	if client.maxRetries != 2 {
		t.Errorf("maxRetries = %d, want 2", client.maxRetries)
	}
	if client.initialRetryDelay != 5*time.Second {
		t.Errorf("initialRetryDelay = %v, want 5s", client.initialRetryDelay)
	}
}

func TestNewConservativeClient_LimitedRetries(t *testing.T) {
	// Create a test server that always fails
	var attemptCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewConservativeClient()
	if err != nil {
		t.Fatalf("NewConservativeClient() error = %v", err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, server.URL)

	// Should fail with RetryError
	var retryErr *RetryError
	if err == nil {
		resp.Body.Close()
		t.Fatal("Get() expected error, got nil")
	}
	if !errors.As(err, &retryErr) {
		t.Fatalf("Get() error type = %T, want *RetryError", err)
	}

	if resp != nil {
		defer resp.Body.Close()
	}

	// Should have made initial attempt + 2 retries = 3 total attempts
	expectedAttempts := 3
	if attemptCount != expectedAttempts {
		t.Errorf("attemptCount = %d, want %d", attemptCount, expectedAttempts)
	}
	if retryErr.Attempts != expectedAttempts {
		t.Errorf("RetryError.Attempts = %d, want %d", retryErr.Attempts, expectedAttempts)
	}
}

func TestPresets_Integration(t *testing.T) {
	tests := []struct {
		name        string
		createFunc  func(...Option) (*Client, error)
		wantRetries int
	}{
		{"Realtime", NewRealtimeClient, 2},
		{"Background", NewBackgroundClient, 10},
		{"RateLimited", NewRateLimitedClient, 5},
		{"Microservice", NewMicroserviceClient, 3},
		{"Aggressive", NewAggressiveClient, 10},
		{"Conservative", NewConservativeClient, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server that succeeds on the second attempt
			var attemptCount int
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attemptCount++
					if attemptCount == 1 {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusOK)
				}),
			)
			defer server.Close()

			client, err := tt.createFunc()
			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}

			ctx := context.Background()
			resp, err := client.Get(ctx, server.URL)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			// Should have retried once after initial failure
			if attemptCount != 2 {
				t.Errorf("attemptCount = %d, want 2 (initial + 1 retry)", attemptCount)
			}

			// Verify maxRetries configuration
			if client.maxRetries != tt.wantRetries {
				t.Errorf("maxRetries = %d, want %d", client.maxRetries, tt.wantRetries)
			}
		})
	}
}
