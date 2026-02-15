package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClient_ObservabilityFlags_DefaultConfiguration verifies the default
// observability state when no options are provided.
func TestClient_ObservabilityFlags_DefaultConfiguration(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Default: metrics and tracer are nop (disabled), logger is slogAdapter (enabled)
	if client.metricsEnabled {
		t.Error("Expected metricsEnabled=false by default (nopMetricsCollector)")
	}
	if client.tracerEnabled {
		t.Error("Expected tracerEnabled=false by default (nopTracer)")
	}
	if !client.loggerEnabled {
		t.Error("Expected loggerEnabled=true by default (slogAdapter)")
	}
}

// TestClient_ObservabilityFlags_AllDisabled verifies that all observability
// components can be explicitly disabled.
func TestClient_ObservabilityFlags_AllDisabled(t *testing.T) {
	client, err := NewClient(
		WithNoLogging(),
		WithMetrics(nil),
		WithTracer(nil),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.metricsEnabled {
		t.Error("Expected metricsEnabled=false when WithMetrics(nil)")
	}
	if client.tracerEnabled {
		t.Error("Expected tracerEnabled=false when WithTracer(nil)")
	}
	if client.loggerEnabled {
		t.Error("Expected loggerEnabled=false when WithNoLogging()")
	}
}

// TestClient_ObservabilityFlags_IndividualComponents verifies that each
// observability component is detected correctly when enabled individually.
func TestClient_ObservabilityFlags_IndividualComponents(t *testing.T) {
	tests := []struct {
		name        string
		options     []Option
		wantMetrics bool
		wantTracer  bool
		wantLogger  bool
	}{
		{
			name: "only metrics enabled",
			options: []Option{
				WithMetrics(&testMetricsCollector{}),
				WithNoLogging(),
				WithTracer(nil),
			},
			wantMetrics: true,
			wantTracer:  false,
			wantLogger:  false,
		},
		{
			name:        "only tracer enabled",
			options:     []Option{WithTracer(&testTracer{}), WithNoLogging(), WithMetrics(nil)},
			wantMetrics: false,
			wantTracer:  true,
			wantLogger:  false,
		},
		{
			name:        "only logger enabled",
			options:     []Option{WithLogger(&testLogger{}), WithMetrics(nil), WithTracer(nil)},
			wantMetrics: false,
			wantTracer:  false,
			wantLogger:  true,
		},
		{
			name: "all enabled",
			options: []Option{
				WithMetrics(&testMetricsCollector{}),
				WithTracer(&testTracer{}),
				WithLogger(&testLogger{}),
			},
			wantMetrics: true,
			wantTracer:  true,
			wantLogger:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.options...)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			if client.metricsEnabled != tt.wantMetrics {
				t.Errorf("metricsEnabled = %v, want %v", client.metricsEnabled, tt.wantMetrics)
			}
			if client.tracerEnabled != tt.wantTracer {
				t.Errorf("tracerEnabled = %v, want %v", client.tracerEnabled, tt.wantTracer)
			}
			if client.loggerEnabled != tt.wantLogger {
				t.Errorf("loggerEnabled = %v, want %v", client.loggerEnabled, tt.wantLogger)
			}
		})
	}
}

// Test helper types for observability components

type testMetricsCollector struct{}

func (t *testMetricsCollector) RecordAttempt(
	method string,
	statusCode int,
	duration time.Duration,
	err error,
) {
}
func (t *testMetricsCollector) RecordRetry(method string, reason string, attemptNumber int) {}

func (t *testMetricsCollector) RecordRequestComplete(
	method string,
	statusCode int,
	totalDuration time.Duration,
	totalAttempts int,
	success bool,
) {
}

type testTracer struct{}

func (t *testTracer) StartSpan(
	ctx context.Context,
	operationName string,
	attrs ...Attribute,
) (context.Context, Span) {
	return ctx, &testSpan{}
}

type testSpan struct{}

func (t *testSpan) End()                                     {}
func (t *testSpan) SetAttributes(attrs ...Attribute)         {}
func (t *testSpan) SetStatus(code, description string)       {}
func (t *testSpan) AddEvent(name string, attrs ...Attribute) {}

type testLogger struct{}

func (t *testLogger) Debug(msg string, args ...any) {}
func (t *testLogger) Info(msg string, args ...any)  {}
func (t *testLogger) Warn(msg string, args ...any)  {}
func (t *testLogger) Error(msg string, args ...any) {}

func TestClient_DoWithContext_TracerDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Track whether tracer was called
	tracerCalled := false
	customTracer := &testTracerWithCallback{
		onStartSpan: func() { tracerCalled = true },
	}

	// Create client with tracer disabled
	client, err := NewClient(
		WithTracer(nil), // Explicitly disable tracer
		WithNoLogging(),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Temporarily replace tracer to detect if it's called
	client.tracer = customTracer
	client.tracerEnabled = false // Force disabled flag

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.DoWithContext(context.Background(), req)
	if err != nil {
		t.Fatalf("DoWithContext() error = %v", err)
	}
	resp.Body.Close()

	// Verify tracer was NOT called
	if tracerCalled {
		t.Error("Expected tracer.StartSpan() not to be called when tracerEnabled = false")
	}
}

// Test helper
type testTracerWithCallback struct {
	onStartSpan func()
}

func (t *testTracerWithCallback) StartSpan(
	ctx context.Context,
	_ string,
	_ ...Attribute,
) (context.Context, Span) {
	if t.onStartSpan != nil {
		t.onStartSpan()
	}
	return ctx, &testSpan{}
}
