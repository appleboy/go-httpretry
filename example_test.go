package retry_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

// Example_basic demonstrates basic usage with default configuration
func Example_basic() {
	// Create a retry client with default settings
	// (3 retries, 1s initial delay, 10s max delay, 2.0 multiplier)
	client, err := retry.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	// Create a request
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}

	// Execute with automatic retries
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Request succeeded")
	}
}

// Example_customConfiguration demonstrates custom retry configuration
func Example_customConfiguration() {
	// Create a retry client with custom settings
	client, err := retry.NewClient(
		retry.WithMaxRetries(5),                           // Retry up to 5 times
		retry.WithInitialRetryDelay(500*time.Millisecond), // Start with 500ms delay
		retry.WithMaxRetryDelay(30*time.Second),           // Cap delay at 30s
		retry.WithRetryDelayMultiple(3.0),                 // Triple delay each time
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.example.com/submit",
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
}

// Example_withTimeout demonstrates using context timeout
func Example_withTimeout() {
	client, err := retry.NewClient(
		retry.WithMaxRetries(10),
		retry.WithInitialRetryDelay(2*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Set overall timeout for the operation (including retries)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		cancel()
		log.Fatal(err) //nolint:gocritic // cancel() is called before Fatal
	}

	resp, err := client.Do(req)
	if err != nil {
		// May be context deadline exceeded if retries take too long
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Request completed within timeout")
}

// Example_customHTTPClient demonstrates using a custom http.Client
func Example_customHTTPClient() {
	// Create a custom http.Client with specific settings
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Use the custom client with retry logic
	client, err := retry.NewClient(
		retry.WithHTTPClient(httpClient),
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(1*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response received: %d\n", resp.StatusCode)
}

// Example_customRetryChecker demonstrates custom retry logic
func Example_customRetryChecker() {
	// Custom checker that also retries on 403 Forbidden
	customChecker := func(err error, resp *http.Response) bool {
		if err != nil {
			return true // Retry on network errors
		}
		if resp == nil {
			return false
		}

		// Retry on 5xx, 429, and also 403
		statusCode := resp.StatusCode
		return statusCode >= 500 ||
			statusCode == http.StatusTooManyRequests ||
			statusCode == http.StatusForbidden
	}

	client, err := retry.NewClient(
		retry.WithRetryableChecker(customChecker),
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(1*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Final status: %d\n", resp.StatusCode)
}

// Example_noRetries demonstrates disabling retries
func Example_noRetries() {
	// Set maxRetries to 0 to disable retries
	client, err := retry.NewClient(
		retry.WithMaxRetries(0),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Request executed once (no retries)")
}

// Example_customTLSConfiguration demonstrates how to configure TLS
// when using go-httpretry with services that require custom certificates.
//
// This example shows the recommended approach: configure your http.Client
// with the desired TLS settings, then pass it to the retry client.
func Example_customTLSConfiguration() {
	// Step 1: Load custom certificate
	certPool, err := x509.SystemCertPool()
	if err != nil {
		// Fall back to empty pool if system pool unavailable
		certPool = x509.NewCertPool()
	}

	certPEM, err := os.ReadFile("/path/to/internal-ca.pem")
	if err != nil {
		log.Fatal(err)
	}

	if !certPool.AppendCertsFromPEM(certPEM) {
		log.Fatal("failed to append certificate")
	}

	// Step 2: Create TLS configuration
	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
	}

	// Step 3: Create HTTP client with TLS config
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 30 * time.Second,
	}

	// Step 4: Create retry client with pre-configured HTTP client
	client, err := retry.NewClient(
		retry.WithHTTPClient(httpClient),
		retry.WithMaxRetries(3),
		retry.WithJitter(true),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Step 5: Use the retry client normally
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api",
		nil,
	)

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
}

// Example_insecureTLS demonstrates how to skip TLS verification for testing.
// WARNING: Only use this in development/testing environments!
func Example_insecureTLS() {
	// Create HTTP client with insecure TLS config
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // #nosec G402
			},
		},
	}

	client, err := retry.NewClient(
		retry.WithHTTPClient(httpClient),
		retry.WithMaxRetries(2),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://self-signed.badssl.com/",
		nil,
	)

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Connected (insecure): %d\n", resp.StatusCode)
}

// Example_presetRealtimeClient demonstrates using the realtime preset
// for user-facing requests that require fast response times
func Example_presetRealtimeClient() {
	// Use the realtime preset for user-facing operations
	// (2 retries, 100ms initial delay, 1s max delay, 3s per-attempt timeout)
	client, err := retry.NewRealtimeClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, "https://api.example.com/search?q=hello")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Search results retrieved")
}

// Example_presetBackgroundClient demonstrates using the background preset
// for non-time-sensitive operations like batch jobs
func Example_presetBackgroundClient() {
	// Use the background preset for background tasks
	// (10 retries, 5s initial delay, 60s max delay, 3.0x multiplier, 30s per-attempt timeout)
	client, err := retry.NewBackgroundClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Post(ctx, "https://api.example.com/batch/sync")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Batch sync completed")
}

// Example_presetRateLimitedClient demonstrates using the rate-limited preset
// for APIs with strict rate limits and Retry-After headers
func Example_presetRateLimitedClient() {
	// Use the rate-limited preset for third-party APIs
	// (5 retries, 2s initial delay, respects Retry-After header, jitter enabled)
	client, err := retry.NewRateLimitedClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, "https://api.github.com/users/appleboy")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("GitHub API responded: %d\n", resp.StatusCode)
}

// Example_presetMicroserviceClient demonstrates using the microservice preset
// for internal service-to-service communication
func Example_presetMicroserviceClient() {
	// Use the microservice preset for internal calls
	// (3 retries, 50ms initial delay, 500ms max delay, 2s per-attempt timeout, jitter enabled)
	client, err := retry.NewMicroserviceClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, "http://user-service:8080/users/123")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("User data retrieved from internal service")
}

// Example_presetAggressiveClient demonstrates using the aggressive preset
// for scenarios with frequent transient failures
func Example_presetAggressiveClient() {
	// Use the aggressive preset for unreliable networks
	// (10 retries, 100ms initial delay, 5s max delay)
	client, err := retry.NewAggressiveClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, "https://unreliable-api.example.com/data")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Data retrieved after aggressive retries")
}

// Example_presetConservativeClient demonstrates using the conservative preset
// for operations where you want to be cautious about retry storms
func Example_presetConservativeClient() {
	// Use the conservative preset to avoid retry storms
	// (2 retries, 5s initial delay)
	client, err := retry.NewConservativeClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Post(ctx, "https://api.example.com/expensive-operation")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Expensive operation completed")
}

// Example_presetWithCustomOverride demonstrates overriding preset defaults
func Example_presetWithCustomOverride() {
	// Start with a preset and override specific settings
	client, err := retry.NewRealtimeClient(
		retry.WithMaxRetries(5),                          // Override: more retries than default (2)
		retry.WithInitialRetryDelay(50*time.Millisecond), // Override: faster initial retry
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	resp, err := client.Get(ctx, "https://api.example.com/data")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Request completed with custom realtime config")
}

// Example_doWithContext demonstrates using DoWithContext for explicit context control.
// Use DoWithContext when you need to pass a different context than the one in the request,
// or when you want explicit control over the context used for retries.
func Example_doWithContext() {
	client, err := retry.NewClient(
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(1*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create request with background context first
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://api.example.com/data",
		nil,
	)
	if err != nil {
		cancel()
		log.Fatal(err) //nolint:gocritic // cancel() is called before Fatal
	}

	// Use DoWithContext to override the request's context with our timeout context
	resp, err := client.DoWithContext(ctx, req)
	if err != nil {
		log.Printf("Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
}

// Example_standardInterface demonstrates that retry.Client is compatible with http.Client interface.
// You can use retry.Client anywhere that accepts the standard Do(req) signature.
func Example_standardInterface() {
	// Create retry client
	retryClient, err := retry.NewClient(
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(1*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Function that accepts anything with Do(*http.Request) signature
	executeRequest := func(doer interface {
		Do(*http.Request) (*http.Response, error)
	}, url string,
	) error {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		resp, err := doer.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		fmt.Printf("Status: %d\n", resp.StatusCode)
		return nil
	}

	// Works with retry.Client
	if err := executeRequest(retryClient, "https://api.example.com/data"); err != nil {
		log.Printf("Request failed: %v", err)
	}

	// Also works with standard http.Client
	if err := executeRequest(http.DefaultClient, "https://api.example.com/data"); err != nil {
		log.Printf("Request failed: %v", err)
	}
}

// Example_largeFileUpload demonstrates the correct way to upload large files with retry support.
// This is important because WithBody() buffers the entire body in memory, which is not suitable
// for large files.
func Example_largeFileUpload() {
	client, err := retry.NewClient(
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(1*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	filePath := "/path/to/large-file.dat"

	// ❌ WRONG: Do NOT use WithBody for large files (buffers entire file in memory)
	// file, _ := os.ReadFile(filePath)  // Loads entire file into memory!
	// resp, _ := client.Post(ctx, "https://api.example.com/upload",
	//     retry.WithBody("application/octet-stream", bytes.NewReader(file)))

	// ✅ CORRECT: Use Do() with GetBody for large files (memory efficient)

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Get file info for Content-Length
	stat, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	// Create request with file as body
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.example.com/upload", file)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = stat.Size()

	// CRITICAL: Set GetBody to reopen the file for each retry attempt
	// This enables retry support without buffering the entire file in memory
	req.GetBody = func() (io.ReadCloser, error) {
		return os.Open(filePath)
	}

	// Execute with automatic retry support
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Upload completed: %d\n", resp.StatusCode)
}
