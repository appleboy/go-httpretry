package retry_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

// Example_withCertFromFile demonstrates loading a certificate from a file
func Example_withCertFromFile() {
	// Load a custom certificate from a file
	// This is useful for connecting to servers with self-signed or internal CA certificates
	client, err := retry.NewClient(
		retry.WithCertFromFile("/path/to/internal-ca.pem"),
		retry.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api/data",
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

	fmt.Printf("Connected securely: %d\n", resp.StatusCode)
}

// Example_withCertFromURL demonstrates downloading a certificate from a URL
func Example_withCertFromURL() {
	// Download and trust a certificate from a URL
	// This is useful for dynamically loading certificates from certificate servers
	client, err := retry.NewClient(
		retry.WithCertFromURL("https://pki.company.com/certs/internal-ca.pem"),
		retry.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api/data",
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

	fmt.Printf("Connected with downloaded cert: %d\n", resp.StatusCode)
}

// Example_withMultipleCerts demonstrates using multiple certificates
func Example_withMultipleCerts() {
	// Trust multiple certificates from different sources
	// This is useful when connecting to multiple internal services
	// with different CAs or certificate chains
	client, err := retry.NewClient(
		retry.WithCertFromFile("/path/to/internal-ca-1.pem"),
		retry.WithCertFromFile("/path/to/internal-ca-2.pem"),
		retry.WithCertFromURL("https://pki.company.com/certs/partner-ca.pem"),
		retry.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api/data",
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

	fmt.Printf("Connected with multiple certs: %d\n", resp.StatusCode)
}

// Example_withCertFromBytes demonstrates using an in-memory certificate
func Example_withCertFromBytes() {
	// Use a certificate loaded from memory (e.g., from configuration or embedded data)
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHHCgVZU7c7MA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBnRl
c3RjYTAeFw0yNDAxMDEwMDAwMDBaFw0yNTAxMDEwMDAwMDBaMBExDzANBgNVBAMM
BnRlc3RjYTCBnzANBgkqhkiG9w0BAQEFAAOBjQAwgYkCgYEAw6h0WqLjGcDmPlNb
Nz3VPzQjGiLvNSBB+9CX6sqQ7KBwCcHiVJqTnG5KsLqkLJDl6pKN6gqVyPJ5J5qD
AeLOCwHK7qKv5V7YwJpnGPGXPGxd2LQJYdZnqKTb9a1sKQqC8BZ+NfPnHZLU5wUc
yWlJpLLqYMbKqW4VlGYVxXQcUGsCAwEAATANBgkqhkiG9w0BAQsFAAOBgQB7LNhL
v8V8SvLZFdQxSJzTpKRq3BZQfPNXqVzQDXqYpL7KNQBzqO2dpPxZ3JqKk4lLGmGF
zD7E7l9KQC3sM1xqMVCMKxqL1VHQH3YyWMJYqIlYQqFkLCBxL9VqFCZyLqGlLFLX
nHyqB3LJ0L5+zLqQH9KYXKQz5Ly3VKVsKQqC8A==
-----END CERTIFICATE-----`)

	client, err := retry.NewClient(
		retry.WithCertFromBytes(certPEM),
		retry.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api/data",
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

	fmt.Printf("Connected with embedded cert: %d\n", resp.StatusCode)
}

// Example_withCertAndCustomHTTPClient demonstrates combining certificates with a custom HTTP client
func Example_withCertAndCustomHTTPClient() {
	// Create a custom HTTP client with specific settings
	customHTTPClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Add custom certificates to the custom client
	// The certificates will be merged into the client's transport
	client, err := retry.NewClient(
		retry.WithHTTPClient(customHTTPClient),
		retry.WithCertFromFile("/path/to/internal-ca.pem"),
		retry.WithMaxRetries(3),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://internal.company.com/api/data",
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

	fmt.Printf("Connected with custom client and certs: %d\n", resp.StatusCode)
}
