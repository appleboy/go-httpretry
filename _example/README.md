# go-httpretry Examples

This directory contains various examples demonstrating how to use the `go-httpretry` library.

## Examples

### 1. Basic Usage

**Directory:** `basic/`

Demonstrates the simplest way to use the library with default settings.

```bash
cd basic
go run main.go
```

**Features:**

- Default retry configuration
- Simple GET request

---

### 2. Convenience Methods

**Directory:** `convenience_methods/`

Shows all available convenience methods (Get, Post, Put, Patch, Delete, Head) with various configurations.

```bash
cd convenience_methods
go run main.go
```

**Features:**

- GET, POST, PUT, PATCH, DELETE, HEAD requests
- JSON body handling
- Custom headers
- Error handling

---

### 3. Request Options

**Directory:** `request_options/`

Demonstrates the flexible RequestOption pattern for configuring requests.

```bash
cd request_options
go run main.go
```

**Features:**

- `WithBody()` - Set request body with optional Content-Type
- `WithHeader()` - Add single header
- `WithHeaders()` - Add multiple headers
- Combining multiple options

---

### 4. Advanced Configuration

**Directory:** `advanced/`

Production-ready example with full configuration including observability and custom retry logic.

```bash
cd advanced
go run main.go
```

**Features:**

- Custom retry configuration
- Observability hooks (logging retries)
- Per-attempt timeout
- Custom retry checker
- Context timeout
- Rate limiting with Retry-After
- Handling flaky endpoints

---

## Running All Examples

To run all examples at once:

```bash
# From the _example directory
for dir in basic convenience_methods request_options advanced; do
    echo "=== Running $dir example ==="
    (cd $dir && go run main.go)
    echo ""
done
```

## Notes

- All examples use `https://httpbin.org` as the test endpoint
- Examples include error handling and proper resource cleanup
- The advanced example demonstrates production-ready configuration
- Some examples may show retry attempts in action when endpoints return errors

## Learning Path

1. Start with **basic/** to understand the core concept
2. Move to **convenience_methods/** to learn simple HTTP methods
3. Explore **request_options/** for flexible request configuration
4. Study **advanced/** for production-ready patterns

## Requirements

- Go 1.24 or higher
- Internet connection (to access httpbin.org)

## More Information

See the main [README.md](../README.md) for complete documentation.
