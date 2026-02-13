# Configuration Options

This document describes all available configuration options for the HTTP retry client.

## Table of Contents

- [WithMaxRetries](#withmaxretries)
- [WithInitialRetryDelay](#withinitialretrydelay)
- [WithMaxRetryDelay](#withmaxretrydelay)
- [WithRetryDelayMultiple](#withretrydelaymultiple)
- [WithHTTPClient](#withhttpclient)
- [WithRetryableChecker](#withretryablechecker)
- [WithJitter](#withjitter)
- [WithRespectRetryAfter](#withrespectretryafter)
- [WithPerAttemptTimeout](#withperattempttimeout)
- [WithOnRetry](#withonretry)
- [Request Options](#request-options)

## WithMaxRetries

Sets the maximum number of retry attempts.

```go
client, err := retry.NewClient(retry.WithMaxRetries(5))
if err != nil {
    log.Fatal(err)
}
```

## WithInitialRetryDelay

Sets the initial delay before the first retry.

```go
client, err := retry.NewClient(retry.WithInitialRetryDelay(500*time.Millisecond))
if err != nil {
    log.Fatal(err)
}
```

## WithMaxRetryDelay

Sets the maximum delay between retries (caps exponential backoff).

```go
client, err := retry.NewClient(retry.WithMaxRetryDelay(30*time.Second))
if err != nil {
    log.Fatal(err)
}
```

## WithRetryDelayMultiple

Sets the exponential backoff multiplier.

```go
client, err := retry.NewClient(retry.WithRetryDelayMultiple(3.0))
if err != nil {
    log.Fatal(err)
}
```

## WithHTTPClient

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

### Custom TLS Configuration

For services requiring custom TLS certificates (e.g., self-signed certificates, internal CAs), configure the TLS settings on your `http.Client` before passing it to the retry client.

**Example: Custom CA Certificate**

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

**Example: Skip TLS Verification (Testing Only)**

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

## WithRetryableChecker

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

### Default Retry Behavior

The `DefaultRetryableChecker` retries in the following cases:

- **Network errors**: Connection refused, timeouts, DNS errors, etc.
- **5xx Server Errors**: 500, 502, 503, 504, etc.
- **429 Too Many Requests**: Rate limiting errors

It does **NOT** retry:

- **4xx Client Errors** (except 429): 400, 401, 403, 404, etc.
- **2xx Success**: 200, 201, 204, etc.
- **3xx Redirects**: 301, 302, 307, etc.

## WithJitter

Controls random jitter to prevent thundering herd problem. **Jitter is enabled by default.** When enabled, retry delays will be randomized by ±25% to avoid synchronized retries from multiple clients.

```go
// Jitter is enabled by default, but you can explicitly disable it if needed
client, err := retry.NewClient(
    retry.WithJitter(false), // Disable jitter for predictable delays
    retry.WithMaxRetries(3),
)
```

**Use Case**: When multiple clients might fail simultaneously (e.g., during a service outage), jitter prevents them from retrying at the exact same time, reducing load spikes on the recovering service. This is the recommended behavior for most production use cases.

## WithRespectRetryAfter

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

## WithPerAttemptTimeout

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
// Result: Each attempt is limited to at most 5 seconds.
// Note: With exponential backoff between retries, the actual number of
//       attempts that fit within the 30s overall timeout depends on the
//       configured backoff delays.
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

client, _ := retry.NewClient(
    retry.WithPerAttemptTimeout(5*time.Second),
    retry.WithMaxRetries(5),
)
```

## WithOnRetry

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

## Request Options

The convenience methods (Get, Post, Put, Patch, Delete, Head) support optional request configuration through `RequestOption` functions:

### WithBody

Sets the request body and optionally the Content-Type header. If `contentType` is empty, no Content-Type header will be set.

```go
// POST request with JSON body
jsonData := bytes.NewReader([]byte(`{"username":"user","password":"pass"}`))
resp, err := client.Post(ctx, "https://api.example.com/login",
    retry.WithBody("application/json", jsonData))

// POST request with body but no Content-Type
resp, err := client.Post(ctx, "https://api.example.com/data",
    retry.WithBody("", strings.NewReader("plain text data")))
```

### WithHeader

Sets a single header on the request.

```go
resp, err := client.Get(ctx, "https://api.example.com/protected",
    retry.WithHeader("Authorization", "Bearer your-token-here"))
```

### WithHeaders

Sets multiple headers on the request.

```go
resp, err := client.Post(ctx, "https://api.example.com/api",
    retry.WithBody("application/json", jsonData),
    retry.WithHeaders(map[string]string{
        "Authorization": "Bearer token",
        "X-Request-ID": "req-12345",
        "X-API-Version": "v2",
    }))
```

### Combining Multiple Options

Request options can be combined to configure complex requests:

```go
resp, err := client.Post(ctx, "https://api.example.com/graphql",
    retry.WithBody("application/json", graphqlQuery),
    retry.WithHeader("Authorization", "Bearer token"),
    retry.WithHeader("X-Request-ID", requestID),
    retry.WithHeader("Content-Encoding", "gzip"))
```

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

The client respects context cancellation and timeouts. There are two ways to pass context:

**Option 1: Use request's context (recommended)**

```go
// Overall timeout for the entire operation (including retries)
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.Do(req)
if err != nil {
    // May be context.DeadlineExceeded
    log.Printf("Request failed: %v", err)
}
```

**Option 2: Use DoWithContext for explicit context**

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
resp, err := client.DoWithContext(ctx, req)
if err != nil {
    log.Printf("Request failed: %v", err)
}
```
