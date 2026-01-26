# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Table of Contents

- [CLAUDE.md](#claudemd)
  - [Table of Contents](#table-of-contents)
  - [Project Overview](#project-overview)
  - [Architecture](#architecture)
    - [Core Components](#core-components)
    - [Defaults](#defaults)
    - [Exponential Backoff](#exponential-backoff)
  - [Testing](#testing)
  - [Development Commands](#development-commands)
  - [CI/CD](#cicd)
  - [Important Constraints](#important-constraints)

## Project Overview

`go-httpretry` is a Go library providing an HTTP client with automatic retry logic using exponential backoff. It's built using the Functional Options Pattern and has zero external dependencies beyond the Go standard library.

## Architecture

### Core Components

**retry.go** - Single-file implementation containing:

- `Client` struct: HTTP client wrapper with retry configuration
- `RetryableChecker` function type: Determines if an error/response should trigger a retry
- Functional options pattern for configuration (`WithMaxRetries`, `WithInitialRetryDelay`, etc.)
- `Do(ctx, req)` method: Main entry point that executes requests with exponential backoff

**Key Design Patterns:**

- **Functional Options Pattern**: All client configuration uses `Option` functions
- **Request Cloning**: Each retry clones the request to handle consumed request bodies
- **Resource Management**: Response bodies are explicitly closed before retries to prevent leaks
- **Context-Aware**: Respects context cancellation/timeouts throughout retry loop

### Defaults

The library ships with sensible defaults (see constants in retry.go:17-22 and NewClient in retry.go:136-146):

- Max retries: 3
- Initial delay: 1 second
- Max delay: 10 seconds
- Backoff multiplier: 2.0x
- Jitter: Enabled (±25% randomization to prevent thundering herd)
- Retry-After: Enabled (respects HTTP Retry-After header per RFC 7231)
- Retry checker: `DefaultRetryableChecker` (retries on network errors, 5xx, and 429)

### Exponential Backoff

Implemented in the main retry loop (retry.go:124-174):

1. First attempt is immediate
2. Each retry waits: `delay = delay * multiplier` (capped at `maxRetryDelay`)
3. Context cancellation is checked during wait periods

## Testing

**IMPORTANT: All changes MUST pass both `make test` and `make lint` before committing.**

**Run all tests (recommended):**

```bash
make test
```

This runs tests with coverage and generates `coverage.txt`.

**Run linting:**

```bash
make lint
```

**Run both (required before commit):**

```bash
make test && make lint
```

**Run tests manually:**

```bash
go test -v ./...
```

**Run tests with coverage:**

```bash
go test -v -cover ./...
```

**Run specific test:**

```bash
go test -v -run TestName
```

### Test Coverage Requirements

- **Every code change MUST include corresponding tests**
- **New functions require test cases** covering:
  - Happy path (normal operation)
  - Error cases (invalid inputs, network failures)
  - Edge cases (nil values, timeouts, context cancellation)
- **Bug fixes MUST include a regression test** that reproduces the bug before the fix

## Development Commands

**Linting:**

```bash
make lint
```

Or run golangci-lint directly:

```bash
golangci-lint run --verbose
```

**Format code:**

```bash
make fmt
```

**Clean build artifacts:**

```bash
rm -rf coverage.txt
```

**No build required** - this is a library package, not a binary.

## CI/CD

- **testing.yml**: Runs lints and tests on Go 1.24-1.25 across Ubuntu and macOS
- **goreleaser.yml**: Publishes releases on git tags
- **security.yml**: Daily Trivy security scans
- **codeql.yml**: Static analysis on push/PR

## Important Constraints

1. **Zero dependencies** - Only use Go standard library
2. **No breaking changes to functional options** - The public API surface is small and stable
3. **Request body handling** - Always clone requests before retry (request bodies may be consumed)
4. **Resource safety** - Close response bodies before retrying to prevent leaks
5. **Context respect** - Never ignore context cancellation in the retry loop
6. **Testing is mandatory** - All changes MUST:
   - Pass `make test` and `make lint` before committing
   - Include corresponding test cases for new functionality
   - Include regression tests for bug fixes
   - Maintain or improve test coverage
