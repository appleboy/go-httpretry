# Error Handling

When all retry attempts are exhausted or the context is cancelled, the client returns a structured `RetryError` that provides detailed information about the failure. This allows for programmatic error inspection and better debugging.

## Table of Contents

- [RetryError Structure](#retryerror-structure)
- [Using RetryError](#using-retryerror)
- [Error Wrapping Support](#error-wrapping-support)
- [Response Availability](#response-availability)
- [Examples](#examples)

## RetryError Structure

The `RetryError` type contains:

- **Attempts**: Total number of attempts made (initial request + retries)
- **LastErr**: The underlying error from the last attempt (e.g., network error, context timeout)
- **LastStatus**: HTTP status code from the last attempt (0 if the request failed before receiving a response)
- **Elapsed**: Total time elapsed from the first attempt to the final failure

## Using RetryError

```go
import (
    "context"
    "errors"
    "log"
    "net/http"
    "time"

    "github.com/appleboy/go-httpretry"
)

client, _ := retry.NewClient(
    retry.WithMaxRetries(3),
    retry.WithInitialRetryDelay(1*time.Second),
)

req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.DoWithContext(context.Background(), req)
if err != nil {
    // Check if it's a RetryError
    var retryErr *retry.RetryError
    if errors.As(err, &retryErr) {
        log.Printf("Request failed after %d attempts in %v",
            retryErr.Attempts,
            retryErr.Elapsed,
        )

        // Check the last HTTP status code
        if retryErr.LastStatus != 0 {
            log.Printf("Last HTTP status: %d", retryErr.LastStatus)
        }

        // Check for specific underlying errors
        if errors.Is(err, context.DeadlineExceeded) {
            log.Println("Timeout occurred during retries")
        }

        // Access the underlying error
        if retryErr.LastErr != nil {
            log.Printf("Last error: %v", retryErr.LastErr)
        }
    }
    return
}
defer resp.Body.Close()
```

## Error Wrapping Support

`RetryError` implements Go's error wrapping interface (`Unwrap()`), which means you can use `errors.Is()` and `errors.As()` to check for underlying errors:

```go
import (
    "context"
    "errors"
    "log"
    "net"
)

// Using request's context
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(req)
if err != nil {
    // Check if the underlying error is a context timeout
    if errors.Is(err, context.DeadlineExceeded) {
        log.Println("Request timed out")
    }

    // Check if it's a specific network error
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        log.Println("Network timeout occurred")
    }
}
```

## Response Availability

**Important**: When all retries are exhausted but the last attempt received a response (even with an error status like 500), both the `response` and the `RetryError` are returned. This allows you to inspect the final response:

```go
import (
    "errors"
    "io"
    "log"

    "github.com/appleboy/go-httpretry"
)

// Using request's context
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(req)
if err != nil {
    var retryErr *retry.RetryError
    if errors.As(err, &retryErr) {
        log.Printf("Exhausted retries: %d attempts", retryErr.Attempts)

        // The response may still be available for inspection
        if resp != nil {
            defer resp.Body.Close()
            log.Printf("Last response status: %d", resp.StatusCode)

            // You can read the response body if needed
            body, _ := io.ReadAll(resp.Body)
            log.Printf("Last response body: %s", body)
        }
    }
}
```

## Examples

### Basic Error Inspection

```go
client, _ := retry.NewClient(
    retry.WithMaxRetries(3),
    retry.WithInitialRetryDelay(1*time.Second),
)

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(req)
if err != nil {
    // Use RetryError for detailed failure analysis
    var retryErr *retry.RetryError
    if errors.As(err, &retryErr) {
        // Log detailed retry information
        log.Printf("Request failed:")
        log.Printf("  - Total attempts: %d", retryErr.Attempts)
        log.Printf("  - Time elapsed: %v", retryErr.Elapsed)
        log.Printf("  - Last status: %d", retryErr.LastStatus)

        // Check if it was a timeout
        if errors.Is(err, context.DeadlineExceeded) {
            log.Println("  - Reason: timeout")
            // Handle timeout specifically (e.g., use circuit breaker)
            return
        }

        // Check if it was a server error
        if retryErr.LastStatus >= 500 {
            log.Println("  - Reason: server error")
            // Handle server errors (e.g., alert on-call team)
            return
        }

        // Handle other errors
        if retryErr.LastErr != nil {
            log.Printf("  - Error: %v", retryErr.LastErr)
        }
    }
    return
}
defer resp.Body.Close()

log.Println("Request succeeded!")
```

### Observability with Retry Callbacks

```go
// Set up retry callback for logging and metrics
var retryCount int
client, err := retry.NewClient(
    retry.WithMaxRetries(3),
    retry.WithOnRetry(func(info retry.RetryInfo) {
        retryCount++
        log.Printf("[RETRY] Attempt %d/%d after %v (total: %v)",
            info.Attempt,
            3, // max retries
            info.Delay,
            info.TotalElapsed,
        )

        if info.Err != nil {
            log.Printf("[RETRY] Error: %v", info.Err)
        } else {
            log.Printf("[RETRY] Status: %d", info.StatusCode)
        }

        if info.RetryAfter > 0 {
            log.Printf("[RETRY] Server requested retry after: %v", info.RetryAfter)
        }

        // Send metrics to your monitoring system
        // metrics.IncrementRetryCounter("api_call", info.StatusCode)
    }),
)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.DoWithContext(ctx, req)
if err != nil {
    log.Printf("Request failed after %d retries: %v", retryCount, err)
    return
}
defer resp.Body.Close()

log.Printf("Request succeeded after %d retries", retryCount)
```

### Production-Ready Error Handling

```go
client, err := retry.NewClient(
    // Basic retry configuration
    retry.WithMaxRetries(5),
    retry.WithInitialRetryDelay(1*time.Second),
    retry.WithMaxRetryDelay(30*time.Second),
    retry.WithRetryDelayMultiple(2.0),

    // Note: Jitter and Retry-After are enabled by default
    // - Jitter prevents thundering herd problem
    // - Retry-After respects HTTP standard for rate limiting
    // No need to call WithJitter(true) or WithRespectRetryAfter(true)

    // Observability
    retry.WithOnRetry(func(info retry.RetryInfo) {
        // Log retry attempts
        logger.Warn("HTTP request retry",
            "attempt", info.Attempt,
            "delay", info.Delay,
            "status", info.StatusCode,
            "elapsed", info.TotalElapsed,
        )

        // Record metrics
        metrics.RecordRetry(info.StatusCode, info.Attempt)
    }),

    // Custom retry logic
    retry.WithRetryableChecker(func(err error, resp *http.Response) bool {
        // Always retry network errors
        if err != nil {
            return true
        }

        // Retry on 5xx, 429, and also 408 (Request Timeout)
        if resp != nil {
            return resp.StatusCode >= 500 ||
                   resp.StatusCode == http.StatusTooManyRequests ||
                   resp.StatusCode == http.StatusRequestTimeout
        }

        return false
    }),
)
if err != nil {
    log.Fatal(err)
}

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(req)
if err != nil {
    var retryErr *retry.RetryError
    if errors.As(err, &retryErr) {
        logger.Error("Request failed",
            "attempts", retryErr.Attempts,
            "elapsed", retryErr.Elapsed,
            "last_status", retryErr.LastStatus,
            "error", retryErr.LastErr,
        )

        // Increment failure metrics
        metrics.IncrementFailureCounter("api_call", retryErr.LastStatus)
    }
    return
}
defer resp.Body.Close()

log.Println("Request succeeded!")
```
