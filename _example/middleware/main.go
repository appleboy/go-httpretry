package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

// Example: Per-Attempt Logging Middleware
// This middleware logs each individual HTTP attempt, including retries.
func customLoggingMiddleware(next http.RoundTripper) http.RoundTripper {
	return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		start := time.Now()
		log.Printf("[ATTEMPT] %s %s - starting", req.Method, req.URL.Path)

		resp, err := next.RoundTrip(req)
		duration := time.Since(start)

		if err != nil {
			log.Printf("[ATTEMPT] %s %s - failed after %v: %v",
				req.Method, req.URL.Path, duration, err)
		} else {
			log.Printf("[ATTEMPT] %s %s - completed in %v with status %d",
				req.Method, req.URL.Path, duration, resp.StatusCode)
		}

		return resp, err
	})
}

// Example: Request ID Header Middleware
// This middleware adds a unique request ID to each attempt.
func requestIDMiddleware(requestID string) retry.Middleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return retry.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Clone request before modification (concurrent safety)
			req = req.Clone(req.Context())
			req.Header.Set("X-Request-ID", requestID)
			return next.RoundTrip(req)
		})
	}
}

// Example: Simple Rate Limiter
// This is a basic token bucket rate limiter implementation.
type simpleRateLimiter struct {
	ticker   *time.Ticker
	tokens   chan struct{}
	capacity int
}

func newSimpleRateLimiter(rate int, duration time.Duration) *simpleRateLimiter {
	rl := &simpleRateLimiter{
		ticker:   time.NewTicker(duration / time.Duration(rate)),
		tokens:   make(chan struct{}, rate),
		capacity: rate,
	}

	// Fill initial tokens
	for i := 0; i < rate; i++ {
		rl.tokens <- struct{}{}
	}

	// Refill tokens periodically
	go func() {
		for range rl.ticker.C {
			select {
			case rl.tokens <- struct{}{}:
			default:
				// Token bucket is full
			}
		}
	}()

	return rl
}

func (rl *simpleRateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rl *simpleRateLimiter) Close() {
	rl.ticker.Stop()
}

// Example: Request-Level Timing Middleware
// This middleware times the entire retry operation (not per attempt).
func timingMiddleware(operationName string) retry.RequestMiddleware {
	return func(next retry.RetryFunc) retry.RetryFunc {
		return func(ctx context.Context, req *http.Request) (*http.Response, error) {
			start := time.Now()
			log.Printf("[REQUEST] %s - starting retry operation", operationName)

			resp, err := next(ctx, req)
			duration := time.Since(start)

			if err != nil {
				log.Printf("[REQUEST] %s - failed after %v: %v", operationName, duration, err)
			} else {
				log.Printf("[REQUEST] %s - completed in %v with status %d",
					operationName, duration, resp.StatusCode)
			}

			return resp, err
		}
	}
}

func main() {
	fmt.Println("=== go-httpretry Middleware Examples ===")

	// Example 1: Per-Attempt Logging
	// This shows middleware that runs for EVERY HTTP attempt
	fmt.Println("Example 1: Per-Attempt Logging Middleware")
	fmt.Println("-------------------------------------------")
	runPerAttemptLoggingExample()
	fmt.Println()

	// Example 2: Request-Level Rate Limiting
	// This shows middleware that runs ONCE per client call
	fmt.Println("Example 2: Request-Level Rate Limiting")
	fmt.Println("---------------------------------------")
	runRateLimitingExample()
	fmt.Println()

	// Example 3: Combined Middleware
	// This shows both levels of middleware working together
	fmt.Println("Example 3: Combined Middleware (Per-Attempt + Request-Level)")
	fmt.Println("-------------------------------------------------------------")
	runCombinedExample()
	fmt.Println()
}

func runPerAttemptLoggingExample() {
	// Create a test server that fails twice, then succeeds
	var attemptCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count <= 2 {
			log.Printf("[SERVER] Attempt %d - returning 503", count)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		log.Printf("[SERVER] Attempt %d - returning 200", count)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Success after %d attempts", count)
	}))
	defer server.Close()

	// Create client with per-attempt logging middleware
	client, err := retry.NewClient(
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(100*time.Millisecond),
		retry.WithPerAttemptMiddleware(customLoggingMiddleware),
		retry.WithNoLogging(), // Disable built-in logging for clarity
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Make request
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf(
		"\n✓ Request succeeded with status %d after %d attempts\n",
		resp.StatusCode,
		attemptCount,
	)
	fmt.Printf("  Note: Logging middleware executed %d times (once per attempt)\n", attemptCount)
}

func runRateLimitingExample() {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Success")
	}))
	defer server.Close()

	// Create rate limiter: 2 requests per second
	limiter := newSimpleRateLimiter(2, time.Second)
	defer limiter.Close()

	// Create client with rate limiting middleware
	client, err := retry.NewClient(
		retry.WithRequestMiddleware(retry.RateLimitMiddleware(limiter)),
		retry.WithNoLogging(),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Make 3 requests - the third should be rate limited
	start := time.Now()
	for i := 1; i <= 3; i++ {
		requestStart := time.Now()
		log.Printf("[RATE LIMIT] Making request %d...", i)

		resp, err := client.Get(context.Background(), server.URL)
		if err != nil {
			log.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()

		elapsed := time.Since(requestStart)
		log.Printf("[RATE LIMIT] Request %d completed after %v", i, elapsed)
	}

	totalElapsed := time.Since(start)
	fmt.Printf("\n✓ Completed 3 requests in %v (rate limited to 2/second)\n", totalElapsed)
	fmt.Printf("  Note: Rate limiting middleware executed 3 times (once per client call)\n")
}

func runCombinedExample() {
	// Create a test server that fails once, then succeeds
	var attemptCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		reqID := r.Header.Get("X-Request-ID")
		log.Printf("[SERVER] Request ID: %s, Attempt %d", reqID, count)

		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Success")
	}))
	defer server.Close()

	// Create client with BOTH per-attempt and request-level middleware
	requestID := fmt.Sprintf("req-%d", time.Now().Unix())
	client, err := retry.NewClient(
		retry.WithMaxRetries(2),
		retry.WithInitialRetryDelay(100*time.Millisecond),
		// Per-attempt middleware (runs for each HTTP call)
		retry.WithPerAttemptMiddleware(
			customLoggingMiddleware,
			requestIDMiddleware(requestID),
		),
		// Request-level middleware (runs once for entire operation)
		retry.WithRequestMiddleware(
			timingMiddleware("combined-example"),
		),
		retry.WithNoLogging(),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Make request
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("\n✓ Request succeeded with status %d\n", resp.StatusCode)
	fmt.Printf("  Per-Attempt middleware: executed %d times (logging + headers)\n", attemptCount)
	fmt.Printf("  Request-Level middleware: executed 1 time (timing)\n")
	fmt.Printf("  Request ID: %s\n", requestID)
}
