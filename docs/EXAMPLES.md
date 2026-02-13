# Examples

This document provides detailed examples for various use cases of the HTTP retry client.

## Table of Contents

- [Using Convenience Methods](#using-convenience-methods)
- [Disable Retries](#disable-retries)
- [Aggressive Retries for Critical Requests](#aggressive-retries-for-critical-requests)
- [Conservative Retries for Background Tasks](#conservative-retries-for-background-tasks)
- [Custom Retry Logic for Authentication Tokens](#custom-retry-logic-for-authentication-tokens)
- [Per-Attempt Timeout for Slow Requests](#per-attempt-timeout-for-slow-requests)
- [Retry with Jitter to Prevent Thundering Herd](#retry-with-jitter-to-prevent-thundering-herd)
- [Respect Rate Limiting with Retry-After Header](#respect-rate-limiting-with-retry-after-header)
- [Complete Working Examples](#complete-working-examples)

## Using Convenience Methods

The convenience methods provide a simpler API for common HTTP operations:

```go
client, err := retry.NewClient(retry.WithMaxRetries(3))
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()

// Simple GET request
resp, err := client.Get(ctx, "https://api.example.com/users")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

// POST with JSON body
user := User{Name: "John", Email: "john@example.com"}
jsonData, _ := json.Marshal(user)
resp, err = client.Post(ctx, "https://api.example.com/users",
    retry.WithBody("application/json", bytes.NewReader(jsonData)))

// PUT with authentication
resp, err = client.Put(ctx, "https://api.example.com/users/123",
    retry.WithBody("application/json", bytes.NewReader(jsonData)),
    retry.WithHeader("Authorization", "Bearer token"))

// DELETE request
resp, err = client.Delete(ctx, "https://api.example.com/users/123")

// GET with custom headers
resp, err = client.Get(ctx, "https://api.example.com/protected",
    retry.WithHeader("Authorization", "Bearer token"),
    retry.WithHeader("X-Request-ID", "req-123"))
```

## Disable Retries

```go
// Set maxRetries to 0 to disable retries
client, err := retry.NewClient(retry.WithMaxRetries(0))
if err != nil {
    log.Fatal(err)
}
```

## Aggressive Retries for Critical Requests

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

## Conservative Retries for Background Tasks

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

## Custom Retry Logic for Authentication Tokens

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

## Per-Attempt Timeout for Slow Requests

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

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/slow-endpoint", nil)
resp, err := client.Do(req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

// Result: Up to 5 attempts (initial + 4 retries) can be made
// Each attempt fails fast after 3 seconds instead of hanging
// This gives you more chances to succeed within the 15-second window
```

## Retry with Jitter to Prevent Thundering Herd

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
resp, err := client.DoWithContext(ctx, req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## Respect Rate Limiting with Retry-After Header

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
resp, err := client.DoWithContext(ctx, req)
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## Complete Working Examples

For complete, runnable examples, see:

- [_example/basic](_example/basic) - Basic usage with default settings
- [_example/advanced](_example/advanced) - Advanced configuration with observability and custom retry logic
- [_example/convenience_methods](_example/convenience_methods) - Using convenience HTTP methods (GET, POST, PUT, DELETE, HEAD, PATCH)
- [_example/request_options](_example/request_options) - Request options usage (WithBody, WithHeader, WithHeaders)
- `example_test.go` - Additional examples and test cases

Each example can be run independently:

```bash
cd _example/basic && go run main.go
cd _example/advanced && go run main.go
cd _example/convenience_methods && go run main.go
cd _example/request_options && go run main.go
```
