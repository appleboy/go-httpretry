# go-httpretry

[![Testing](https://github.com/appleboy/go-httpretry/actions/workflows/testing.yml/badge.svg)](https://github.com/appleboy/go-httpretry/actions/workflows/testing.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/appleboy/go-httpretry)](https://goreportcard.com/report/github.com/appleboy/go-httpretry)
[![codecov](https://codecov.io/gh/appleboy/go-httpretry/branch/main/graph/badge.svg)](https://codecov.io/gh/appleboy/go-httpretry)
[![Go Reference](https://pkg.go.dev/badge/github.com/appleboy/go-httpretry.svg)](https://pkg.go.dev/github.com/appleboy/go-httpretry)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A flexible HTTP client with automatic retry logic using exponential backoff, built with the Functional Options Pattern.

## Table of Contents

- [go-httpretry](#go-httpretry)
  - [Table of Contents](#table-of-contents)
  - [Features](#features)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
    - [Basic Usage (Default Settings)](#basic-usage-default-settings)
    - [Custom Configuration](#custom-configuration)
  - [Configuration Options](#configuration-options)
    - [`WithMaxRetries(n int)`](#withmaxretriesn-int)
    - [`WithInitialRetryDelay(d time.Duration)`](#withinitialretrydelayd-timeduration)
    - [`WithMaxRetryDelay(d time.Duration)`](#withmaxretrydelayd-timeduration)
    - [`WithRetryDelayMultiple(multiplier float64)`](#withretrydelaymultiplemultiplier-float64)
    - [`WithHTTPClient(httpClient *http.Client)`](#withhttpclienthttpclient-httpclient)
    - [`WithRetryableChecker(checker RetryableChecker)`](#withretryablecheckerchecker-retryablechecker)
    - [`WithJitter(enabled bool)`](#withjitterenabled-bool)
    - [`WithRespectRetryAfter(enabled bool)`](#withrespectretryafterenabled-bool)
    - [`WithPerAttemptTimeout(d time.Duration)`](#withperattempttimeoutd-timeduration)
    - [`WithOnRetry(fn OnRetryFunc)`](#withonretryfn-onretryfunc)
  - [Default Retry Behavior](#default-retry-behavior)
  - [Exponential Backoff](#exponential-backoff)
  - [Context Support](#context-support)
  - [Examples](#examples)
    - [Disable Retries](#disable-retries)
    - [Aggressive Retries for Critical Requests](#aggressive-retries-for-critical-requests)
    - [Conservative Retries for Background Tasks](#conservative-retries-for-background-tasks)
    - [Custom Retry Logic for Authentication Tokens](#custom-retry-logic-for-authentication-tokens)
    - [Per-Attempt Timeout for Slow Requests](#per-attempt-timeout-for-slow-requests)
    - [Retry with Jitter to Prevent Thundering Herd](#retry-with-jitter-to-prevent-thundering-herd)
    - [Respect Rate Limiting with Retry-After Header](#respect-rate-limiting-with-retry-after-header)
    - [Observability with Retry Callbacks](#observability-with-retry-callbacks)
    - [Production-Ready Configuration](#production-ready-configuration)
    - [Custom TLS Configuration](#custom-tls-configuration)
      - [Example: Custom CA Certificate](#example-custom-ca-certificate)
      - [Example: Skip TLS Verification (Testing Only)](#example-skip-tls-verification-testing-only)
  - [Testing](#testing)
  - [Design Principles](#design-principles)
  - [License](#license)
  - [Author](#author)

## Features

- **Automatic Retries**: Retries failed requests with configurable exponential backoff
- **Smart Retry Logic**: Default retries on network errors, 5xx server errors, and 429 (Too Many Requests)
- **Jitter Support**: Optional random jitter to prevent thundering herd problem
- **Retry-After Header**: Respects HTTP `Retry-After` header for rate limiting (RFC 2616)
- **Observability Hooks**: Callback functions for logging, metrics, and custom retry logic
- **Flexible Configuration**: Use functional options to customize retry behavior
- **Context Support**: Respects context cancellation and timeouts
- **Custom Retry Logic**: Pluggable retry checker for custom retry conditions
- **Resource Safe**: Automatically closes response bodies before retries to prevent leaks
- **Zero Dependencies**: Uses only Go standard library

## Installation

Install the package using `go get`:

```bash
go get github.com/appleboy/go-httpretry
```

Then import it in your Go code:

```go
import "github.com/appleboy/go-httpretry"
```

## Quick Start

### Basic Usage (Default Settings)

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/appleboy/go-httpretry"
)

func main() {
    // Create a retry client with defaults:
    // - 3 max retries
    // - 1 second initial delay
    // - 10 second max delay
    // - 2.0x exponential multiplier
    // - Jitter enabled (±25% randomization)
    // - Retry-After header respected (HTTP standard compliant)
    client, err := retry.NewClient()
    if err != nil {
        log.Fatal(err)
    }

    req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
    resp, err := client.Do(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
}
```

### Custom Configuration

```go
client, err := retry.NewClient(
    retry.WithMaxRetries(5),                           // Retry up to 5 times
    retry.WithInitialRetryDelay(500*time.Millisecond), // Start with 500ms delay
    retry.WithMaxRetryDelay(30*time.Second),           // Cap delay at 30s
    retry.WithRetryDelayMultiple(3.0),                 // Triple delay each time
)
if err != nil {
    log.Fatal(err)
}
```

## Configuration Options

### `WithMaxRetries(n int)`

Sets the maximum number of retry attempts.

```go
client, err := retry.NewClient(retry.WithMaxRetries(5))
if err != nil {
    log.Fatal(err)
}
```

### `WithInitialRetryDelay(d time.Duration)`

Sets the initial delay before the first retry.

```go
client, err := retry.NewClient(retry.WithInitialRetryDelay(500*time.Millisecond))
if err != nil {
    log.Fatal(err)
}
```

### `WithMaxRetryDelay(d time.Duration)`

Sets the maximum delay between retries (caps exponential backoff).

```go
client, err := retry.NewClient(retry.WithMaxRetryDelay(30*time.Second))
if err != nil {
    log.Fatal(err)
}
```

### `WithRetryDelayMultiple(multiplier float64)`

Sets the exponential backoff multiplier.

```go
client, err := retry.NewClient(retry.WithRetryDelayMultiple(3.0))
if err != nil {
    log.Fatal(err)
}
```

### `WithHTTPClient(httpClient *http.Client)`

Uses a custom `http.Client` instead of `http.DefaultClient`.

```go
httpClient := &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns: 100,
    },
}
client, err := retry.NewClient(retry.WithHTTPClient(httpClient))
if err != nil {
    log.Fatal(err)
}
```

### `WithRetryableChecker(checker RetryableChecker)`

Provides custom logic for determining which errors should trigger retries.

```go
customChecker := func(err error, resp *http.Response) bool {
    if err != nil {
        return true // Always retry network errors
    }
    if resp == nil {
        return false
    }
    // Retry on 5xx, 429, and also 403
    return resp.StatusCode >= 500 ||
           resp.StatusCode == http.StatusTooManyRequests ||
           resp.StatusCode == http.StatusForbidden
}

client, err := retry.NewClient(retry.WithRetryableChecker(customChecker))
if err != nil {
    log.Fatal(err)
}
```

### `WithJitter(enabled bool)`

Controls random jitter to prevent thundering herd problem. **Jitter is enabled by default.** When enabled, retry delays will be randomized by ±25% to avoid synchronized retries from multiple clients.

```go
// Jitter is enabled by default, but you can explicitly disable it if needed
client, err := retry.NewClient(
    retry.WithJitter(false), // Disable jitter for predictable delays
    retry.WithMaxRetries(3),
)
```

**Use Case**: When multiple clients might fail simultaneously (e.g., during a service outage), jitter prevents them from retrying at the exact same time, reducing load spikes on the recovering service. This is the recommended behavior for most production use cases.

### `WithRespectRetryAfter(enabled bool)`

Controls whether to respect the `Retry-After` header from HTTP responses. **This is enabled by default** to comply with HTTP standards (RFC 7231). When enabled, the client will use the server-provided retry delay instead of exponential backoff.

The `Retry-After` header can be:

- **Seconds**: An integer number of seconds (e.g., `Retry-After: 120`)
- **HTTP-date**: An RFC1123 date (e.g., `Retry-After: Wed, 21 Oct 2015 07:28:00 GMT`)

```go
// Retry-After is enabled by default, but you can explicitly disable it if needed
// (not recommended in most cases)
client, err := retry.NewClient(
    retry.WithRespectRetryAfter(false), // Ignore Retry-After header
    retry.WithMaxRetries(5),
)
```

**Use Case**: Essential for proper rate limiting compliance. When a server responds with 429 (Too Many Requests) or 503 (Service Unavailable), it often includes a `Retry-After` header indicating when to retry. Respecting this header is the correct behavior for a well-behaved HTTP client.

### `WithPerAttemptTimeout(d time.Duration)`

Sets a timeout for each individual retry attempt. This prevents a single slow request from consuming all available retry time. By default, no per-attempt timeout is set (0), and only the overall context timeout applies.

```go
client, err := retry.NewClient(
    retry.WithPerAttemptTimeout(5*time.Second), // Each attempt times out after 5s
    retry.WithMaxRetries(3),
    retry.WithInitialRetryDelay(1*time.Second),
)
```

**Use Case**: Critical for preventing slow individual requests from exhausting retry budgets. For example:

- If a request takes 30 seconds to timeout naturally, with 3 retries, you could wait 90 seconds
- With `WithPerAttemptTimeout(5*time.Second)`, each attempt fails faster, giving you more useful retry opportunities
- The per-attempt timeout works independently from the overall context timeout
- When an attempt times out, it's treated as a retryable error

**Example scenario:**

```go
// Overall timeout: 30 seconds
// Per-attempt timeout: 5 seconds
// Max retries: 5
// Result: Up to 6 attempts (initial + 5 retries) can be made within 30 seconds,
// each taking at most 5 seconds
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

client, _ := retry.NewClient(
    retry.WithPerAttemptTimeout(5*time.Second),
    retry.WithMaxRetries(5),
)
```

### `WithOnRetry(fn OnRetryFunc)`

Sets a callback function that will be called before each retry attempt. Useful for logging, metrics collection, or custom retry logic.

```go
client, err := retry.NewClient(
    retry.WithOnRetry(func(info retry.RetryInfo) {
        log.Printf("Retry attempt %d after %v (status: %d, error: %v)",
            info.Attempt,
            info.Delay,
            info.StatusCode,
            info.Err,
        )
    }),
    retry.WithMaxRetries(3),
)
```

The `RetryInfo` struct contains:

- `Attempt`: Current attempt number (1-indexed)
- `Delay`: Delay before this retry
- `Err`: Error that triggered the retry (nil if retrying due to response status)
- `StatusCode`: HTTP status code (0 if request failed)
- `RetryAfter`: Retry-After duration from response header (0 if not present)
- `TotalElapsed`: Total time elapsed since first attempt

**Use Case**: Essential for production observability - integrate with your logging system, metrics (Prometheus, Datadog), or alerting.

## Default Retry Behavior

The `DefaultRetryableChecker` retries in the following cases:

- **Network errors**: Connection refused, timeouts, DNS errors, etc.
- **5xx Server Errors**: 500, 502, 503, 504, etc.
- **429 Too Many Requests**: Rate limiting errors

It does **NOT** retry:

- **4xx Client Errors** (except 429): 400, 401, 403, 404, etc.
- **2xx Success**: 200, 201, 204, etc.
- **3xx Redirects**: 301, 302, 307, etc.

## Exponential Backoff

Retries use exponential backoff to avoid overwhelming the server:

1. **First retry**: Wait `initialRetryDelay` (default: 1s)
2. **Second retry**: Wait `initialRetryDelay * multiplier` (default: 2s)
3. **Third retry**: Wait `initialRetryDelay * multiplier²` (default: 4s)
4. **Subsequent retries**: Continue multiplying until `maxRetryDelay` is reached

Example with defaults:

- Attempt 1: Immediate
- Attempt 2: After 1s
- Attempt 3: After 2s
- Attempt 4: After 4s

## Context Support

The client respects context cancellation and timeouts:

```go
// Overall timeout for the entire operation (including retries)
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := client.Do(ctx, req)
if err != nil {
    // May be context.DeadlineExceeded
    log.Printf("Request failed: %v", err)
}
```

## Examples

### Disable Retries

```go
// Set maxRetries to 0 to disable retries
client, err := retry.NewClient(retry.WithMaxRetries(0))
if err != nil {
    log.Fatal(err)
}
```

### Aggressive Retries for Critical Requests

```go
client, err := retry.NewClient(
    retry.WithMaxRetries(10),
    retry.WithInitialRetryDelay(100*time.Millisecond),
    retry.WithMaxRetryDelay(5*time.Second),
    retry.WithRetryDelayMultiple(1.5),
)
if err != nil {
    log.Fatal(err)
}
```

### Conservative Retries for Background Tasks

```go
client, err := retry.NewClient(
    retry.WithMaxRetries(2),
    retry.WithInitialRetryDelay(5*time.Second),
    retry.WithMaxRetryDelay(60*time.Second),
    retry.WithRetryDelayMultiple(2.0),
)
if err != nil {
    log.Fatal(err)
}
```

### Custom Retry Logic for Authentication Tokens

```go
// Retry on 401 Unauthorized (e.g., for token refresh scenarios)
authRetryChecker := func(err error, resp *http.Response) bool {
    if err != nil {
        return true
    }
    if resp == nil {
        return false
    }
    return resp.StatusCode >= 500 ||
           resp.StatusCode == http.StatusUnauthorized
}

client, err := retry.NewClient(
    retry.WithRetryableChecker(authRetryChecker),
    retry.WithMaxRetries(3),
)
if err != nil {
    log.Fatal(err)
}
```

### Per-Attempt Timeout for Slow Requests

```go
// Prevent slow individual requests from consuming all retry opportunities
// Each attempt gets 3 seconds max, with overall context timeout of 15 seconds
client, err := retry.NewClient(
    retry.WithPerAttemptTimeout(3*time.Second),
    retry.WithMaxRetries(4),
    retry.WithInitialRetryDelay(500*time.Millisecond),
)
if err != nil {
    log.Fatal(err)
}

// Set overall timeout for the entire operation
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/slow-endpoint", nil)
resp, err := client.Do(ctx, req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

// Result: Up to 5 attempts (initial + 4 retries) can be made
// Each attempt fails fast after 3 seconds instead of hanging
// This gives you more chances to succeed within the 15-second window
```

### Retry with Jitter to Prevent Thundering Herd

```go
// Jitter is enabled by default to randomize retry delays
// This prevents the thundering herd problem in production systems
client, err := retry.NewClient(
    retry.WithMaxRetries(5),
    retry.WithInitialRetryDelay(1*time.Second),
    // Note: WithJitter(true) is the default, no need to specify
)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(ctx, req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

### Respect Rate Limiting with Retry-After Header

```go
// Retry-After header support is enabled by default for proper rate limiting
// The client will automatically respect the server's Retry-After header
client, err := retry.NewClient(
    retry.WithMaxRetries(5),
    retry.WithInitialRetryDelay(1*time.Second), // Fallback if no Retry-After header
)
if err != nil {
    log.Fatal(err)
}

// When the server responds with 429 + Retry-After: 60,
// the client will wait 60 seconds instead of using exponential backoff
ctx := context.Background()
req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/rate-limited", nil)
resp, err := client.Do(ctx, req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
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
resp, err := client.Do(ctx, req)
if err != nil {
    log.Printf("Request failed after %d retries: %v", retryCount, err)
    return
}
defer resp.Body.Close()

log.Printf("Request succeeded after %d retries", retryCount)
```

### Production-Ready Configuration

Combine all features for a robust production setup:

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
```

### Custom TLS Configuration

For services requiring custom TLS certificates (e.g., self-signed certificates, internal CAs),
configure the TLS settings on your `http.Client` before passing it to the retry client.

#### Example: Custom CA Certificate

```go
// Load custom certificate
certPool, _ := x509.SystemCertPool()
certPEM, _ := os.ReadFile("/path/to/internal-ca.pem")
certPool.AppendCertsFromPEM(certPEM)

// Create HTTP client with TLS config
httpClient := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            RootCAs:    certPool,
            MinVersion: tls.VersionTLS12,
        },
    },
}

// Create retry client
client, _ := retry.NewClient(
    retry.WithHTTPClient(httpClient),
    retry.WithMaxRetries(3),
)
```

#### Example: Skip TLS Verification (Testing Only)

```go
// WARNING: Only for development/testing!
httpClient := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: true,
        },
    },
}

client, _ := retry.NewClient(retry.WithHTTPClient(httpClient))
```

**Why External TLS Configuration?**

- **Single Responsibility**: The retry client focuses solely on retry logic
- **Full Control**: You manage HTTP client settings (timeouts, connection pooling, TLS)
- **Standard Practice**: Use Go's standard `crypto/tls` package directly
- **Better Composability**: Works with any HTTP client configuration

See `example_test.go` for complete working examples.

## Testing

Run the test suite:

```bash
go test -v ./...
```

With coverage:

```bash
go test -v -cover ./...
```

## Design Principles

- **Functional Options Pattern**: Provides clean, flexible API for configuration
- **Sensible Defaults**: Works out of the box for most use cases
- **Separation of Concerns**: HTTP client configuration (including TLS) is the user's responsibility; retry logic is ours
- **Single Responsibility**: Focus exclusively on retry behavior, not HTTP client building
- **Context-Aware**: Respects cancellation and timeouts
- **Resource Safe**: Prevents response body leaks by closing them before retries
- **Request Cloning**: Clones requests for each retry to handle consumed request bodies
- **Zero Dependencies**: Uses only standard library

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

Copyright (c) 2026 Bo-Yi Wu

## Author

- GitHub: [@appleboy](https://github.com/appleboy)
- Website: [https://blog.wu-boy.com](https://blog.wu-boy.com)

Support this project:

[![Donate](https://img.shields.io/badge/Donate-PayPal-green.svg)](https://www.paypal.me/appleboy46)
