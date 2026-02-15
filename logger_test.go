package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// LogRecord stores information about a log entry
type LogRecord struct {
	Level   string
	Message string
	Args    []any
}

// MockLogger implements Logger for testing
type MockLogger struct {
	DebugLogs []LogRecord
	InfoLogs  []LogRecord
	WarnLogs  []LogRecord
	ErrorLogs []LogRecord
	mu        sync.Mutex
}

func (l *MockLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.DebugLogs = append(l.DebugLogs, LogRecord{Level: "debug", Message: msg, Args: args})
}

func (l *MockLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.InfoLogs = append(l.InfoLogs, LogRecord{Level: "info", Message: msg, Args: args})
}

func (l *MockLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.WarnLogs = append(l.WarnLogs, LogRecord{Level: "warn", Message: msg, Args: args})
}

func (l *MockLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ErrorLogs = append(l.ErrorLogs, LogRecord{Level: "error", Message: msg, Args: args})
}

func TestClient_WithLogger(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	mockLogger := &MockLogger{}
	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
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

	// Verify logs were recorded
	if len(mockLogger.DebugLogs) < 2 {
		t.Errorf(
			"Expected at least 2 debug logs (start + complete), got %d",
			len(mockLogger.DebugLogs),
		)
	}

	if len(mockLogger.InfoLogs) != 1 {
		t.Errorf("Expected 1 info log (retry), got %d", len(mockLogger.InfoLogs))
	}

	if len(mockLogger.WarnLogs) != 1 {
		t.Errorf("Expected 1 warn log (retry warning), got %d", len(mockLogger.WarnLogs))
	}

	// Verify log messages
	if mockLogger.DebugLogs[0].Message != "starting request" {
		t.Errorf(
			"Expected first debug log 'starting request', got '%s'",
			mockLogger.DebugLogs[0].Message,
		)
	}
}

func TestClient_DefaultLogger(t *testing.T) {
	// This test verifies that the default logger is slog, not nopLogger
	// We can't easily capture slog output in tests, but we can verify the client is created
	// successfully and uses the default logger (which is slogAdapter wrapping slog.Default())

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Create client without WithLogger option - should use default slog
	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Verify the default logger is slogAdapter (not nopLogger)
	if _, ok := client.logger.(*slogAdapter); !ok {
		t.Errorf("Expected default logger to be *slogAdapter, got %T", client.logger)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify retry happened
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestClient_WithNoLogging(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Create client with WithNoLogging() - should disable all logging
	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
		WithNoLogging(),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Verify the logger is nopLogger
	if _, ok := client.logger.(nopLogger); !ok {
		t.Errorf("Expected logger to be nopLogger with WithNoLogging(), got %T", client.logger)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify retry happened (even though logging is disabled)
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}
