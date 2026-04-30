package retry

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// MetricsCollector defines the interface for collecting metrics (thread-safe)
type MetricsCollector interface {
	// RecordAttempt records a single request attempt
	RecordAttempt(method string, statusCode int, duration time.Duration, err error)

	// RecordRetry records a retry event
	RecordRetry(method string, reason string, attemptNumber int)

	// RecordRequestComplete records request completion (including all retries)
	RecordRequestComplete(
		method string,
		statusCode int,
		totalDuration time.Duration,
		totalAttempts int,
		success bool,
	)
}

// nopMetricsCollector provides no-op implementation to avoid nil checks
type nopMetricsCollector struct{}

func (nopMetricsCollector) RecordAttempt(string, int, time.Duration, error)             {}
func (nopMetricsCollector) RecordRetry(string, string, int)                             {}
func (nopMetricsCollector) RecordRequestComplete(string, int, time.Duration, int, bool) {}

// defaultMetrics is the package-level singleton (internal use, not exported)
var defaultMetrics = nopMetricsCollector{}

const (
	RetryReasonTimeout     = "timeout"
	RetryReasonCanceled    = "canceled"
	RetryReasonNetworkErr  = "network_error"
	RetryReasonRateLimited = "rate_limited"
	RetryReason5xx         = "5xx"
	RetryReason4xx         = "4xx"
	RetryReasonUnknown     = "unknown"
	RetryReasonOther       = "other"
)

// determineRetryReason categorizes the retry reason (for metrics and logging)
func determineRetryReason(err error, resp *http.Response) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return RetryReasonTimeout
		}
		if errors.Is(err, context.Canceled) {
			return RetryReasonCanceled
		}
		return RetryReasonNetworkErr
	}

	if resp == nil {
		return RetryReasonUnknown
	}

	switch {
	case resp.StatusCode == 429:
		return RetryReasonRateLimited
	case resp.StatusCode >= 500:
		return RetryReason5xx
	case resp.StatusCode >= 400:
		return RetryReason4xx
	default:
		return RetryReasonOther
	}
}
