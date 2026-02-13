package retry

import (
	"time"
)

// NewRealtimeClient creates a client optimized for realtime user-facing requests.
// This preset is designed for scenarios where fast response times are critical,
// such as user interactions, search suggestions, or API calls triggered by UI actions.
//
// Configuration:
//   - Max retries: 2 (quick failure for better UX)
//   - Initial delay: 100ms (minimal wait time)
//   - Max delay: 1s (short maximum delay)
//   - Per-attempt timeout: 3s (prevent slow requests)
//
// Use cases:
//   - User-initiated API calls
//   - Real-time search and autocomplete
//   - Interactive UI operations requiring fast failure
func NewRealtimeClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(2),
		WithInitialRetryDelay(100 * time.Millisecond),
		WithMaxRetryDelay(1 * time.Second),
		WithPerAttemptTimeout(3 * time.Second),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewBackgroundClient creates a client optimized for background tasks.
// This preset is designed for non-time-sensitive operations where reliability
// is more important than speed, such as batch processing, scheduled jobs, or data sync.
//
// Configuration:
//   - Max retries: 10 (persistent retries for reliability)
//   - Initial delay: 5s (longer initial backoff)
//   - Max delay: 60s (up to 1 minute between retries)
//   - Backoff multiplier: 3.0 (aggressive exponential backoff)
//   - Per-attempt timeout: 30s (generous timeout per attempt)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Batch data synchronization
//   - Scheduled/cron jobs
//   - Data export/import operations
//   - Async task processing
func NewBackgroundClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(10),
		WithInitialRetryDelay(5 * time.Second),
		WithMaxRetryDelay(60 * time.Second),
		WithRetryDelayMultiple(3.0),
		WithPerAttemptTimeout(30 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewRateLimitedClient creates a client optimized for APIs with strict rate limits.
// This preset respects server-provided Retry-After headers and uses jitter
// to prevent thundering herd problems when multiple clients retry simultaneously.
//
// Configuration:
//   - Max retries: 5 (balanced retry count)
//   - Initial delay: 2s (moderate initial backoff)
//   - Max delay: 30s (reasonable maximum wait)
//   - Per-attempt timeout: 15s (prevent slow requests)
//   - Respect Retry-After: enabled (honor server guidance)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Third-party APIs (GitHub, Stripe, AWS, etc.)
//   - Services returning 429 Too Many Requests
//   - APIs with published rate limits
//   - Services providing Retry-After headers
func NewRateLimitedClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(5),
		WithInitialRetryDelay(2 * time.Second),
		WithMaxRetryDelay(30 * time.Second),
		WithPerAttemptTimeout(15 * time.Second),
		WithRespectRetryAfter(true),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewMicroserviceClient creates a client optimized for internal microservice communication.
// This preset uses very short delays suitable for high-speed internal networks
// where services are geographically close (e.g., within the same Kubernetes cluster).
//
// Configuration:
//   - Max retries: 3 (moderate retry count)
//   - Initial delay: 50ms (very short delay for internal network)
//   - Max delay: 500ms (sub-second maximum)
//   - Per-attempt timeout: 2s (fast timeout for internal calls)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Kubernetes pod-to-pod communication
//   - Internal service mesh calls
//   - Low-latency internal APIs
//   - gRPC fallback to HTTP
func NewMicroserviceClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(3),
		WithInitialRetryDelay(50 * time.Millisecond),
		WithMaxRetryDelay(500 * time.Millisecond),
		WithPerAttemptTimeout(2 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewAggressiveClient creates a client with aggressive retry behavior.
// This preset attempts many retries with short delays, suitable for scenarios
// where transient failures are common and quick recovery is expected.
//
// Configuration:
//   - Max retries: 10 (many retry attempts)
//   - Initial delay: 100ms (very short initial delay)
//   - Max delay: 5s (moderate maximum delay)
//   - Per-attempt timeout: 10s (prevent slow requests)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Highly unreliable networks
//   - Services with frequent transient failures
//   - Scenarios where eventual success is expected
func NewAggressiveClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(10),
		WithInitialRetryDelay(100 * time.Millisecond),
		WithMaxRetryDelay(5 * time.Second),
		WithPerAttemptTimeout(10 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewConservativeClient creates a client with conservative retry behavior.
// This preset uses fewer retries with longer delays, suitable for scenarios
// where you want to be cautious about retry storms or when failures are likely permanent.
//
// Configuration:
//   - Max retries: 2 (few retry attempts)
//   - Initial delay: 5s (long initial delay)
//   - Per-attempt timeout: 20s (generous timeout)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - External APIs with strict limits
//   - Operations where failures are likely permanent
//   - Preventing retry storms during outages
func NewConservativeClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(2),
		WithInitialRetryDelay(5 * time.Second),
		WithPerAttemptTimeout(20 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewWebhookClient creates a client optimized for webhook/callback scenarios.
// This preset is designed for outbound webhook calls where the sender typically
// has its own retry mechanism, so quick failure is preferred over aggressive retries.
//
// Configuration:
//   - Max retries: 1 (single retry attempt)
//   - Initial delay: 500ms (quick retry)
//   - Max delay: 1s (short maximum delay)
//   - Per-attempt timeout: 5s (reasonable timeout)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Sending webhooks to external services
//   - Third-party integration callbacks
//   - Event notification systems
//   - Outbound webhook deliveries
func NewWebhookClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(1),
		WithInitialRetryDelay(500 * time.Millisecond),
		WithMaxRetryDelay(1 * time.Second),
		WithPerAttemptTimeout(5 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewCriticalClient creates a client for mission-critical operations.
// This preset ensures maximum effort to succeed, suitable for operations
// that absolutely must complete, such as payment processing or critical data sync.
//
// Configuration:
//   - Max retries: 15 (many retry attempts)
//   - Initial delay: 1s (reasonable initial backoff)
//   - Max delay: 120s (up to 2 minutes between retries)
//   - Backoff multiplier: 2.0 (standard exponential backoff)
//   - Per-attempt timeout: 60s (generous timeout per attempt)
//   - Jitter: enabled (prevent synchronized retries)
//   - Respect Retry-After: enabled (honor server guidance)
//
// Use cases:
//   - Payment processing
//   - Order confirmation
//   - Critical data synchronization
//   - Operations that cannot fail
func NewCriticalClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(15),
		WithInitialRetryDelay(1 * time.Second),
		WithMaxRetryDelay(120 * time.Second),
		WithRetryDelayMultiple(2.0),
		WithPerAttemptTimeout(60 * time.Second),
		WithJitter(true),
		WithRespectRetryAfter(true),
	}
	return NewClient(append(defaults, opts...)...)
}

// NewFastFailClient creates a client optimized for fast failure scenarios.
// This preset minimizes retry delays and attempts, suitable for operations
// where you need to know about failures quickly.
//
// Configuration:
//   - Max retries: 1 (single retry attempt)
//   - Initial delay: 50ms (minimal delay)
//   - Max delay: 200ms (very short maximum)
//   - Per-attempt timeout: 1s (short timeout)
//   - Jitter: enabled (prevent synchronized retries)
//
// Use cases:
//   - Health checks
//   - Service discovery
//   - Quick availability probes
//   - Circuit breaker implementations
func NewFastFailClient(opts ...Option) (*Client, error) {
	defaults := []Option{
		WithMaxRetries(1),
		WithInitialRetryDelay(50 * time.Millisecond),
		WithMaxRetryDelay(200 * time.Millisecond),
		WithPerAttemptTimeout(1 * time.Second),
		WithJitter(true),
	}
	return NewClient(append(defaults, opts...)...)
}
