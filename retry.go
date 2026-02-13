package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Default retry configuration
const (
	defaultMaxRetries         = 3
	defaultInitialRetryDelay  = 1 * time.Second
	defaultMaxRetryDelay      = 10 * time.Second
	defaultRetryDelayMultiple = 2.0
)

// Client is an HTTP client with automatic retry logic using exponential backoff
type Client struct {
	maxRetries         int
	initialRetryDelay  time.Duration
	maxRetryDelay      time.Duration
	retryDelayMultiple float64
	httpClient         *http.Client
	retryableChecker   RetryableChecker
	jitterEnabled      bool // Add random jitter to retry delays
	onRetryFunc        OnRetryFunc
	respectRetryAfter  bool          // Respect Retry-After header from responses
	perAttemptTimeout  time.Duration // Timeout for each individual attempt (0 = no per-attempt timeout)
	err                error
}

// RetryableChecker determines if an error or response should trigger a retry
type RetryableChecker func(err error, resp *http.Response) bool

// RetryInfo contains information about a retry attempt
type RetryInfo struct {
	Attempt      int           // Current attempt number (1-indexed)
	Delay        time.Duration // Delay before this retry
	Err          error         // Error that triggered the retry (nil if retrying due to response status)
	StatusCode   int           // HTTP status code (0 if request failed)
	RetryAfter   time.Duration // Retry-After duration from response header (0 if not present)
	TotalElapsed time.Duration // Total time elapsed since first attempt
}

// RetryError is returned when all retry attempts have been exhausted.
// It provides detailed information about the retry attempts and the final failure.
type RetryError struct {
	Attempts   int           // Total number of attempts made (initial + retries)
	LastErr    error         // The last error that occurred (nil if last attempt had non-retryable status)
	LastStatus int           // HTTP status code from the last attempt (0 if request failed)
	Elapsed    time.Duration // Total time elapsed from first attempt to final failure
}

// Error implements the error interface
func (e *RetryError) Error() string {
	if e.LastErr != nil {
		return fmt.Sprintf(
			"request failed after %d attempts (elapsed: %v): %v",
			e.Attempts,
			e.Elapsed,
			e.LastErr,
		)
	}
	return fmt.Sprintf(
		"request failed after %d attempts (elapsed: %v): HTTP %d",
		e.Attempts,
		e.Elapsed,
		e.LastStatus,
	)
}

// Unwrap returns the underlying error for error unwrapping
func (e *RetryError) Unwrap() error {
	return e.LastErr
}

// NewClient creates a new retry-enabled HTTP client with the given options.
// Returns an error if any option encounters an error.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		maxRetries:         defaultMaxRetries,
		initialRetryDelay:  defaultInitialRetryDelay,
		maxRetryDelay:      defaultMaxRetryDelay,
		retryDelayMultiple: defaultRetryDelayMultiple,
		httpClient:         http.DefaultClient,
		retryableChecker:   DefaultRetryableChecker,
		jitterEnabled:      true, // Enable jitter by default to prevent thundering herd
		respectRetryAfter:  true, // Respect HTTP standard Retry-After header by default
	}

	for _, opt := range opts {
		opt(c)
	}

	// Check if any option returned an error
	if c.err != nil {
		return nil, c.err
	}

	return c, nil
}

// DefaultRetryableChecker is the default implementation for determining retryable errors
// It retries on network errors and 5xx/429 status codes
func DefaultRetryableChecker(err error, resp *http.Response) bool {
	if err != nil {
		// Network errors, timeouts, connection errors are retryable
		return true
	}

	if resp == nil {
		return false
	}

	// Retry on 5xx server errors and 429 Too Many Requests
	statusCode := resp.StatusCode
	return statusCode >= 500 || statusCode == http.StatusTooManyRequests
}

// parseRetryAfter parses the Retry-After header and returns the duration to wait.
// The Retry-After header can be either a number of seconds or an HTTP-date.
// Returns 0 if the header is not present or cannot be parsed.
func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}

	// Try parsing as seconds (integer)
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date (RFC1123, RFC850, or ANSI C asctime format)
	if t, err := http.ParseTime(retryAfter); err == nil {
		duration := time.Until(t)
		if duration > 0 {
			return duration
		}
	}

	return 0
}

// applyJitter adds random jitter to the delay (±25% of the original value)
func applyJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	// Add jitter: delay * (0.75 + random[0, 0.5])
	// #nosec G404 - Cryptographic randomness not required for jitter
	jitter := 0.75 + rand.Float64()*0.5
	return time.Duration(float64(delay) * jitter)
}

// cancelOnCloseBody wraps an io.ReadCloser and calls a cancel function when Close() is called.
// This ensures the per-attempt context timeout is released when the response body is closed.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnCloseBody) Close() error {
	err := c.ReadCloser.Close()
	if c.cancel != nil {
		c.cancel()
	}
	return err
}

// Do executes an HTTP request with automatic retry logic using exponential backoff.
// This method is compatible with the standard http.Client interface.
// The context is taken from the request via req.Context().
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("retry: nil Request")
	}
	return c.DoWithContext(req.Context(), req)
}

// DoWithContext executes an HTTP request with automatic retry logic using exponential backoff.
// Use this when you need explicit control over the context separate from the request.
func (c *Client) DoWithContext(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("retry: nil Request")
	}
	var lastErr error
	var resp *http.Response
	delay := c.initialRetryDelay
	startTime := time.Now()

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Check for Retry-After header if enabled
			var retryAfterDelay time.Duration
			if c.respectRetryAfter && resp != nil {
				retryAfterDelay = parseRetryAfter(resp)
			}

			// Use Retry-After if available, otherwise use exponential backoff
			actualDelay := delay
			if retryAfterDelay > 0 {
				actualDelay = retryAfterDelay
			}

			// Apply jitter if enabled
			if c.jitterEnabled {
				actualDelay = applyJitter(actualDelay)
			}

			// Cap the delay at maxRetryDelay
			if actualDelay > c.maxRetryDelay {
				actualDelay = c.maxRetryDelay
			}

			// Call onRetry callback if set
			if c.onRetryFunc != nil {
				statusCode := 0
				if resp != nil {
					statusCode = resp.StatusCode
				}
				c.onRetryFunc(RetryInfo{
					Attempt:      attempt,
					Delay:        actualDelay,
					Err:          lastErr,
					StatusCode:   statusCode,
					RetryAfter:   retryAfterDelay,
					TotalElapsed: time.Since(startTime),
				})
			}

			// Wait before retry
			select {
			case <-ctx.Done():
				// Context cancelled - return RetryError with information about the attempts made
				statusCode := 0
				if resp != nil {
					statusCode = resp.StatusCode
				}
				return nil, &RetryError{
					Attempts:   attempt,
					LastErr:    ctx.Err(),
					LastStatus: statusCode,
					Elapsed:    time.Since(startTime),
				}
			case <-time.After(actualDelay):
				// Calculate next delay with exponential backoff (for next iteration)
				delay = time.Duration(float64(delay) * c.retryDelayMultiple)
				if delay > c.maxRetryDelay {
					delay = c.maxRetryDelay
				}
			}
		}

		// Create a per-attempt context with timeout if configured
		attemptCtx := ctx
		var cancelAttempt context.CancelFunc
		if c.perAttemptTimeout > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, c.perAttemptTimeout)
		}

		// Clone the request for retry (important: body might be consumed)
		reqClone := req.Clone(attemptCtx)

		resp, lastErr = c.httpClient.Do(reqClone)

		// Check if we should retry
		if !c.retryableChecker(lastErr, resp) {
			// Success or non-retryable error
			// Wrap the response body to cancel the per-attempt context when the body is closed
			if cancelAttempt != nil && resp != nil && resp.Body != nil {
				resp.Body = &cancelOnCloseBody{
					ReadCloser: resp.Body,
					cancel:     cancelAttempt,
				}
			} else if cancelAttempt != nil {
				// No body to wrap, cancel immediately
				cancelAttempt()
			}
			return resp, lastErr
		}

		// Check if this is the last attempt
		isLastAttempt := attempt == c.maxRetries

		// Going to retry or exhausted retries
		if !isLastAttempt {
			// Not the last attempt: close response body and cancel per-attempt context
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if cancelAttempt != nil {
				cancelAttempt()
			}
		} else {
			// Last attempt: keep response body open but cancel per-attempt context
			// Wrap the response body to cancel the per-attempt context when the body is closed
			if cancelAttempt != nil && resp != nil && resp.Body != nil {
				resp.Body = &cancelOnCloseBody{
					ReadCloser: resp.Body,
					cancel:     cancelAttempt,
				}
			} else if cancelAttempt != nil {
				// No body to wrap, cancel immediately
				cancelAttempt()
			}
		}
	}

	// All retries exhausted - return RetryError with detailed information
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	return resp, &RetryError{
		Attempts:   c.maxRetries + 1, // +1 because attempts include the initial request
		LastErr:    lastErr,
		LastStatus: statusCode,
		Elapsed:    time.Since(startTime),
	}
}

// doRequest is a helper method that creates and executes an HTTP request with retry logic.
// It handles the common pattern of creating a request, applying options, and executing it.
func (c *Client) doRequest(
	ctx context.Context,
	method string,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return c.DoWithContext(ctx, req)
}

// Get is a convenience method for making GET requests with retry logic.
// It creates a GET request for the specified URL and executes it with the configured retry behavior.
func (c *Client) Get(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodGet, url, opts...)
}

// Head is a convenience method for making HEAD requests with retry logic.
// It creates a HEAD request for the specified URL and executes it with the configured retry behavior.
func (c *Client) Head(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodHead, url, opts...)
}

// Post is a convenience method for making POST requests with retry logic.
// It creates a POST request with the specified URL and executes it with the configured retry behavior.
// Use WithBody() to add a request body and content type.
func (c *Client) Post(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPost, url, opts...)
}

// Put is a convenience method for making PUT requests with retry logic.
// It creates a PUT request with the specified URL and executes it with the configured retry behavior.
// Use WithBody() to add a request body and content type.
func (c *Client) Put(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPut, url, opts...)
}

// Patch is a convenience method for making PATCH requests with retry logic.
// It creates a PATCH request with the specified URL and executes it with the configured retry behavior.
// Use WithBody() to add a request body and content type.
func (c *Client) Patch(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPatch, url, opts...)
}

// Delete is a convenience method for making DELETE requests with retry logic.
// It creates a DELETE request for the specified URL and executes it with the configured retry behavior.
func (c *Client) Delete(
	ctx context.Context,
	url string,
	opts ...RequestOption,
) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodDelete, url, opts...)
}
