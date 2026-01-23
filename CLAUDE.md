# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

The library ships with sensible defaults (see constants in retry.go:11-16):

- Max retries: 3
- Initial delay: 1 second
- Max delay: 10 seconds
- Backoff multiplier: 2.0x
- Retry checker: `DefaultRetryableChecker` (retries on network errors, 5xx, and 429)

### Exponential Backoff

Implemented in the main retry loop (retry.go:124-174):

1. First attempt is immediate
2. Each retry waits: `delay = delay * multiplier` (capped at `maxRetryDelay`)
3. Context cancellation is checked during wait periods

## Testing

**Run all tests:**

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

**Note:** The CI workflow (.github/workflows/testing.yml:74) references `make test`, but there is currently no Makefile in the repository. Tests are expected to run via standard `go test` commands.

## Development Commands

**Linting:**
The project uses golangci-lint in CI. Run locally:

```bash
golangci-lint run --verbose
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
