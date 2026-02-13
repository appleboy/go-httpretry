package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

func main() {
	// Create client with custom configuration and observability
	client, err := retry.NewClient(
		// Retry configuration
		retry.WithMaxRetries(5),
		retry.WithInitialRetryDelay(500*time.Millisecond),
		retry.WithMaxRetryDelay(10*time.Second),
		retry.WithRetryDelayMultiple(2.0),

		// Enable jitter to prevent thundering herd
		retry.WithJitter(true),

		// Respect Retry-After header for rate limiting
		retry.WithRespectRetryAfter(true),

		// Per-attempt timeout
		retry.WithPerAttemptTimeout(3*time.Second),

		// Add observability callback
		retry.WithOnRetry(func(info retry.RetryInfo) {
			log.Printf("[RETRY] Attempt %d after %v (status: %d, elapsed: %v)",
				info.Attempt,
				info.Delay,
				info.StatusCode,
				info.TotalElapsed,
			)
			if info.Err != nil {
				log.Printf("[RETRY] Error: %v", info.Err)
			}
			if info.RetryAfter > 0 {
				log.Printf("[RETRY] Server requested retry after: %v", info.RetryAfter)
			}
		}),

		// Custom retry logic
		retry.WithRetryableChecker(func(err error, resp *http.Response) bool {
			// Always retry on network errors
			if err != nil {
				return true
			}

			// Retry on 5xx, 429, and also 408 (Request Timeout)
			if resp != nil {
				return resp.StatusCode >= 500 ||
					resp.StatusCode == http.StatusTooManyRequests ||
					resp.StatusCode == http.StatusRequestTimeout
			}

			return false
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Set overall timeout for the entire operation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Example 1: Test with flaky endpoint (simulates 50% failure rate)
	fmt.Println("=== Example 1: Handling Flaky Endpoint ===")
	resp, err := client.Get(ctx, "https://httpbin.org/status/500:0.3,200:0.7")
	if err != nil {
		log.Printf("Request failed after retries: %v\n", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Success! Status: %d\n\n", resp.StatusCode)
	}

	// Example 2: POST with retry and observability
	fmt.Println("=== Example 2: POST with Retry and Monitoring ===")
	jsonData := []byte(`{"data":"important"}`)
	resp, err = client.Post(ctx, "https://httpbin.org/post",
		retry.WithBody("application/json", bytes.NewReader(jsonData)),
		retry.WithHeaders(map[string]string{
			"Authorization": "Bearer token",
			"X-Request-ID":  "req-12345",
			"X-Idempotency": "key-67890",
		}))
	if err != nil {
		log.Printf("POST request failed: %v\n", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("POST successful! Status: %d\n\n", resp.StatusCode)
	}

	// Example 3: Testing rate limiting with Retry-After
	fmt.Println("=== Example 3: Rate Limiting with Retry-After ===")
	// Note: httpbin.org doesn't return Retry-After header,
	// but this demonstrates the client will respect it when present
	resp, err = client.Get(ctx, "https://httpbin.org/status/429")
	if err != nil {
		log.Printf("Rate limited request failed: %v\n", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)
	}
}
