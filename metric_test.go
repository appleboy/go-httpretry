package retry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// AttemptRecord stores information about a recorded attempt
type AttemptRecord struct {
	Method     string
	StatusCode int
	Duration   time.Duration
	Err        error
}

// RetryRecord stores information about a recorded retry
type RetryRecord struct {
	Method        string
	Reason        string
	AttemptNumber int
}

// RequestCompleteRecord stores information about a completed request
type RequestCompleteRecord struct {
	Method        string
	StatusCode    int
	TotalDuration time.Duration
	TotalAttempts int
	Success       bool
}

// MockMetricsCollector implements MetricsCollector for testing
type MockMetricsCollector struct {
	Attempts         []AttemptRecord
	Retries          []RetryRecord
	RequestsComplete []RequestCompleteRecord
	mu               sync.Mutex
}

func (m *MockMetricsCollector) RecordAttempt(
	method string,
	statusCode int,
	duration time.Duration,
	err error,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Attempts = append(m.Attempts, AttemptRecord{
		Method:     method,
		StatusCode: statusCode,
		Duration:   duration,
		Err:        err,
	})
}

func (m *MockMetricsCollector) RecordRetry(method string, reason string, attemptNumber int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Retries = append(m.Retries, RetryRecord{
		Method:        method,
		Reason:        reason,
		AttemptNumber: attemptNumber,
	})
}

func (m *MockMetricsCollector) RecordRequestComplete(
	method string,
	statusCode int,
	totalDuration time.Duration,
	totalAttempts int,
	success bool,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestsComplete = append(m.RequestsComplete, RequestCompleteRecord{
		Method:        method,
		StatusCode:    statusCode,
		TotalDuration: totalDuration,
		TotalAttempts: totalAttempts,
		Success:       success,
	})
}

func TestClient_WithMetrics(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	mockMetrics := &MockMetricsCollector{}
	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
		WithMetrics(mockMetrics),
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

	// Verify metrics were recorded
	if len(mockMetrics.Attempts) != 3 {
		t.Errorf("Expected 3 attempt records, got %d", len(mockMetrics.Attempts))
	}

	if len(mockMetrics.Retries) != 2 {
		t.Errorf("Expected 2 retry records, got %d", len(mockMetrics.Retries))
	}

	if len(mockMetrics.RequestsComplete) != 1 {
		t.Errorf("Expected 1 request complete record, got %d", len(mockMetrics.RequestsComplete))
	}

	// Verify the request complete record
	complete := mockMetrics.RequestsComplete[0]
	if complete.Method != http.MethodGet {
		t.Errorf("Expected method GET, got %s", complete.Method)
	}
	if complete.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", complete.StatusCode)
	}
	if complete.TotalAttempts != 3 {
		t.Errorf("Expected 3 total attempts, got %d", complete.TotalAttempts)
	}
	if !complete.Success {
		t.Error("Expected success=true")
	}

	// Verify retry reasons
	for i, retry := range mockMetrics.Retries {
		if retry.Reason != "5xx" {
			t.Errorf("Retry %d: expected reason '5xx', got '%s'", i, retry.Reason)
		}
	}
}

func TestClient_MetricsWithNetworkError(t *testing.T) {
	mockMetrics := &MockMetricsCollector{}
	client, err := NewClient(
		WithMaxRetries(2),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
		WithMetrics(mockMetrics),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Use invalid URL to trigger network error
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://invalid-domain-that-does-not-exist.com",
		nil,
	)
	resp, err := client.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("Expected network error")
	}

	// Verify metrics recorded the error
	if len(mockMetrics.Attempts) != 3 {
		t.Errorf("Expected 3 attempt records, got %d", len(mockMetrics.Attempts))
	}

	// All attempts should have errors
	for i, attempt := range mockMetrics.Attempts {
		if attempt.Err == nil {
			t.Errorf("Attempt %d: expected error to be recorded", i)
		}
		if attempt.StatusCode != 0 {
			t.Errorf("Attempt %d: expected status code 0, got %d", i, attempt.StatusCode)
		}
	}

	// Verify retry reasons are network_error
	for i, retry := range mockMetrics.Retries {
		if retry.Reason != "network_error" {
			t.Errorf("Retry %d: expected reason 'network_error', got '%s'", i, retry.Reason)
		}
	}
}

func TestDetermineRetryReason(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		resp     *http.Response
		expected string
	}{
		{
			name:     "timeout error",
			err:      context.DeadlineExceeded,
			resp:     nil,
			expected: "timeout",
		},
		{
			name:     "canceled error",
			err:      context.Canceled,
			resp:     nil,
			expected: "canceled",
		},
		{
			name:     "network error",
			err:      errors.New("connection refused"),
			resp:     nil,
			expected: "network_error",
		},
		{
			name:     "429 rate limited",
			err:      nil,
			resp:     &http.Response{StatusCode: 429},
			expected: "rate_limited",
		},
		{
			name:     "5xx error",
			err:      nil,
			resp:     &http.Response{StatusCode: 503},
			expected: "5xx",
		},
		{
			name:     "4xx error",
			err:      nil,
			resp:     &http.Response{StatusCode: 404},
			expected: "4xx",
		},
		{
			name:     "other",
			err:      nil,
			resp:     &http.Response{StatusCode: 200},
			expected: "other",
		},
		{
			name:     "nil response",
			err:      nil,
			resp:     nil,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineRetryReason(tt.err, tt.resp)
			if result != tt.expected {
				t.Errorf("Expected reason '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
