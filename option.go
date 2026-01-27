package retry

import (
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
