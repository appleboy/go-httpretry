package retry

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// RoundTripperFunc is a function type that implements http.RoundTripper.
// This is a convenience type for creating functional middleware without defining new types.
//
// Example usage:
//
//	func LoggingMiddleware(next http.RoundTripper) http.RoundTripper {
//	    return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
//	        log.Printf("Request: %s %s", req.Method, req.URL)
//	        return next.RoundTrip(req)
//	    })
//	}
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper interface
func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// LoggingMiddleware creates per-attempt middleware that logs each HTTP attempt.
// It logs before and after each attempt, including timing information.
//
// Example:
//
//	client, _ := retry.NewClient(
//	    retry.WithPerAttemptMiddleware(retry.LoggingMiddleware(myLogger)),
//	)
//
// The logger will receive one log per HTTP attempt (including retries).
func LoggingMiddleware(logger Logger) Middleware {
	if logger == nil {
		logger = nopLogger{}
	}

	return func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()

			logger.Debug("http attempt starting",
				"method", req.Method,
				"url", req.URL.String(),
			)

			resp, err := next.RoundTrip(req)
			duration := time.Since(start)

			if err != nil {
				logger.Warn("http attempt failed",
					"method", req.Method,
					"url", req.URL.String(),
					"duration_ms", duration.Milliseconds(),
					"error", err.Error(),
				)
			} else {
				logger.Debug("http attempt completed",
					"method", req.Method,
					"url", req.URL.String(),
					"status", resp.StatusCode,
					"duration_ms", duration.Milliseconds(),
				)
			}

			return resp, err
		})
	}
}

// HeaderMiddleware creates per-attempt middleware that adds headers to each request.
// Headers are added to every HTTP attempt including retries.
//
// Example:
//
//	client, _ := retry.NewClient(
//	    retry.WithPerAttemptMiddleware(
//	        retry.HeaderMiddleware(map[string]string{
//	            "X-Client-Version": "1.0.0",
//	            "User-Agent": "my-client/1.0",
//	        }),
//	    ),
//	)
//
// Note: The request is cloned before modification to ensure concurrent safety.
func HeaderMiddleware(headers map[string]string) Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Clone request before modification (concurrent safety)
			req = req.Clone(req.Context())

			for key, value := range headers {
				req.Header.Set(key, value)
			}

			return next.RoundTrip(req)
		})
	}
}

// RateLimiter is the interface for rate limiting implementations.
// Wait blocks until the rate limiter allows the request to proceed or context is cancelled.
type RateLimiter interface {
	Wait(ctx context.Context) error
}

// RateLimitMiddleware creates request-level middleware that applies rate limiting.
// The rate limiter is checked ONCE per client call, before the retry loop begins.
//
// Example:
//
//	limiter := NewTokenBucketLimiter(10, time.Second) // 10 requests per second
//	client, _ := retry.NewClient(
//	    retry.WithRequestMiddleware(retry.RateLimitMiddleware(limiter)),
//	)
//
// If the rate limit is exceeded, Wait() will block until capacity is available
// or return an error if the context is cancelled.
func RateLimitMiddleware(limiter RateLimiter) RequestMiddleware {
	return func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			if err := limiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("rate limit: %w", err)
			}
			return next(ctx, req)
		}
	}
}

// CircuitBreaker is the interface for circuit breaker implementations.
// Allow checks if the circuit breaker allows the request to proceed.
// RecordSuccess and RecordFailure update the circuit breaker state.
type CircuitBreaker interface {
	// Allow checks if the request is allowed to proceed
	Allow() error

	// RecordSuccess records a successful request
	RecordSuccess()

	// RecordFailure records a failed request
	RecordFailure()
}

// CircuitBreakerMiddleware creates request-level middleware that implements circuit breaker pattern.
// The circuit breaker prevents requests when the service is unhealthy (circuit open).
//
// Example:
//
//	cb := NewCircuitBreaker(5, time.Minute) // Open after 5 failures for 1 minute
//	client, _ := retry.NewClient(
//	    retry.WithRequestMiddleware(retry.CircuitBreakerMiddleware(cb)),
//	)
//
// When the circuit is open, requests fail immediately without attempting the HTTP call.
// This protects downstream services from cascading failures.
func CircuitBreakerMiddleware(cb CircuitBreaker) RequestMiddleware {
	return func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			// Check if circuit breaker allows the request
			if err := cb.Allow(); err != nil {
				return nil, fmt.Errorf("circuit breaker: %w", err)
			}

			// Execute request
			resp, err := next(ctx, req)

			// Record result
			if err != nil || (resp != nil && resp.StatusCode >= 500) {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}

			return resp, err
		}
	}
}

// TracingRequestMiddleware creates request-level middleware that adds distributed tracing.
// It creates a single span for the entire retry operation (not per attempt).
//
// Example:
//
//	tracer := myOpenTelemetryTracer
//	client, _ := retry.NewClient(
//	    retry.WithRequestMiddleware(retry.TracingRequestMiddleware(tracer)),
//	)
//
// For per-attempt tracing, use WithTracer() option instead, which provides
// automatic spans for both the request and each individual attempt.
func TracingRequestMiddleware(tracer Tracer) RequestMiddleware {
	return func(next RetryFunc) RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			// Start request-level span
			ctx, span := tracer.StartSpan(ctx, "http.request.with_retry",
				Attribute{Key: "http.method", Value: req.Method},
				Attribute{Key: "http.url", Value: req.URL.String()},
			)
			defer span.End()

			// Execute request with retry
			resp, err := next(ctx, req)

			// Record result in span
			if err != nil {
				span.SetStatus("error", err.Error())
			} else if resp != nil {
				span.SetAttributes(
					Attribute{Key: "http.status_code", Value: resp.StatusCode},
				)
				if resp.StatusCode >= 400 {
					span.SetStatus("error", fmt.Sprintf("HTTP %d", resp.StatusCode))
				} else {
					span.SetStatus("ok", "")
				}
			}

			return resp, err
		}
	}
}
