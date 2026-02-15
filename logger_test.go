package retry

import (
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

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
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
