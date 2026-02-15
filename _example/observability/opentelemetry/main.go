package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

// OTelTracer implements retry.Tracer for OpenTelemetry
// This is a simplified example showing the interface implementation.
// In production, you would use go.opentelemetry.io/otel
type OTelTracer struct {
	serviceName string
}

func NewOTelTracer(serviceName string) *OTelTracer {
	return &OTelTracer{serviceName: serviceName}
}

func (t *OTelTracer) StartSpan(ctx context.Context, operationName string, attrs ...retry.Attribute) (context.Context, retry.Span) {
	// In production:
	// tracer := otel.Tracer(t.serviceName)
	// ctx, span := tracer.Start(ctx, operationName)
	// for _, attr := range attrs {
	//     span.SetAttributes(attribute.String(attr.Key, fmt.Sprint(attr.Value)))
	// }
	// return ctx, &OTelSpan{span: span}

	span := &OTelSpan{
		name:      operationName,
		startTime: time.Now(),
	}

	fmt.Printf("[OpenTelemetry] Starting span: %s\n", operationName)
	for _, attr := range attrs {
		fmt.Printf("  - %s = %v\n", attr.Key, attr.Value)
	}

	return ctx, span
}

// OTelSpan wraps an OpenTelemetry span
type OTelSpan struct {
	name      string
	startTime time.Time
	status    string
	desc      string
}

func (s *OTelSpan) End() {
	duration := time.Since(s.startTime)
	// In production: s.span.End()
	fmt.Printf("[OpenTelemetry] Ending span: %s (duration: %v, status: %s)\n", s.name, duration, s.status)
}

func (s *OTelSpan) SetAttributes(attrs ...retry.Attribute) {
	// In production:
	// for _, attr := range attrs {
	//     s.span.SetAttributes(attribute.String(attr.Key, fmt.Sprint(attr.Value)))
	// }

	for _, attr := range attrs {
		fmt.Printf("[OpenTelemetry] Setting attribute on %s: %s = %v\n", s.name, attr.Key, attr.Value)
	}
}

func (s *OTelSpan) SetStatus(code string, description string) {
	// In production:
	// if code == "error" {
	//     s.span.SetStatus(codes.Error, description)
	// } else {
	//     s.span.SetStatus(codes.Ok, description)
	// }

	s.status = code
	s.desc = description
	fmt.Printf("[OpenTelemetry] Setting status on %s: %s (%s)\n", s.name, code, description)
}

func (s *OTelSpan) AddEvent(name string, attrs ...retry.Attribute) {
	// In production:
	// eventAttrs := make([]attribute.KeyValue, len(attrs))
	// for i, attr := range attrs {
	//     eventAttrs[i] = attribute.String(attr.Key, fmt.Sprint(attr.Value))
	// }
	// s.span.AddEvent(name, trace.WithAttributes(eventAttrs...))

	fmt.Printf("[OpenTelemetry] Adding event to %s: %s\n", s.name, name)
	for _, attr := range attrs {
		fmt.Printf("  - %s = %v\n", attr.Key, attr.Value)
	}
}

func main() {
	// In production, you would initialize OpenTelemetry:
	// import (
	//     "go.opentelemetry.io/otel"
	//     "go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	//     "go.opentelemetry.io/otel/sdk/trace"
	// )
	//
	// exporter, _ := otlptrace.New(ctx, otlptrace.WithInsecure())
	// tp := trace.NewTracerProvider(
	//     trace.WithBatcher(exporter),
	//     trace.WithResource(resource.NewWithAttributes(
	//         semconv.SchemaURL,
	//         semconv.ServiceName("my-service"),
	//     )),
	// )
	// otel.SetTracerProvider(tp)

	// Create OpenTelemetry tracer
	otelTracer := NewOTelTracer("http-retry-service")

	// Create test server that fails twice then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create retry client with OpenTelemetry tracing
	client, err := retry.NewClient(
		retry.WithMaxRetries(5),
		retry.WithInitialRetryDelay(50*time.Millisecond),
		retry.WithJitter(false),
		retry.WithTracer(otelTracer),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("=== Making HTTP request with OpenTelemetry tracing ===\n")

	// Make request
	ctx := context.Background()
	resp, err := client.Get(ctx, server.URL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Printf("\n=== Request completed: status=%d, attempts=%d ===\n", resp.StatusCode, attempts)

	fmt.Println("\n// In production, these traces would be:")
	fmt.Println("// - Exported to Jaeger, Zipkin, or other tracing backends")
	fmt.Println("// - Visible in distributed tracing UI (e.g., Jaeger UI)")
	fmt.Println("// - Showing the complete request flow with timing and dependencies")
	fmt.Println("//")
	fmt.Println("// Example trace structure:")
	fmt.Println("// └─ http.retry.request (parent span)")
	fmt.Println("//    ├─ http.retry.attempt (attempt 1) - failed")
	fmt.Println("//    ├─ http.retry.attempt (attempt 2) - failed")
	fmt.Println("//    └─ http.retry.attempt (attempt 3) - success")
}
