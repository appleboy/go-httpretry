package retry

import "context"

// Attribute represents a key-value pair attribute
type Attribute struct {
	Key   string
	Value any
}

// Span represents a tracing span (OpenTelemetry-compatible)
type Span interface {
	End()
	SetAttributes(attrs ...Attribute)
	SetStatus(code string, description string)
	AddEvent(name string, attrs ...Attribute)
}

// Tracer defines the distributed tracing interface
type Tracer interface {
	StartSpan(ctx context.Context, operationName string, attrs ...Attribute) (context.Context, Span)
}

// nopTracer provides no-op implementation
type nopTracer struct{}

func (nopTracer) StartSpan(ctx context.Context, _ string, _ ...Attribute) (context.Context, Span) {
	return ctx, nopSpan{}
}

// nopSpan provides no-op implementation
type nopSpan struct{}

func (nopSpan) End()                          {}
func (nopSpan) SetAttributes(...Attribute)    {}
func (nopSpan) SetStatus(string, string)      {}
func (nopSpan) AddEvent(string, ...Attribute) {}

// defaultTracer is the package-level singleton (internal use, not exported)
var defaultTracer = nopTracer{}
