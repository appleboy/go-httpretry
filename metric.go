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

// determineRetryReason categorizes the retry reason (for metrics and logging)
func determineRetryReason(err error, resp *http.Response) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		if errors.Is(err, context.Canceled) {
			return "canceled"
		}
		return "network_error"
	}

	if resp == nil {
		return "unknown"
	}

	switch {
	case resp.StatusCode == 429:
		return "rate_limited"
	case resp.StatusCode >= 500:
		return "5xx"
	case resp.StatusCode >= 400:
		return "4xx"
	default:
		return "other"
	}
}
