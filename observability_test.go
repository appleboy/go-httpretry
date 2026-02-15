package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_WithAllObservability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockMetrics := &MockMetricsCollector{}
	mockTracer := &MockTracer{}
	mockLogger := &MockLogger{}

	client, err := NewClient(
		WithMaxRetries(3),
		WithMetrics(mockMetrics),
		WithTracer(mockTracer),
		WithLogger(mockLogger),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Verify all observability systems recorded data
	if len(mockMetrics.Attempts) == 0 {
		t.Error("Expected metrics attempts to be recorded")
	}
	if len(mockTracer.Spans) == 0 {
		t.Error("Expected tracer spans to be recorded")
	}
	if len(mockLogger.DebugLogs) == 0 {
		t.Error("Expected logger debug logs to be recorded")
	}
}

func TestClient_ObservabilityWithFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mockMetrics := &MockMetricsCollector{}
	mockTracer := &MockTracer{}
	mockLogger := &MockLogger{}

	client, err := NewClient(
		WithMaxRetries(2),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
		WithMetrics(mockMetrics),
		WithTracer(mockTracer),
		WithLogger(mockLogger),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err == nil {
		t.Fatal("Expected error after max retries")
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Verify metrics recorded failure
	if len(mockMetrics.RequestsComplete) != 1 {
		t.Errorf("Expected 1 request complete record, got %d", len(mockMetrics.RequestsComplete))
	}
	complete := mockMetrics.RequestsComplete[0]
	if complete.Success {
		t.Error("Expected success=false for failed request")
	}
	if complete.TotalAttempts != 3 {
		t.Errorf("Expected 3 total attempts, got %d", complete.TotalAttempts)
	}

	// Verify tracer recorded error status
	requestSpan := mockTracer.Spans[0]
	if requestSpan.Status != "error" {
		t.Errorf("Expected request span status 'error', got '%s'", requestSpan.Status)
	}

	// Verify logger recorded error
	if len(mockLogger.ErrorLogs) == 0 {
		t.Error("Expected error log to be recorded")
	}
	if mockLogger.ErrorLogs[0].Message != "request failed after all retries" {
		t.Errorf(
			"Expected error log 'request failed after all retries', got '%s'",
			mockLogger.ErrorLogs[0].Message,
		)
	}
}

func TestClient_ObservabilityNoOverheadWhenDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client without observability (default no-op implementations)
	client, err := NewClient(WithMaxRetries(0))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Just verify it works without panicking (no-op implementations should work)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestClient_WithMetricsNilDisablesMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// First set a mock collector, then disable it with nil
	mockMetrics := &MockMetricsCollector{}
	client, err := NewClient(
		WithMetrics(mockMetrics),
		WithMetrics(nil), // This should disable metrics
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Verify mock metrics were NOT called (disabled by nil)
	if len(mockMetrics.Attempts) != 0 {
		t.Error("Expected no metrics attempts to be recorded after passing nil")
	}
	if len(mockMetrics.RequestsComplete) != 0 {
		t.Error("Expected no metrics requests complete to be recorded after passing nil")
	}
}

func TestClient_WithTracerNilDisablesTracing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// First set a mock tracer, then disable it with nil
	mockTracer := &MockTracer{}
	client, err := NewClient(
		WithTracer(mockTracer),
		WithTracer(nil), // This should disable tracing
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Verify mock tracer was NOT called (disabled by nil)
	if len(mockTracer.Spans) != 0 {
		t.Error("Expected no tracer spans to be recorded after passing nil")
	}
}

func TestClient_WithLoggerNilDisablesLogging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// First set a mock logger, then disable it with nil
	mockLogger := &MockLogger{}
	client, err := NewClient(
		WithLogger(mockLogger),
		WithLogger(nil), // This should disable logging
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Verify mock logger was NOT called (disabled by nil)
	if len(mockLogger.DebugLogs) != 0 {
		t.Error("Expected no debug logs to be recorded after passing nil")
	}
}
