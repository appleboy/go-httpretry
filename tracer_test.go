package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// MockSpan implements Span for testing
type MockSpan struct {
	Name        string
	Attributes  []Attribute
	Status      string
	Description string
	Events      []MockEvent
	Ended       bool
	mu          sync.Mutex
}

// MockEvent stores information about a span event
type MockEvent struct {
	Name       string
	Attributes []Attribute
}

func (s *MockSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Ended = true
}

func (s *MockSpan) SetAttributes(attrs ...Attribute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attributes = append(s.Attributes, attrs...)
}

func (s *MockSpan) SetStatus(code string, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = code
	s.Description = description
}

func (s *MockSpan) AddEvent(name string, attrs ...Attribute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, MockEvent{
		Name:       name,
		Attributes: attrs,
	})
}

// MockTracer implements Tracer for testing
type MockTracer struct {
	Spans []*MockSpan
	mu    sync.Mutex
}

func (t *MockTracer) StartSpan(ctx context.Context, operationName string, attrs ...Attribute) (context.Context, Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := &MockSpan{
		Name:       operationName,
		Attributes: attrs,
	}
	t.Spans = append(t.Spans, span)
	return ctx, span
}

func TestClient_WithTracer(t *testing.T) {
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

	mockTracer := &MockTracer{}
	client, err := NewClient(
		WithMaxRetries(3),
		WithInitialRetryDelay(10*time.Millisecond),
		WithJitter(false),
		WithTracer(mockTracer),
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

	// Verify spans were created
	// Expected: 1 request span + 2 attempt spans
	if len(mockTracer.Spans) != 3 {
		t.Errorf("Expected 3 spans, got %d", len(mockTracer.Spans))
	}

	// Verify request span
	requestSpan := mockTracer.Spans[0]
	if requestSpan.Name != "http.retry.request" {
		t.Errorf("Expected request span name 'http.retry.request', got '%s'", requestSpan.Name)
	}
	if !requestSpan.Ended {
		t.Error("Request span should be ended")
	}
	if requestSpan.Status != "ok" {
		t.Errorf("Expected request span status 'ok', got '%s'", requestSpan.Status)
	}

	// Verify attempt spans
	for i := 1; i < len(mockTracer.Spans); i++ {
		span := mockTracer.Spans[i]
		if span.Name != "http.retry.attempt" {
			t.Errorf("Span %d: expected name 'http.retry.attempt', got '%s'", i, span.Name)
		}
		if !span.Ended {
			t.Errorf("Span %d should be ended", i)
		}
	}

	// Verify retry events on request span
	if len(requestSpan.Events) != 1 {
		t.Errorf("Expected 1 retry event, got %d", len(requestSpan.Events))
	}
}
