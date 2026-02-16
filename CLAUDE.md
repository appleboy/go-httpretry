# CLAUDE.md

Guidance for Claude Code when working with this repository.

## Project Overview

`go-httpretry` is a Go library providing HTTP client with automatic retry logic using exponential backoff. Built with the Functional Options Pattern and zero external dependencies.

## Important Constraints

**These constraints MUST be followed:**

1. **Zero dependencies** - Only use Go standard library
2. **Testing is mandatory** - All changes MUST pass `make test && make lint` before committing
3. **Test coverage required** - New features need tests for happy path, error cases, and edge cases
4. **Request body handling** - Always clone requests before retry (bodies may be consumed)
5. **Resource safety** - Close response bodies before retrying to prevent leaks
6. **Context respect** - Never ignore context cancellation in the retry loop
7. **No breaking changes** - The public API surface is small and stable
8. **Avoid if-else nesting** - Prefer early returns and guard clauses:

   ```go
   // Bad: nested if-else
   if condition {
       // long logic
   } else {
       return err
   }

   // Good: early return
   if !condition {
       return err
   }
   // long logic
   ```

   Use switch statements or lookup tables for multiple conditions instead of long if-else chains.

## Architecture

**Core File:** `retry.go` - Single-file implementation

**Main Components:**

- `Client` struct: HTTP client wrapper with retry configuration
- `RetryableChecker`: Function type determining if error/response should retry
- `Do(ctx, req)`: Main entry point executing requests with exponential backoff

**Design Patterns:**

- Functional Options Pattern for all configuration
- Request cloning for each retry
- Explicit response body closure before retries
- Context-aware retry loop
- Per-attempt timeout (optional) to prevent slow requests exhausting retry budget

**Default Configuration:**

- Max retries: 3
- Initial delay: 1s, Max delay: 10s, Multiplier: 2.0x
- Jitter: Enabled (±25%)
- Retry-After: Enabled (respects RFC 7231)
- Per-attempt timeout: Disabled (0)
- Retry checker: `DefaultRetryableChecker` (network errors, 5xx, 429)

**Per-Attempt Timeout:**

- `WithPerAttemptTimeout(duration)`: Sets timeout per individual attempt
- Prevents slow requests from consuming all retry opportunities
- Uses `context.WithTimeout()` for each attempt
- When disabled (default), only overall context timeout applies

## Development Workflow

**Required before commit:**

```bash
make test && make lint
```

**Common commands:**

```bash
make test              # Run tests with coverage
make lint              # Run golangci-lint
make fmt               # Format code
go test -v -run Name   # Run specific test
```

**Test Coverage Requirements:**

- Every code change needs corresponding tests
- Cover happy path, error cases, edge cases
- Bug fixes need regression tests
