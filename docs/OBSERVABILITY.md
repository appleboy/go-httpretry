# Observability Guide

This guide covers the observability features in `go-httpretry`, including metrics collection, distributed tracing, and structured logging.

## Table of Contents

- [Overview](#overview)
- [Design Philosophy](#design-philosophy)
- [Metrics Collection](#metrics-collection)
- [Distributed Tracing](#distributed-tracing)
- [Structured Logging](#structured-logging)
- [Integration Examples](#integration-examples)
- [Performance Considerations](#performance-considerations)
- [Best Practices](#best-practices)

## Overview

The library provides three observability interfaces:

- **MetricsCollector**: Records quantitative data about requests, retries, and performance
- **Tracer**: Creates distributed tracing spans for request flows
- **Logger**: Outputs structured logs for debugging and monitoring

All observability features are:
- **Optional**: Disabled by default (no-op implementations)
- **Zero-dependency**: Interfaces only, you provide implementations
- **Zero-overhead when disabled**: Uses Null Object Pattern for negligible performance impact
- **Thread-safe**: All callbacks must be thread-safe

## Design Philosophy

### Interface-Driven Architecture

The library defines interfaces but doesn't depend on any observability frameworks. This means:

1. **You control the implementation**: Use Prometheus, OpenTelemetry, Datadog, or any tool you prefer
2. **Zero dependencies**: The library remains dependency-free
3. **Maximum flexibility**: Integrate with existing monitoring infrastructure

### Null Object Pattern

When observability is not enabled, the library uses no-op implementations that:
- Require no nil checks in the codebase
- Can be inlined by the compiler
- Have negligible performance impact

## Metrics Collection

### Interface Definition

```go
type MetricsCollector interface {
    // RecordAttempt records a single request attempt
    RecordAttempt(method string, statusCode int, duration time.Duration, err error)

    // RecordRetry records a retry event
    RecordRetry(method string, reason string, attemptNumber int)

    // RecordRequestComplete records request completion (including all retries)
    RecordRequestComplete(method string, statusCode int, totalDuration time.Duration, totalAttempts int, success bool)
}
```

### Usage

```go
// Implement the interface with your metrics library
type MyMetricsCollector struct {
    attemptsCounter    prometheus.Counter
    retriesCounter     prometheus.Counter
    requestDuration    prometheus.Histogram
    // ... your metrics
}

func (m *MyMetricsCollector) RecordAttempt(method string, statusCode int, duration time.Duration, err error) {
    m.attemptsCounter.WithLabelValues(method, fmt.Sprint(statusCode)).Inc()
    // ... record other metrics
}

// ... implement other methods

// Use with retry client
client, _ := retry.NewClient(
    retry.WithMetrics(myMetricsCollector),
)
```

### Retry Reasons

The `RecordRetry` method includes a `reason` parameter that categorizes why the retry occurred:

- `"timeout"`: Request exceeded deadline
- `"canceled"`: Context was canceled
- `"network_error"`: Network/connection error
- `"rate_limited"`: HTTP 429 Too Many Requests
- `"5xx"`: Server error (500-599)
- `"4xx"`: Client error (400-499)
- `"other"`: Other retryable condition
- `"unknown"`: Unable to determine reason

### Example Metrics

A typical implementation might expose:

```prometheus
# HELP http_retry_attempts_total Total number of HTTP attempts
# TYPE http_retry_attempts_total counter
http_retry_attempts_total{method="GET",status="200"} 1
http_retry_attempts_total{method="GET",status="500"} 2

# HELP http_retry_retries_total Total number of retries by reason
# TYPE http_retry_retries_total counter
http_retry_retries_total{method="GET",reason="5xx"} 2

# HELP http_retry_requests_total Total number of completed requests
# TYPE http_retry_requests_total counter
http_retry_requests_total{method="GET",success="true"} 1

# HELP http_retry_request_duration_seconds Request duration including retries
# TYPE http_retry_request_duration_seconds histogram
http_retry_request_duration_seconds_bucket{method="GET",le="0.1"} 0
http_retry_request_duration_seconds_bucket{method="GET",le="0.5"} 1
```

## Distributed Tracing

### Interface Definition

```go
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
```

### Usage

```go
// Implement with your tracing library (e.g., OpenTelemetry)
type OTelTracer struct {
    tracer trace.Tracer
}

func (t *OTelTracer) StartSpan(ctx context.Context, operationName string, attrs ...retry.Attribute) (context.Context, retry.Span) {
    ctx, span := t.tracer.Start(ctx, operationName)
    for _, attr := range attrs {
        span.SetAttributes(attribute.String(attr.Key, fmt.Sprint(attr.Value)))
    }
    return ctx, &OTelSpan{span: span}
}

// Use with retry client
client, _ := retry.NewClient(
    retry.WithTracer(otelTracer),
)
```

### Span Hierarchy

The tracer creates a hierarchical structure:

```
└─ http.retry.request (outer span for entire retry operation)
   ├─ http.retry.attempt (attempt 1)
   ├─ http.retry.attempt (attempt 2)
   └─ http.retry.attempt (attempt 3)
```

### Span Attributes

**Request span attributes:**
- `http.method`: HTTP method (GET, POST, etc.)
- `http.url`: Request URL
- `retry.max_attempts`: Maximum attempts configured
- `retry.exhausted`: true if all retries exhausted (on error)

**Attempt span attributes:**
- `retry.attempt`: Attempt number (1-indexed)
- `http.method`: HTTP method
- `http.status_code`: Response status code (if available)

**Retry events:**
- Event name: `"retry"`
- `retry.attempt`: Attempt number
- `retry.reason`: Reason for retry (same as metrics)
- `retry.delay_ms`: Delay before next retry in milliseconds

### Status Codes

Spans use status codes compatible with OpenTelemetry:
- `"ok"`: Success
- `"error"`: Failure (with description)

## Structured Logging

### Interface Definition

```go
// Logger defines the structured logging interface (slog-compatible)
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

### Built-in slog Adapter

For convenience, the library provides an adapter for Go's standard `log/slog`:

```go
import "log/slog"

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client, _ := retry.NewClient(
    retry.WithLogger(retry.NewSlogAdapter(logger)),
)
```

### Log Levels

- **Debug**: Request start, request completion (success)
- **Info**: Retry attempts (before delay)
- **Warn**: Retry decisions (with reason)
- **Error**: Request failure after all retries exhausted

### Log Fields

Common fields included in logs:
- `method`: HTTP method
- `url`: Request URL (in start log)
- `max_retries`: Maximum retries configured (in start log)
- `attempt`: Current attempt number (1-indexed)
- `attempts`: Total attempts made (in completion log)
- `reason`: Retry reason category
- `delay`: Delay duration before retry
- `next_delay`: Next retry delay (in warn logs)
- `duration`: Total request duration (in completion log)
- `final_status`: Final HTTP status code (in error log)

### Example Log Output (JSON)

```json
{"time":"2024-02-14T10:00:00Z","level":"DEBUG","msg":"starting request","method":"GET","url":"https://api.example.com/data","max_retries":3}
{"time":"2024-02-14T10:00:00.150Z","level":"WARN","msg":"request failed, will retry","method":"GET","attempt":1,"reason":"5xx","next_delay":"1s"}
{"time":"2024-02-14T10:00:00.151Z","level":"INFO","msg":"retrying request","method":"GET","attempt":2,"delay":"1s"}
{"time":"2024-02-14T10:00:01.200Z","level":"DEBUG","msg":"request completed","method":"GET","attempts":2,"duration":"1.2s"}
```

## Integration Examples

### Example 1: Prometheus Metrics

See `_example/observability/prometheus/main.go` for a complete implementation showing:
- Counter metrics for attempts and retries
- Histogram metrics for durations
- Labels for method, status, and reason

### Example 2: OpenTelemetry Tracing

See `_example/observability/opentelemetry/main.go` for a complete implementation showing:
- Span creation and nesting
- Attribute propagation
- Event recording for retries

### Example 3: Standard Library Logging

See `_example/observability/slog/main.go` for using Go's built-in structured logging.

### Example 4: Combined Observability

See `_example/observability/combined/main.go` for using all three features together.

## Performance Considerations

### When Disabled (Default)

- **Zero overhead**: No-op implementations use empty inline functions
- **No allocations**: Null objects are package-level singletons
- **No branches**: No `if != nil` checks in hot path

### When Enabled

Performance impact depends on your implementation:

- **MetricsCollector**: Typically low overhead (~microseconds per call)
  - Use atomic operations or mutexes appropriately
  - Consider batching or sampling for high-volume applications

- **Tracer**: Overhead varies by implementation
  - OpenTelemetry: Moderate overhead (~tens of microseconds)
  - Consider sampling for high-traffic services
  - Use context propagation efficiently

- **Logger**: Impact depends on log level and destination
  - Structured logging (slog): Low overhead for suppressed levels
  - Network logging: Add buffering/batching
  - File logging: Use async writers

### Best Practices

1. **Start with logging**: Enable debug logging during development, info+ in production
2. **Add metrics next**: Always monitor production services with metrics
3. **Add tracing selectively**: Use sampling for high-volume endpoints
4. **Benchmark your workload**: Measure impact in your specific use case
5. **Thread safety**: Ensure all implementations are thread-safe

## Best Practices

### Metrics

✅ **Do:**
- Use counters for totals (attempts, retries, requests)
- Use histograms for durations and distributions
- Add labels for method, status, outcome
- Track both success and failure rates

❌ **Don't:**
- Create unbounded label values (e.g., full URLs)
- Record PII in labels
- Perform expensive computations in callbacks

### Tracing

✅ **Do:**
- Propagate context through your application
- Use sampling in high-volume scenarios
- Include relevant attributes for debugging
- End spans in defer statements

❌ **Don't:**
- Create spans without ending them (memory leak)
- Add high-cardinality attributes
- Block in span operations

### Logging

✅ **Do:**
- Use appropriate log levels
- Include structured fields for filtering
- Log at Info level for retries (actionable events)
- Log at Error level for final failures

❌ **Don't:**
- Log sensitive data (tokens, passwords)
- Use Debug level in production for high-volume endpoints
- Perform expensive formatting in log calls

## Compatibility with Existing Observability

### Works With

- **OnRetryFunc**: The legacy callback remains available and works alongside new observability
- **Existing metrics**: You can combine with HTTP client middleware
- **Custom implementations**: Any logging/tracing/metrics library

### Migration from OnRetryFunc

The old `OnRetryFunc` callback is still supported:

```go
// Old way (still works)
client, _ := retry.NewClient(
    retry.WithOnRetry(func(info retry.RetryInfo) {
        log.Printf("Retry attempt %d after %v", info.Attempt, info.Delay)
    }),
)

// New way (recommended)
client, _ := retry.NewClient(
    retry.WithLogger(retry.NewSlogAdapter(logger)),
    retry.WithMetrics(metricsCollector),
)
```

Both can be used together if needed during migration.

## Reference Implementations

The `_example/observability/` directory contains:
- `slog/`: Using Go's standard library
- `prometheus/`: Metrics collection pattern
- `opentelemetry/`: Distributed tracing pattern
- `combined/`: All three together

These examples show the interface patterns but don't add dependencies to your project.
