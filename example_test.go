package retry_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
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
	resp, err := client.Do(ctx, req)
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
	resp, err := client.Do(ctx, req)
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

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/data", nil)
	if err != nil {
		log.Fatal(err)
	}

	// Set overall timeout for the operation (including retries)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
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

	resp, err := client.Do(ctx, req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Connected (insecure): %d\n", resp.StatusCode)
}
