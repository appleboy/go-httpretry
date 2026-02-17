# Middleware

The go-httpretry library supports a two-level middleware system that allows you to inject custom behavior at different stages of the request lifecycle.

## Table of Contents

- [Overview](#overview)
- [Per-Attempt Middleware](#per-attempt-middleware)
  - [Execution Model](#execution-model)
  - [Use Cases](#use-cases)
  - [Creating Custom Per-Attempt Middleware](#creating-custom-per-attempt-middleware)
  - [Built-in Per-Attempt Middleware](#built-in-per-attempt-middleware)
- [Request-Level Middleware](#request-level-middleware)
  - [Execution Model](#execution-model-1)
  - [Use Cases](#use-cases-1)
  - [Creating Custom Request-Level Middleware](#creating-custom-request-level-middleware)
  - [Built-in Request-Level Middleware](#built-in-request-level-middleware)
- [Middleware Ordering](#middleware-ordering)
- [Complete Examples](#complete-examples)
- [Best Practices](#best-practices)

## Overview

The middleware system provides two levels of interception:

| Level | Type | Execution Frequency | Wraps | Use Cases |
|-------|------|-------------------|-------|-----------|
| **Per-Attempt** | `Middleware` | Every HTTP attempt (including retries) | `http.RoundTripper` | Logging each attempt, adding headers, per-attempt tracing |
| **Request-Level** | `RequestMiddleware` | Once per client call | Entire retry operation | Rate limiting, circuit breaking, request-level tracing |

**Example**: If a request requires 3 attempts (initial + 2 retries):
- **Per-Attempt middleware** executes 3 times (once for each HTTP call)
- **Request-Level middleware** executes 1 time (wrapping all 3 attempts)

## Per-Attempt Middleware

Per-attempt middleware wraps the `http.RoundTripper` interface and executes for **every HTTP attempt**, including retries.

### Execution Model

```
Request → [Middleware 1] → [Middleware 2] → Transport → HTTP Call
          ↓ Executes for each retry attempt ↓
```

Per-attempt middleware uses the standard Go `http.RoundTripper` interface, making it familiar to Go developers and compatible with existing HTTP middleware patterns.

### Use Cases

- **Per-attempt logging**: Log each individual HTTP attempt with timing information
- **Header injection**: Add or modify headers for each attempt (e.g., request IDs, timestamps)
- **Fine-grained tracing**: Create spans for each individual HTTP attempt
- **Request/response inspection**: Examine or modify requests and responses at the HTTP level
- **Attempt-specific metrics**: Collect metrics for each HTTP call

### Creating Custom Per-Attempt Middleware

Per-attempt middleware is a function that wraps an `http.RoundTripper`:

```go
type Middleware func(http.RoundTripper) http.RoundTripper
```

**Basic Example - Logging Middleware:**

```go
func loggingMiddleware(next http.RoundTripper) http.RoundTripper {
    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        start := time.Now()
        log.Printf("Starting attempt: %s %s", req.Method, req.URL)

        resp, err := next.RoundTrip(req)
        duration := time.Since(start)

        if err != nil {
            log.Printf("Attempt failed after %v: %v", duration, err)
        } else {
            log.Printf("Attempt completed in %v with status %d", duration, resp.StatusCode)
        }

        return resp, err
    })
}

// Use it
client, _ := retry.NewClient(
    retry.WithPerAttemptMiddleware(loggingMiddleware),
)
```

**Modifying Requests - IMPORTANT:**

If your middleware needs to modify the request, **you must clone it first** to ensure concurrent safety:

```go
func headerMiddleware(headers map[string]string) retry.Middleware {
    return func(next http.RoundTripper) http.RoundTripper {
        return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
            // Clone request before modification (concurrent safety)
            req = req.Clone(req.Context())

            for key, value := range headers {
                req.Header.Set(key, value)
            }

            return next.RoundTrip(req)
        })
    }
}
```

**Helper Type - `RoundTripperFunc`:**

The library provides `RoundTripperFunc` as a convenience type for creating functional middleware without defining new types:

```go
type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
    return f(req)
}
```

### Built-in Per-Attempt Middleware

#### LoggingMiddleware

Logs each HTTP attempt with timing information:

```go
client, _ := retry.NewClient(
    retry.WithPerAttemptMiddleware(
        retry.LoggingMiddleware(myLogger),
    ),
)
```

**Output Example:**
```
[DEBUG] http attempt starting method=GET url=https://api.example.com/data
[DEBUG] http attempt completed method=GET url=https://api.example.com/data status=200 duration_ms=45
```

#### HeaderMiddleware

Adds headers to each HTTP attempt:

```go
client, _ := retry.NewClient(
    retry.WithPerAttemptMiddleware(
        retry.HeaderMiddleware(map[string]string{
            "X-Client-Version": "1.0.0",
            "User-Agent":       "my-client/1.0",
        }),
    ),
)
```

## Request-Level Middleware

Request-level middleware wraps the entire retry operation and executes **once per client call**, regardless of how many retry attempts are made.

### Execution Model

```
Client Call → [Middleware 1] → [Middleware 2] → Retry Loop (all attempts)
             ↓ Executes once per client call ↓
```

Request-level middleware wraps the `RetryFunc`, which represents the entire retry operation.

### Use Cases

- **Rate limiting**: Limit the number of requests before they enter the retry loop
- **Circuit breaking**: Prevent requests when the service is unhealthy
- **Request-level tracing**: Create a single span for the entire retry operation
- **Caching**: Cache responses to avoid redundant requests
- **Request-level metrics**: Collect metrics for the entire request lifecycle

### Creating Custom Request-Level Middleware

Request-level middleware is a function that wraps a `RetryFunc`:

```go
type RequestMiddleware func(RetryFunc) RetryFunc
type RetryFunc func(context.Context, *http.Request) (*http.Response, error)
```

**Example - Rate Limiting Middleware:**

```go
func rateLimitMiddleware(limiter RateLimiter) retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            // Wait for rate limiter before executing retry loop
            if err := limiter.Wait(ctx); err != nil {
                return nil, fmt.Errorf("rate limit: %w", err)
            }

            // Execute retry loop
            return next(ctx, req)
        }
    }
}

// Use it
client, _ := retry.NewClient(
    retry.WithRequestMiddleware(
        rateLimitMiddleware(myRateLimiter),
    ),
)
```

**Example - Circuit Breaker Middleware:**

```go
func circuitBreakerMiddleware(cb CircuitBreaker) retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            // Check if circuit breaker allows the request
            if err := cb.Allow(); err != nil {
                return nil, fmt.Errorf("circuit breaker: %w", err)
            }

            // Execute retry loop
            resp, err := next(ctx, req)

            // Record result in circuit breaker
            if err != nil || (resp != nil && resp.StatusCode >= 500) {
                cb.RecordFailure()
            } else {
                cb.RecordSuccess()
            }

            return resp, err
        }
    }
}
```

**Example - Timing Middleware:**

```go
func timingMiddleware(operationName string) retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            start := time.Now()
            log.Printf("[%s] Starting retry operation", operationName)

            resp, err := next(ctx, req)
            duration := time.Since(start)

            if err != nil {
                log.Printf("[%s] Failed after %v: %v", operationName, duration, err)
            } else {
                log.Printf("[%s] Completed in %v with status %d",
                    operationName, duration, resp.StatusCode)
            }

            return resp, err
        }
    }
}
```

### Built-in Request-Level Middleware

#### RateLimitMiddleware

Applies rate limiting before the retry loop begins:

```go
// RateLimiter interface
type RateLimiter interface {
    Wait(ctx context.Context) error
}

// Usage
client, _ := retry.NewClient(
    retry.WithRequestMiddleware(
        retry.RateLimitMiddleware(myRateLimiter),
    ),
)
```

#### CircuitBreakerMiddleware

Implements circuit breaker pattern to prevent cascading failures:

```go
// CircuitBreaker interface
type CircuitBreaker interface {
    Allow() error
    RecordSuccess()
    RecordFailure()
}

// Usage
client, _ := retry.NewClient(
    retry.WithRequestMiddleware(
        retry.CircuitBreakerMiddleware(myCircuitBreaker),
    ),
)
```

#### TracingRequestMiddleware

Adds request-level distributed tracing spans:

```go
client, _ := retry.NewClient(
    retry.WithRequestMiddleware(
        retry.TracingRequestMiddleware(myTracer),
    ),
)
```

## Middleware Ordering

Middleware is applied in the order it's added, with **the first middleware being the outermost**:

```go
client, _ := retry.NewClient(
    retry.WithPerAttemptMiddleware(
        middleware1,  // Executes first (outermost)
        middleware2,  // Executes second
        middleware3,  // Executes third (innermost)
    ),
)
```

**Execution Flow:**

```
Request → middleware1 → middleware2 → middleware3 → Transport → HTTP
                ↓                              ↓
Response ← middleware1 ← middleware2 ← middleware3 ← Transport ← HTTP
```

**Multiple Calls to `With*Middleware`:**

You can call the option functions multiple times, and middleware will be appended:

```go
client, _ := retry.NewClient(
    retry.WithPerAttemptMiddleware(middleware1),
    retry.WithPerAttemptMiddleware(middleware2),  // Appended
    retry.WithRequestMiddleware(requestMiddleware1),
    retry.WithRequestMiddleware(requestMiddleware2),  // Appended
)
```

## Complete Examples

### Example 1: Combined Per-Attempt and Request-Level Middleware

```go
// Per-attempt logging middleware
func attemptLogger(next http.RoundTripper) http.RoundTripper {
    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        log.Printf("[ATTEMPT] %s %s", req.Method, req.URL.Path)
        return next.RoundTrip(req)
    })
}

// Request-level timing middleware
func requestTimer(operationName string) retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            start := time.Now()
            log.Printf("[REQUEST] %s - starting", operationName)

            resp, err := next(ctx, req)

            log.Printf("[REQUEST] %s - completed in %v", operationName, time.Since(start))
            return resp, err
        }
    }
}

// Create client with both levels of middleware
client, _ := retry.NewClient(
    retry.WithMaxRetries(2),
    retry.WithInitialRetryDelay(100 * time.Millisecond),

    // Per-attempt middleware (executes for each HTTP attempt)
    retry.WithPerAttemptMiddleware(
        attemptLogger,
        retry.HeaderMiddleware(map[string]string{
            "X-Request-ID": "abc-123",
        }),
    ),

    // Request-level middleware (executes once per client call)
    retry.WithRequestMiddleware(
        requestTimer("api-call"),
        retry.RateLimitMiddleware(myRateLimiter),
    ),
)

// Make a request that requires 2 attempts
resp, err := client.Get(ctx, "https://api.example.com/data")
```

**Expected Output:**
```
[REQUEST] api-call - starting
[ATTEMPT] GET /data
[ATTEMPT] GET /data
[REQUEST] api-call - completed in 150ms
```

### Example 2: Request ID Propagation

```go
// Generate unique request ID per client call
func requestIDMiddleware() retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            // Generate ID once for entire retry operation
            requestID := generateUUID()

            // Store in context for per-attempt middleware to use
            ctx = context.WithValue(ctx, "request-id", requestID)

            // Clone request with new context
            req = req.WithContext(ctx)

            return next(ctx, req)
        }
    }
}

// Add request ID header to each attempt
func requestIDHeaderMiddleware(next http.RoundTripper) http.RoundTripper {
    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        req = req.Clone(req.Context())

        // Get request ID from context
        if id := req.Context().Value("request-id"); id != nil {
            req.Header.Set("X-Request-ID", id.(string))
        }

        return next.RoundTrip(req)
    })
}

client, _ := retry.NewClient(
    retry.WithRequestMiddleware(requestIDMiddleware()),
    retry.WithPerAttemptMiddleware(requestIDHeaderMiddleware),
)
```

### Example 3: Custom HTTP Client with Middleware

```go
// Create custom HTTP client with custom transport
customTransport := &http.Transport{
    MaxIdleConns:        100,
    IdleConnTimeout:     90 * time.Second,
    TLSHandshakeTimeout: 10 * time.Second,
}

customClient := &http.Client{
    Transport: customTransport,
    Timeout:   30 * time.Second,
}

// Apply middleware on top of custom client
client, _ := retry.NewClient(
    retry.WithHTTPClient(customClient),
    retry.WithPerAttemptMiddleware(
        retry.LoggingMiddleware(myLogger),
        retry.HeaderMiddleware(map[string]string{
            "X-Custom": "value",
        }),
    ),
)
```

**Important**: The middleware wraps the custom client's transport without modifying it. Your custom transport configuration is preserved.

## Best Practices

### 1. Choose the Right Middleware Level

- **Use Per-Attempt** when you need to inspect or modify individual HTTP attempts
- **Use Request-Level** when you need to control or observe the entire retry operation

### 2. Clone Requests Before Modification

Always clone requests before modifying them in per-attempt middleware:

```go
// ✅ Good
req = req.Clone(req.Context())
req.Header.Set("X-Custom", "value")

// ❌ Bad - can cause race conditions
req.Header.Set("X-Custom", "value")
```

### 3. Handle Errors Gracefully

Middleware should handle errors and allow the retry logic to proceed when appropriate:

```go
func resilientMiddleware(next http.RoundTripper) http.RoundTripper {
    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        // Non-critical operation - don't fail the request if it fails
        if err := doSomething(); err != nil {
            log.Printf("Warning: middleware operation failed: %v", err)
            // Continue anyway
        }

        return next.RoundTrip(req)
    })
}
```

### 4. Keep Middleware Focused

Each middleware should have a single, clear responsibility:

```go
// ✅ Good - focused middleware
func loggingMiddleware() { /* ... */ }
func metricsMiddleware() { /* ... */ }
func tracingMiddleware() { /* ... */ }

// ❌ Bad - does too much
func everythingMiddleware() { /* logs, metrics, tracing, headers, etc. */ }
```

### 5. Be Aware of Execution Count

Remember that per-attempt middleware runs multiple times:

```go
// ⚠️ This will log 3 times if the request requires 3 attempts
func loggingMiddleware(next http.RoundTripper) http.RoundTripper {
    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        log.Printf("Making HTTP request")  // Logs for each attempt
        return next.RoundTrip(req)
    })
}
```

If you only want one log per client call, use request-level middleware instead.

### 6. Respect Context Cancellation

Always respect context cancellation in your middleware:

```go
func contextAwareMiddleware(limiter RateLimiter) retry.RequestMiddleware {
    return func(next retry.RetryFunc) retry.RetryFunc {
        return func(ctx context.Context, req *http.Request) (*http.Response, error) {
            // Check context before expensive operation
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            default:
            }

            // Wait for rate limiter (respects context)
            if err := limiter.Wait(ctx); err != nil {
                return nil, err
            }

            return next(ctx, req)
        }
    }
}
```

### 7. Order Matters

Place middleware in logical order:

```go
client, _ := retry.NewClient(
    // Request-level: outermost operations first
    retry.WithRequestMiddleware(
        rateLimitMiddleware,      // Check rate limit first
        circuitBreakerMiddleware, // Then check circuit breaker
        timingMiddleware,         // Finally wrap with timing
    ),

    // Per-attempt: outermost operations first
    retry.WithPerAttemptMiddleware(
        loggingMiddleware,        // Log first
        metricsMiddleware,        // Collect metrics
        headerMiddleware,         // Add headers last (closest to transport)
    ),
)
```

### 8. Thread Safety

Per-attempt middleware must be thread-safe as the wrapped `RoundTripper` may be shared across goroutines:

```go
// ✅ Good - stateless or uses proper synchronization
func safeMiddleware(next http.RoundTripper) http.RoundTripper {
    var mu sync.Mutex
    counter := 0

    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        mu.Lock()
        counter++
        mu.Unlock()

        return next.RoundTrip(req)
    })
}

// ❌ Bad - race condition
func unsafeMiddleware(next http.RoundTripper) http.RoundTripper {
    counter := 0  // Not protected

    return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        counter++  // Race condition!
        return next.RoundTrip(req)
    })
}
```

## See Also

- [Complete Middleware Example](_example/middleware) - Runnable examples demonstrating both middleware levels
- [Observability](OBSERVABILITY.md) - Built-in observability features (metrics, tracing, logging)
- [Configuration Options](CONFIGURATION.md) - All available client configuration options
