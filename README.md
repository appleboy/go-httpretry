# go-httpretry

[![Trivy Security Scan](https://github.com/appleboy/go-httpretry/actions/workflows/security.yml/badge.svg)](https://github.com/appleboy/go-httpretry/actions/workflows/security.yml)
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
    - [Using Convenience Methods](#using-convenience-methods)
    - [JSON Requests Made Easy](#json-requests-made-easy)
    - [Custom Configuration](#custom-configuration)
    - [Using Preset Configurations](#using-preset-configurations)
  - [Documentation](#documentation)
    - [Key Topics](#key-topics)
      - [Exponential Backoff](#exponential-backoff)
      - [Default Retry Behavior](#default-retry-behavior)
      - [Context Support](#context-support)
    - [Complete Working Examples](#complete-working-examples)
    - [⚠️ Important: Large File Uploads](#️-important-large-file-uploads)
  - [Testing](#testing)
  - [Design Principles](#design-principles)
  - [License](#license)
  - [Author](#author)

## Features

- **Automatic Retries**: Retries failed requests with configurable exponential backoff
- **Smart Retry Logic**: Default retries on network errors, 5xx server errors, and 429 (Too Many Requests)
- **Preset Configurations**: Ready-to-use presets for common scenarios (realtime, background, rate-limited, microservice, webhook, critical, fast-fail, etc.)
- **Structured Error Types**: Rich error information with `RetryError` for programmatic error inspection
- **Convenience Methods**: Simple HTTP methods (Get, Post, Put, Patch, Delete, Head) with optional request configuration
- **Request Options**: Flexible request configuration with `WithBody()`, `WithJSON()`, `WithHeader()`, and `WithHeaders()`
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

    // Simple GET request
    resp, err := client.Get(context.Background(), "https://api.example.com/data")
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
}
```

### Using Convenience Methods

```go
// GET request
resp, err := client.Get(ctx, "https://api.example.com/users")

// POST request with JSON body (automatic marshaling)
type User struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}
user := User{Name: "John", Email: "john@example.com"}
resp, err := client.Post(ctx, "https://api.example.com/users",
    retry.WithJSON(user))

// POST request with raw JSON body
jsonData := bytes.NewReader([]byte(`{"name":"John"}`))
resp, err := client.Post(ctx, "https://api.example.com/users",
    retry.WithBody("application/json", jsonData))

// PUT request with custom headers
resp, err := client.Put(ctx, "https://api.example.com/users/123",
    retry.WithJSON(user),
    retry.WithHeader("Authorization", "Bearer token"))

// DELETE request
resp, err := client.Delete(ctx, "https://api.example.com/users/123")
```

### JSON Requests Made Easy

The `WithJSON()` helper automatically marshals your data to JSON:

```go
type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
    Age   int    `json:"age"`
}

user := CreateUserRequest{
    Name:  "John Doe",
    Email: "john@example.com",
    Age:   30,
}

// Automatically marshals to JSON and sets Content-Type header
resp, err := client.Post(ctx, "https://api.example.com/users",
    retry.WithJSON(user))
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

### Using Preset Configurations

The library provides optimized presets for common scenarios:

```go
// Realtime client - Fast response times for user-facing requests
client, err := retry.NewRealtimeClient()

// Background client - Reliable background task processing
client, err := retry.NewBackgroundClient()

// Rate-limited client - Respects API rate limits
client, err := retry.NewRateLimitedClient()

// Microservice client - Internal service communication
client, err := retry.NewMicroserviceClient()

// Critical client - Mission-critical operations (payments, etc.)
client, err := retry.NewCriticalClient()

// Fast-fail client - Health checks and service discovery
client, err := retry.NewFastFailClient()
```

All presets can be customized by passing additional options:

```go
// Start with realtime preset but use more retries
client, err := retry.NewRealtimeClient(
    retry.WithMaxRetries(5), // Override preset default
)
```

## Documentation

For detailed documentation, please refer to:

- **[Preset Configurations](docs/PRESETS.md)** - Pre-configured clients for common scenarios (realtime, background, rate-limited, microservice, webhook, critical, fast-fail, etc.)
- **[Configuration Options](docs/CONFIGURATION.md)** - All available configuration options including retry behavior, HTTP client settings, custom TLS, and request options
- **[Error Handling](docs/ERROR_HANDLING.md)** - Structured error handling with `RetryError` and response inspection
- **[Examples](docs/EXAMPLES.md)** - Detailed usage examples for various scenarios

### Key Topics

#### Exponential Backoff

Retries use exponential backoff to avoid overwhelming the server:

1. **First retry**: Wait `initialRetryDelay` (default: 1s)
2. **Second retry**: Wait `initialRetryDelay * multiplier` (default: 2s)
3. **Third retry**: Wait `initialRetryDelay * multiplier²` (default: 4s)
4. **Subsequent retries**: Continue multiplying until `maxRetryDelay` is reached

#### Default Retry Behavior

The `DefaultRetryableChecker` retries in the following cases:

- **Network errors**: Connection refused, timeouts, DNS errors, etc.
- **5xx Server Errors**: 500, 502, 503, 504, etc.
- **429 Too Many Requests**: Rate limiting errors

It does **NOT** retry:

- **4xx Client Errors** (except 429): 400, 401, 403, 404, etc.
- **2xx Success**: 200, 201, 204, etc.
- **3xx Redirects**: 301, 302, 307, etc.

#### Context Support

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

### Complete Working Examples

For complete, runnable examples, see:

- [\_example/basic](_example/basic) - Basic usage with default settings
- [\_example/advanced](_example/advanced) - Advanced configuration with observability and custom retry logic
- [\_example/convenience_methods](_example/convenience_methods) - Using convenience HTTP methods (GET, POST, PUT, DELETE, HEAD, PATCH)
- [\_example/request_options](_example/request_options) - Request options usage (WithBody, WithJSON, WithHeader, WithHeaders)
- [\_example/large_file_upload](_example/large_file_upload) - ⚠️ **Important**: Correct way to upload large files (>10MB) with retry support
- `example_test.go` - Additional examples and test cases

Each example can be run independently:

```bash
cd _example/basic && go run main.go
cd _example/advanced && go run main.go
cd _example/convenience_methods && go run main.go
cd _example/request_options && go run main.go
cd _example/large_file_upload && go run main.go
```

### ⚠️ Important: Large File Uploads

**Do NOT use `WithBody()` or `WithJSON()` for files larger than 10MB.** These functions buffer the entire body in memory to support retries.

For large files, use the `Do()` method with a custom `GetBody` function:

```go
// ✅ CORRECT: Upload large files with retry support
file, _ := os.Open("large-file.dat")
req, _ := http.NewRequestWithContext(ctx, "POST", url, file)

// CRITICAL: Set GetBody to reopen the file for each retry
req.GetBody = func() (io.ReadCloser, error) {
    return os.Open("large-file.dat")
}

resp, err := client.Do(req)
```

**Size Guidelines:**

- ✅ **<1MB**: Safe to use `WithBody()` or `WithJSON()`
- ⚠️ **1-10MB**: Use with caution, monitor memory usage
- ❌ **>10MB**: Use `Do()` with `GetBody` (see [large_file_upload example](_example/large_file_upload))

For complete patterns and best practices, see the [large_file_upload example](_example/large_file_upload) with detailed explanations.

## Testing

Run the test suite:

```bash
go test -v ./...
```

With coverage:

```bash
go test -v -cover ./...
```

Or use the Makefile:

```bash
make test
make lint
```

## Design Principles

- **Functional Options Pattern**: Provides clean, flexible API for both client configuration and request options
- **Sensible Defaults**: Works out of the box for most use cases
- **Convenience Methods**: Simple HTTP methods (Get, Post, Put, Patch, Delete, Head) with optional configuration through RequestOption functions
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
