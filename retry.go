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

	// Observability (default to no-op implementations, can be replaced via Options)
	metrics MetricsCollector
	tracer  Tracer
	logger  Logger

	// Observability optimization flags (internal use only, not exported)
	metricsEnabled bool // true if metrics is not nopMetricsCollector
	tracerEnabled  bool // true if tracer is not nopTracer
	loggerEnabled  bool // true if logger is not nopLogger
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

		// Initialize observability with no-op implementations (avoids nil checks later)
		metrics: defaultMetrics,
		tracer:  defaultTracer,
		logger:  defaultLogger,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Check if any option returned an error
	if c.err != nil {
		return nil, c.err
	}

	// Detect whether each observability component is enabled
	// Use type assertion to check if the component is a no-op implementation
	_, isNopMetrics := c.metrics.(nopMetricsCollector)
	c.metricsEnabled = !isNopMetrics

	_, isNopTracer := c.tracer.(nopTracer)
	c.tracerEnabled = !isNopTracer

	_, isNopLogger := c.logger.(nopLogger)
	c.loggerEnabled = !isNopLogger

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

// computeNextDelay calculates the next retry delay using exponential backoff
func computeNextDelay(
	current time.Duration,
	multiplier float64,
	maxDelay time.Duration,
) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > maxDelay {
		return maxDelay
	}
	return next
}

// applyDelayModifiers applies Retry-After, jitter, and max cap to the delay
// Returns: (actual delay, Retry-After delay)
func (c *Client) applyDelayModifiers(
	baseDelay time.Duration,
	resp *http.Response,
) (time.Duration, time.Duration) {
	actualDelay := baseDelay
	retryAfterDelay := time.Duration(0)

	// Check Retry-After header
	if c.respectRetryAfter && resp != nil {
		retryAfterDelay = parseRetryAfter(resp)
		if retryAfterDelay > 0 {
			actualDelay = retryAfterDelay
		}
	}

	// Apply jitter
	if c.jitterEnabled {
		actualDelay = applyJitter(actualDelay)
	}

	// Apply max cap
	if actualDelay > c.maxRetryDelay {
		actualDelay = c.maxRetryDelay
	}

	return actualDelay, retryAfterDelay
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
//
// For large file uploads or streaming data, set req.GetBody to enable retries:
//
//	file, _ := os.Open("large-file.dat")
//	req, _ := http.NewRequestWithContext(ctx, "POST", url, file)
//	req.GetBody = func() (io.ReadCloser, error) {
//	    return os.Open("large-file.dat")  // Reopen for each retry
//	}
//	resp, err := client.Do(req)
//
// See the large_file_upload example for complete implementation patterns.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("retry: nil Request")
	}
	return c.DoWithContext(req.Context(), req)
}

// attemptResult contains the result of executing an attempt
type attemptResult struct {
	resp            *http.Response
	err             error
	statusCode      int
	attemptDuration time.Duration
	cancelAttempt   context.CancelFunc
}

// executeAttempt performs a single HTTP request attempt with tracing
func (c *Client) executeAttempt(
	ctx context.Context,
	req *http.Request,
	attempt int,
) (attemptResult, Span) {
	attemptStart := time.Now()

	// Start attempt span (conditional on tracerEnabled)
	var attemptSpan Span
	attemptCtx := ctx
	if c.tracerEnabled {
		attemptCtx, attemptSpan = c.tracer.StartSpan(ctx, "http.retry.attempt",
			Attribute{Key: "retry.attempt", Value: attempt + 1},
			Attribute{Key: "http.method", Value: req.Method},
		)
	} else {
		// Return no-op span to maintain interface consistency
		attemptSpan = nopSpan{}
	}

	// Create a per-attempt context with timeout if configured
	var cancelAttempt context.CancelFunc
	if c.perAttemptTimeout > 0 {
		attemptCtx, cancelAttempt = context.WithTimeout(attemptCtx, c.perAttemptTimeout)
	}

	// Clone the request for retry (important: body might be consumed)
	reqClone := req.Clone(attemptCtx)

	//nolint:bodyclose // Response body is returned to caller who will close it
	resp, err := c.httpClient.Do(reqClone)
	attemptDuration := time.Since(attemptStart)

	// Record metrics for this attempt (conditional on metricsEnabled)
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	if c.metricsEnabled {
		c.metrics.RecordAttempt(req.Method, statusCode, attemptDuration, err)
	}

	// Update attempt span (conditional on tracerEnabled)
	if c.tracerEnabled {
		if resp != nil {
			attemptSpan.SetAttributes(
				Attribute{Key: "http.status_code", Value: resp.StatusCode},
			)
		}
		if err != nil {
			attemptSpan.SetStatus("error", err.Error())
		} else {
			attemptSpan.SetStatus("ok", "")
		}
	}

	return attemptResult{
		resp:            resp,
		err:             err,
		statusCode:      statusCode,
		attemptDuration: attemptDuration,
		cancelAttempt:   cancelAttempt,
	}, attemptSpan
}

// wrapBodyWithCancel wraps a response body to cancel the context when closed
func wrapBodyWithCancel(resp *http.Response, cancelAttempt context.CancelFunc) {
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

// DoWithContext executes an HTTP request with automatic retry logic using exponential backoff.
// Use this when you need explicit control over the context separate from the request.
func (c *Client) DoWithContext(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("retry: nil Request")
	}
	var lastErr error
	var resp *http.Response
	startTime := time.Now()

	// Start outer span for entire retry operation (conditional on tracerEnabled)
	var requestSpan Span
	if c.tracerEnabled {
		ctx, requestSpan = c.tracer.StartSpan(ctx, "http.retry.request",
			Attribute{Key: "http.method", Value: req.Method},
			Attribute{Key: "http.url", Value: req.URL.String()},
			Attribute{Key: "retry.max_attempts", Value: c.maxRetries + 1},
		)
		defer requestSpan.End()
	}

	// Log request start (conditional on loggerEnabled)
	if c.loggerEnabled {
		c.logger.Debug("starting request",
			"method", req.Method,
			"url", req.URL.String(),
			"max_retries", c.maxRetries,
		)
	}

	var nextDelayBase time.Duration // Base delay for next retry (before modifiers)
	var shouldWait bool             // Whether to wait before this attempt

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// === PHASE 1: Wait for delay (if retrying) ===
		if shouldWait && attempt > 0 {
			// Apply Retry-After, jitter, cap
			actualDelay, retryAfterDelay := c.applyDelayModifiers(nextDelayBase, resp)

			// Call onRetry callback
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

			// Log retry attempt (conditional on loggerEnabled)
			if c.loggerEnabled {
				c.logger.Info("retrying request",
					"method", req.Method,
					"attempt", attempt+1,
					"delay", actualDelay,
				)
			}

			// Wait for delay
			select {
			case <-ctx.Done():
				// Context cancelled during wait
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
				// Continue to attempt
			}
		}

		// === PHASE 2: Execute the attempt ===
		result, attemptSpan := c.executeAttempt(ctx, req, attempt)
		attemptSpan.End()

		resp = result.resp
		lastErr = result.err

		// === PHASE 3: Check if we should retry ===
		if !c.retryableChecker(lastErr, resp) {
			// Success or non-retryable error
			if c.metricsEnabled {
				c.metrics.RecordRequestComplete(
					req.Method,
					result.statusCode,
					time.Since(startTime),
					attempt+1,
					true,
				)
			}
			if c.loggerEnabled {
				c.logger.Debug("request completed",
					"method", req.Method,
					"attempts", attempt+1,
					"duration", time.Since(startTime),
				)
			}
			if c.tracerEnabled {
				requestSpan.SetStatus("ok", "")
			}

			// Wrap the response body to cancel the per-attempt context when the body is closed
			wrapBodyWithCancel(resp, result.cancelAttempt)
			return resp, lastErr
		}

		// === PHASE 4: Decide whether to retry ===
		isLastAttempt := attempt == c.maxRetries

		if !isLastAttempt {
			// Going to retry - calculate and record next delay

			// Calculate base delay for next attempt
			if attempt == 0 {
				nextDelayBase = c.initialRetryDelay
			} else {
				nextDelayBase = computeNextDelay(
					nextDelayBase,
					c.retryDelayMultiple,
					c.maxRetryDelay,
				)
			}

			// Preview actual delay (for logging)
			previewDelay, _ := c.applyDelayModifiers(nextDelayBase, resp)

			// Record retry decision
			retryReason := determineRetryReason(lastErr, resp)
			if c.metricsEnabled {
				c.metrics.RecordRetry(req.Method, retryReason, attempt+1)
			}

			if c.loggerEnabled {
				// Build base log fields
				logFields := []any{
					"method", req.Method,
					"url", req.URL.String(),
					"attempt", attempt + 1,
					"reason", retryReason,
					"next_delay_ms", previewDelay.Milliseconds(),
					"elapsed_ms", time.Since(startTime).Milliseconds(),
				}

				// Add error message if available (network errors, timeouts)
				if lastErr != nil {
					logFields = append(logFields, "error", lastErr.Error())
				}

				// Add HTTP status code if available (5xx, 429)
				if resp != nil {
					logFields = append(logFields, "status", resp.StatusCode)
				}

				c.logger.Warn("request failed, will retry", logFields...)
			}

			if c.tracerEnabled {
				requestSpan.AddEvent("retry",
					Attribute{Key: "retry.attempt", Value: attempt + 1},
					Attribute{Key: "retry.reason", Value: retryReason},
					Attribute{Key: "retry.delay_ms", Value: previewDelay.Milliseconds()},
				)
			}

			shouldWait = true

			// Close response body for retry
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if result.cancelAttempt != nil {
				result.cancelAttempt()
			}
		} else {
			// Last attempt - keep response body open
			wrapBodyWithCancel(resp, result.cancelAttempt)
		}
	}

	// All retries exhausted
	totalDuration := time.Since(startTime)
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	// Log failure (conditional on loggerEnabled)
	if c.loggerEnabled {
		// Build base log fields
		logFields := []any{
			"method", req.Method,
			"url", req.URL.String(),
			"attempts", c.maxRetries + 1,
			"duration_ms", totalDuration.Milliseconds(),
			"final_status", statusCode,
		}

		// Add final error if available
		if lastErr != nil {
			logFields = append(logFields, "error", lastErr.Error())
		}

		c.logger.Error("request failed after all retries", logFields...)
	}

	// Record final metrics (conditional on metricsEnabled)
	if c.metricsEnabled {
		c.metrics.RecordRequestComplete(
			req.Method,
			statusCode,
			totalDuration,
			c.maxRetries+1,
			false,
		)
	}

	// Update request span (conditional on tracerEnabled)
	if c.tracerEnabled {
		requestSpan.SetStatus("error", "max retries exceeded")
		requestSpan.SetAttributes(
			Attribute{Key: "retry.exhausted", Value: true},
		)
	}

	// All retries exhausted - return RetryError with detailed information
	return resp, &RetryError{
		Attempts:   c.maxRetries + 1, // +1 because attempts include the initial request
		LastErr:    lastErr,
		LastStatus: statusCode,
		Elapsed:    totalDuration,
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
