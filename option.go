package retry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// OnRetryFunc is called before each retry attempt
type OnRetryFunc func(info RetryInfo)

// Option configures a Client
type Option func(*Client)

// WithMaxRetries sets the maximum number of retry attempts
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithInitialRetryDelay sets the initial delay before the first retry
func WithInitialRetryDelay(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.initialRetryDelay = d
		}
	}
}

// WithMaxRetryDelay sets the maximum delay between retries
func WithMaxRetryDelay(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.maxRetryDelay = d
		}
	}
}

// WithRetryDelayMultiple sets the exponential backoff multiplier
func WithRetryDelayMultiple(multiplier float64) Option {
	return func(c *Client) {
		if multiplier > 1.0 {
			c.retryDelayMultiple = multiplier
		}
	}
}

// WithHTTPClient sets a custom http.Client
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithRetryableChecker sets a custom function to determine retryable errors
func WithRetryableChecker(checker RetryableChecker) Option {
	return func(c *Client) {
		if checker != nil {
			c.retryableChecker = checker
		}
	}
}

// WithJitter enables random jitter to prevent thundering herd problem.
// When enabled, retry delays will be randomized by ±25% to avoid synchronized retries
// from multiple clients hitting the server at the same time.
func WithJitter(enabled bool) Option {
	return func(c *Client) {
		c.jitterEnabled = enabled
	}
}

// WithOnRetry sets a callback function that will be called before each retry attempt.
// This is useful for logging, metrics collection, or custom retry logic.
func WithOnRetry(fn OnRetryFunc) Option {
	return func(c *Client) {
		c.onRetryFunc = fn
	}
}

// WithRespectRetryAfter enables respecting the Retry-After header from HTTP responses.
// When enabled, the client will use the server-provided retry delay instead of
// the exponential backoff delay. This is useful for rate limiting scenarios.
// The Retry-After header can be either a number of seconds or an HTTP-date.
func WithRespectRetryAfter(enabled bool) Option {
	return func(c *Client) {
		c.respectRetryAfter = enabled
	}
}

// WithPerAttemptTimeout sets a timeout for each individual retry attempt.
// This prevents a single slow request from consuming all available retry time.
// If set to 0 (default), no per-attempt timeout is applied.
// The per-attempt timeout is independent of the overall context timeout.
func WithPerAttemptTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.perAttemptTimeout = d
		}
	}
}

// WithMetrics sets the metrics collector for observability.
// The collector will receive metrics events for each request attempt, retry, and completion.
// If nil is provided, metrics collection will be disabled (no-op).
func WithMetrics(collector MetricsCollector) Option {
	return func(c *Client) {
		if collector != nil {
			c.metrics = collector
		}
	}
}

// WithTracer sets the distributed tracer for observability.
// The tracer will create spans for each request and attempt, providing distributed tracing support.
// If nil is provided, tracing will be disabled (no-op).
func WithTracer(tracer Tracer) Option {
	return func(c *Client) {
		if tracer != nil {
			c.tracer = tracer
		}
	}
}

// WithLogger sets the structured logger for observability.
// The logger will output structured logs for request lifecycle events.
// By default, the client uses slog.Default() which outputs to stderr at INFO level.
// Use WithNoLogging() to disable logging entirely.
func WithLogger(logger Logger) Option {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithNoLogging disables all logging output.
// By default, the client uses slog.Default() which outputs to stderr.
// Use this option if you want to suppress all log messages.
func WithNoLogging() Option {
	return func(c *Client) {
		c.logger = nopLogger{}
	}
}

// RequestOption is a function that configures an HTTP request
type RequestOption func(*http.Request)

// WithBody sets the request body and optionally the Content-Type header.
// If contentType is empty, no Content-Type header will be set.
//
// ⚠️ MEMORY USAGE WARNING:
// To support retries, this function buffers the ENTIRE body in memory using io.ReadAll.
// This is ideal for small payloads like JSON/XML API requests (typically <1MB), but
// NOT suitable for large files or streaming data.
//
// Size Guidelines:
//   - ✅ Small payloads (<1MB):   Safe to use WithBody
//   - ⚠️ Medium payloads (1-10MB): Use with caution, consider memory constraints
//   - ❌ Large payloads (>10MB):   DO NOT use WithBody, use Do() with GetBody instead
//
// For large files or streaming data, use the Do() method directly with a custom
// GetBody function that can reopen the file/stream for each retry attempt.
// See the large_file_upload example for proper implementation.
//
// Example (small JSON payload):
//
//	jsonData := []byte(`{"user":"john"}`)
//	resp, err := client.Post(ctx, url,
//	    retry.WithBody("application/json", bytes.NewReader(jsonData)))
//
// For large files, see Do() method and the large_file_upload example instead.
func WithBody(contentType string, body io.Reader) RequestOption {
	return func(req *http.Request) {
		// Buffer the entire body to support retries.
		// http.Request.Clone() uses GetBody to get a fresh reader for each retry.
		// Without this, retries would send an empty body because io.Reader is consumed.
		data, err := io.ReadAll(body)
		if err != nil {
			// If reading fails, set the body as-is (best effort).
			// The request will fail when actually executed.
			req.Body = io.NopCloser(body)
			if contentType != "" {
				req.Header.Set("Content-Type", contentType)
			}
			return
		}

		// Set both Body and GetBody to support request retries
		req.Body = io.NopCloser(bytes.NewReader(data))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
		req.ContentLength = int64(len(data))

		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	}
}

// WithJSON serializes the given value to JSON and sets it as the request body.
// It automatically sets the Content-Type header to "application/json".
//
// ⚠️ MEMORY USAGE WARNING:
// Like WithBody, this function buffers the entire JSON payload in memory to support
// retries. This is ideal for typical API requests with small to medium JSON objects,
// but NOT suitable for very large JSON documents.
//
// Size Guidelines:
//   - ✅ Typical API payloads:  Safe to use (most API requests are <100KB)
//   - ⚠️ Large JSON (1-10MB):   Use with caution
//   - ❌ Very large JSON (>10MB): Use Do() with custom GetBody instead
//
// If JSON marshaling fails, the request will fail when executed with an error.
//
// Example (typical API request):
//
//	type User struct {
//	    Name  string `json:"name"`
//	    Email string `json:"email"`
//	}
//	user := User{Name: "John", Email: "john@example.com"}
//	resp, err := client.Post(ctx, url, retry.WithJSON(user))
//
// For large JSON documents, consider streaming or use Do() with custom GetBody.
func WithJSON(v any) RequestOption {
	return func(req *http.Request) {
		data, err := json.Marshal(v)
		if err != nil {
			// Set an error body that will fail when the request is executed.
			// We can't return an error from RequestOption, so we defer the error
			// to request execution time. Using a reader that returns the error
			// ensures the request will fail immediately when trying to read the body.
			req.Body = io.NopCloser(&errorReader{err: err})
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(&errorReader{err: err}), nil
			}
			req.Header.Set("Content-Type", "application/json")
			return
		}

		// Set both Body and GetBody to support request retries
		req.Body = io.NopCloser(bytes.NewReader(data))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
		req.ContentLength = int64(len(data))
		req.Header.Set("Content-Type", "application/json")
	}
}

// errorReader is an io.Reader that always returns an error.
// Used to defer JSON marshaling errors to request execution time.
type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}

// WithHeader sets a header key-value pair on the request.
func WithHeader(key, value string) RequestOption {
	return func(req *http.Request) {
		req.Header.Set(key, value)
	}
}

// WithHeaders sets multiple headers on the request.
func WithHeaders(headers map[string]string) RequestOption {
	return func(req *http.Request) {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
}
