package retry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
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
	customCertsPEM     [][]byte // Store PEM bytes to merge with system certs later
	insecureSkipVerify bool     // Skip TLS certificate verification
	jitterEnabled      bool     // Add random jitter to retry delays
	onRetryFunc        OnRetryFunc
	respectRetryAfter  bool // Respect Retry-After header from responses
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

// WithInsecureSkipVerify disables TLS certificate verification.
// WARNING: This makes the client vulnerable to man-in-the-middle attacks.
// Only use this for testing or development environments.
func WithInsecureSkipVerify() Option {
	return func(c *Client) {
		c.insecureSkipVerify = true
	}
}

// WithCertFromFile loads a PEM-encoded certificate from a file path
// and adds it to the client's trusted certificate pool.
// This is useful for connecting to servers with self-signed or internal CA certificates.
func WithCertFromFile(certPath string) Option {
	return func(c *Client) {
		if c.err != nil {
			return // Previous error, skip
		}

		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			c.err = fmt.Errorf("failed to read certificate file %s: %w", certPath, err)
			return
		}

		// Validate PEM format by trying to append to a test pool
		testPool := x509.NewCertPool()
		if !testPool.AppendCertsFromPEM(certPEM) {
			c.err = fmt.Errorf("failed to parse certificate from %s: invalid PEM format", certPath)
			return
		}

		// Store PEM bytes for later merging with system certs
		c.customCertsPEM = append(c.customCertsPEM, certPEM)
	}
}

// WithCertFromBytes loads a PEM-encoded certificate from memory
// and adds it to the client's trusted certificate pool.
// This is useful for certificates loaded from configuration or embedded in the application.
func WithCertFromBytes(certPEM []byte) Option {
	return func(c *Client) {
		if c.err != nil {
			return // Previous error, skip
		}

		if len(certPEM) == 0 {
			c.err = fmt.Errorf("certificate data is empty")
			return
		}

		// Validate PEM format by trying to append to a test pool
		testPool := x509.NewCertPool()
		if !testPool.AppendCertsFromPEM(certPEM) {
			c.err = fmt.Errorf("failed to parse certificate: invalid PEM format")
			return
		}

		// Store PEM bytes for later merging with system certs
		c.customCertsPEM = append(c.customCertsPEM, certPEM)
	}
}

// WithCertFromURL downloads a PEM-encoded certificate from a URL
// and adds it to the client's trusted certificate pool.
// The download has a fixed timeout of 30 seconds.
// This is useful for dynamically loading certificates from certificate servers.
func WithCertFromURL(certURL string) Option {
	return func(c *Client) {
		if c.err != nil {
			return // Previous error, skip
		}

		// Create client with 30-second timeout for downloading certificate
		httpClient := &http.Client{Timeout: 30 * time.Second}

		// Create request with context for timeout and cancellation support
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
		if err != nil {
			c.err = fmt.Errorf("failed to create request for %s: %w", certURL, err)
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			c.err = fmt.Errorf("failed to download certificate from %s: %w", certURL, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.err = fmt.Errorf(
				"failed to download certificate from %s: HTTP %d",
				certURL,
				resp.StatusCode,
			)
			return
		}

		certPEM, err := io.ReadAll(resp.Body)
		if err != nil {
			c.err = fmt.Errorf("failed to read certificate from %s: %w", certURL, err)
			return
		}

		// Reuse WithCertFromBytes to parse and add
		WithCertFromBytes(certPEM)(c)
	}
}

// NewClient creates a new retry-enabled HTTP client with the given options.
// Returns an error if certificate loading fails or if any option encounters an error.
func NewClient(opts ...Option) (*Client, error) {
	c := &Client{
		maxRetries:         defaultMaxRetries,
		initialRetryDelay:  defaultInitialRetryDelay,
		maxRetryDelay:      defaultMaxRetryDelay,
		retryDelayMultiple: defaultRetryDelayMultiple,
		httpClient:         http.DefaultClient,
		retryableChecker:   DefaultRetryableChecker,
		jitterEnabled:      true, // Enable jitter by default to prevent thundering herd
	}

	for _, opt := range opts {
		opt(c)
	}

	// Check if any option returned an error
	if c.err != nil {
		return nil, c.err
	}

	// Apply custom TLS configuration if needed
	if len(c.customCertsPEM) > 0 || c.insecureSkipVerify {
		var rootCAs *x509.CertPool

		// Only set up custom cert pool if certificates are provided
		if len(c.customCertsPEM) > 0 {
			// Start with system cert pool, fall back to empty pool if unavailable
			var err error
			rootCAs, err = x509.SystemCertPool()
			if rootCAs == nil || err != nil {
				rootCAs = x509.NewCertPool()
			}

			// Add all custom certificates to the system cert pool
			for _, certPEM := range c.customCertsPEM {
				rootCAs.AppendCertsFromPEM(certPEM)
			}
		}

		// Create TLS config with combined cert pool and InsecureSkipVerify
		tlsConfig := &tls.Config{
			RootCAs: rootCAs,
			// #nosec G402 - InsecureSkipVerify is intentionally configurable via WithInsecureSkipVerify()
			InsecureSkipVerify: c.insecureSkipVerify,
			MinVersion:         tls.VersionTLS12, // Require TLS 1.2 or higher for security
		}

		// Handle different httpClient scenarios
		if c.httpClient == http.DefaultClient {
			// User didn't provide custom client, create new one with TLS config
			transport := &http.Transport{
				TLSClientConfig: tlsConfig,
			}
			c.httpClient = &http.Client{
				Transport: transport,
			}
		} else {
			// User provided custom client, merge TLS config into its transport
			switch t := c.httpClient.Transport.(type) {
			case *http.Transport:
				// Clone the existing transport to avoid modifying shared transport
				newTransport := t.Clone()
				if newTransport.TLSClientConfig == nil {
					newTransport.TLSClientConfig = tlsConfig
				} else {
					// Merge with existing TLS config
					if len(c.customCertsPEM) > 0 {
						newTransport.TLSClientConfig.RootCAs = rootCAs
					}
					if c.insecureSkipVerify {
						newTransport.TLSClientConfig.InsecureSkipVerify = true
					}
					if newTransport.TLSClientConfig.MinVersion == 0 {
						newTransport.TLSClientConfig.MinVersion = tls.VersionTLS12
					}
				}
				c.httpClient.Transport = newTransport
			case nil:
				// No transport set, create new one
				c.httpClient.Transport = &http.Transport{
					TLSClientConfig: tlsConfig,
				}
			default:
				// Custom transport type, can't modify - this is a limitation
				// User should use WithCertFrom* before WithHTTPClient to avoid this
				c.err = fmt.Errorf(
					"cannot apply certificates to custom transport type %T; "+
						"use certificate options before WithHTTPClient or provide transport with *http.Transport",
					t,
				)
				return nil, c.err
			}
		}
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

// Do executes an HTTP request with automatic retry logic using exponential backoff
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
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
				if lastErr != nil {
					return nil, fmt.Errorf(
						"context cancelled after %d attempts: %w",
						attempt,
						lastErr,
					)
				}
				return nil, ctx.Err()
			case <-time.After(actualDelay):
				// Calculate next delay with exponential backoff (for next iteration)
				delay = time.Duration(float64(delay) * c.retryDelayMultiple)
				if delay > c.maxRetryDelay {
					delay = c.maxRetryDelay
				}
			}
		}

		// Clone the request for retry (important: body might be consumed)
		reqClone := req.Clone(ctx)

		resp, lastErr = c.httpClient.Do(reqClone)

		// Check if we should retry
		if !c.retryableChecker(lastErr, resp) {
			// Success or non-retryable error
			return resp, lastErr
		}

		// Close response body before retry to prevent resource leak
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}

	// All retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetries, lastErr)
	}

	return resp, lastErr
}
