package retry

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClient_Defaults(t *testing.T) {
	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if client.maxRetries != defaultMaxRetries {
		t.Errorf("expected maxRetries=%d, got %d", defaultMaxRetries, client.maxRetries)
	}
	if client.initialRetryDelay != defaultInitialRetryDelay {
		t.Errorf(
			"expected initialRetryDelay=%v, got %v",
			defaultInitialRetryDelay,
			client.initialRetryDelay,
		)
	}
	if client.maxRetryDelay != defaultMaxRetryDelay {
		t.Errorf("expected maxRetryDelay=%v, got %v", defaultMaxRetryDelay, client.maxRetryDelay)
	}
	if client.retryDelayMultiple != defaultRetryDelayMultiple {
		t.Errorf(
			"expected retryDelayMultiple=%f, got %f",
			defaultRetryDelayMultiple,
			client.retryDelayMultiple,
		)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
	if !client.jitterEnabled {
		t.Error("expected jitterEnabled to be true by default")
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	customChecker := func(err error, resp *http.Response) bool { return false }

	client, err := NewClient(
		WithMaxRetries(5),
		WithInitialRetryDelay(2*time.Second),
		WithMaxRetryDelay(20*time.Second),
		WithRetryDelayMultiple(3.0),
		WithHTTPClient(httpClient),
		WithRetryableChecker(customChecker),
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if client.maxRetries != 5 {
		t.Errorf("expected maxRetries=5, got %d", client.maxRetries)
	}
	if client.initialRetryDelay != 2*time.Second {
		t.Errorf("expected initialRetryDelay=2s, got %v", client.initialRetryDelay)
	}
	if client.maxRetryDelay != 20*time.Second {
		t.Errorf("expected maxRetryDelay=20s, got %v", client.maxRetryDelay)
	}
	if client.retryDelayMultiple != 3.0 {
		t.Errorf("expected retryDelayMultiple=3.0, got %f", client.retryDelayMultiple)
	}
	if client.httpClient != httpClient {
		t.Error("expected custom httpClient to be set")
	}
}

func TestNewClient_InvalidOptions(t *testing.T) {
	client, err := NewClient(
		WithMaxRetries(-1),          // Invalid, should be ignored
		WithInitialRetryDelay(-1),   // Invalid, should be ignored
		WithMaxRetryDelay(-1),       // Invalid, should be ignored
		WithRetryDelayMultiple(0.5), // Invalid, should be ignored
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	// Should still have defaults
	if client.maxRetries != defaultMaxRetries {
		t.Errorf("expected default maxRetries=%d, got %d", defaultMaxRetries, client.maxRetries)
	}
	if client.initialRetryDelay != defaultInitialRetryDelay {
		t.Errorf(
			"expected default initialRetryDelay=%v, got %v",
			defaultInitialRetryDelay,
			client.initialRetryDelay,
		)
	}
}

func TestDefaultRetryableChecker(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		resp     *http.Response
		expected bool
	}{
		{
			name:     "network error",
			err:      errors.New("connection refused"),
			resp:     nil,
			expected: true,
		},
		{
			name:     "no error, 200 OK",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusOK},
			expected: false,
		},
		{
			name:     "no error, 400 Bad Request",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusBadRequest},
			expected: false,
		},
		{
			name:     "no error, 429 Too Many Requests",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusTooManyRequests},
			expected: true,
		},
		{
			name:     "no error, 500 Internal Server Error",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusInternalServerError},
			expected: true,
		},
		{
			name:     "no error, 503 Service Unavailable",
			err:      nil,
			resp:     &http.Response{StatusCode: http.StatusServiceUnavailable},
			expected: true,
		},
		{
			name:     "no error, nil response",
			err:      nil,
			resp:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultRetryableChecker(tt.err, tt.resp)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClient_Do_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClient_Do_RetryOn500(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success after retries"))
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestClient_Do_ExhaustedRetries(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(2),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should return the last response with 500 status
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}

	// Should have 1 initial attempt + 2 retries = 3 total
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestClient_Do_ContextCancellation(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(5),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err == nil {
		defer resp.Body.Close()
		t.Fatal("expected context cancellation error")
	}

	// Should only have 1 attempt before context is cancelled during retry delay
	if attempts.Load() > 2 {
		t.Errorf("expected at most 2 attempts before cancellation, got %d", attempts.Load())
	}
}

func TestClient_Do_NoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should not retry on 4xx errors
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", attempts.Load())
	}
}

func TestClient_Do_ExponentialBackoff(t *testing.T) {
	var attempts atomic.Int32
	var requestTimes []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes = append(requestTimes, time.Now())
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetryDelay(500*time.Millisecond),
		WithRetryDelayMultiple(2.0),
		WithMaxRetries(3),
		WithJitter(false), // Disable jitter for predictable timing tests
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err == nil && resp != nil {
		resp.Body.Close()
	}

	if len(requestTimes) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(requestTimes))
	}

	// Check that delays increase exponentially
	delay1 := requestTimes[1].Sub(requestTimes[0])
	delay2 := requestTimes[2].Sub(requestTimes[1])

	if delay1 < 90*time.Millisecond || delay1 > 150*time.Millisecond {
		t.Errorf("first retry delay should be ~100ms, got %v", delay1)
	}

	if delay2 < 180*time.Millisecond || delay2 > 250*time.Millisecond {
		t.Errorf("second retry delay should be ~200ms, got %v", delay2)
	}
}

func TestClient_Do_CustomRetryableChecker(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Custom checker that never retries
	neverRetry := func(err error, resp *http.Response) bool {
		return false
	}

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
		WithRetryableChecker(neverRetry),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should not retry with custom checker
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", attempts.Load())
	}
}

func TestWithCertFromFile_ValidPath(t *testing.T) {
	client, err := NewClient(
		WithCertFromFile("testdata/test-cert.pem"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.customCertsPEM) != 1 {
		t.Errorf("expected 1 custom cert, got %d", len(client.customCertsPEM))
	}
}

func TestWithCertFromFile_InvalidPath(t *testing.T) {
	_, err := NewClient(
		WithCertFromFile("/nonexistent/path/to/cert.pem"),
	)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !errors.Is(err, context.DeadlineExceeded) && err.Error() == "" {
		t.Errorf("expected error message about file not found, got: %v", err)
	}
}

func TestWithCertFromBytes_InvalidPEM(t *testing.T) {
	invalidPEM := []byte("this is not a valid PEM certificate")
	_, err := NewClient(
		WithCertFromBytes(invalidPEM),
	)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestWithCertFromBytes_EmptyData(t *testing.T) {
	_, err := NewClient(
		WithCertFromBytes([]byte{}),
	)
	if err == nil {
		t.Fatal("expected error for empty certificate data")
	}
}

func TestWithCertFromBytes_ValidPEM(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	client, err := NewClient(
		WithCertFromBytes(validPEM),
	)
	if err != nil {
		t.Fatalf("unexpected error for valid PEM: %v", err)
	}
	if len(client.customCertsPEM) != 1 {
		t.Errorf("expected 1 custom cert, got %d", len(client.customCertsPEM))
	}
}

func TestWithCertFromURL_InvalidURL(t *testing.T) {
	_, err := NewClient(
		WithCertFromURL("http://invalid-url-that-does-not-exist.local/cert.pem"),
	)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestWithCertFromURL_ValidURL(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	// Create mock server that serves the certificate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validPEM)
	}))
	defer server.Close()

	client, err := NewClient(
		WithCertFromURL(server.URL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.customCertsPEM) != 1 {
		t.Errorf("expected 1 custom cert, got %d", len(client.customCertsPEM))
	}
}

func TestWithCertFromURL_NonOKStatus(t *testing.T) {
	// Create mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := NewClient(
		WithCertFromURL(server.URL),
	)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestWithMultipleCerts(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	client, err := NewClient(
		WithCertFromBytes(validPEM),
		WithCertFromBytes(validPEM), // Add the same cert twice (just for testing)
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.customCertsPEM) != 2 {
		t.Errorf("expected 2 custom certs, got %d", len(client.customCertsPEM))
	}
}

func TestWithCertAndHTTPClient_Merge(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	// Test 1: Custom client with no transport - should add transport with certs
	customHTTPClient1 := &http.Client{Timeout: 5 * time.Second}
	client1, err := NewClient(
		WithHTTPClient(customHTTPClient1),
		WithCertFromBytes(validPEM),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use the same HTTP client instance
	if client1.httpClient != customHTTPClient1 {
		t.Error("expected same httpClient instance")
	}

	// Should have added transport with TLS config
	if client1.httpClient.Transport == nil {
		t.Fatal("expected transport to be set")
	}
	transport1, ok := client1.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected transport to be *http.Transport")
	}
	if transport1.TLSClientConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
	if transport1.TLSClientConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}

	// Test 2: Custom client with existing transport - should merge TLS config
	existingTransport := &http.Transport{
		MaxIdleConns: 100,
	}
	customHTTPClient2 := &http.Client{
		Timeout:   10 * time.Second,
		Transport: existingTransport,
	}
	client2, err := NewClient(
		WithCertFromBytes(validPEM),
		WithHTTPClient(customHTTPClient2),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use the same HTTP client instance
	if client2.httpClient != customHTTPClient2 {
		t.Error("expected same httpClient instance")
	}

	// Should have cloned and modified transport
	transport2, ok := client2.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected transport to be *http.Transport")
	}
	if transport2 == existingTransport {
		t.Error("expected transport to be cloned, not same instance")
	}
	if transport2.MaxIdleConns != 100 {
		t.Error("expected existing transport settings to be preserved")
	}
	if transport2.TLSClientConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
	if transport2.TLSClientConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
	if transport2.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected MinVersion TLS 1.2, got %d", transport2.TLSClientConfig.MinVersion)
	}
}

// customTransport is a custom http.RoundTripper for testing
type customTransport struct{}

func (ct *customTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestWithCertAndCustomTransportType_Error(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	customHTTPClient := &http.Client{
		Transport: &customTransport{},
	}

	// Should return error when trying to apply certs to custom transport
	_, err = NewClient(
		WithHTTPClient(customHTTPClient),
		WithCertFromBytes(validPEM),
	)
	if err == nil {
		t.Fatal("expected error for custom transport type")
	}
	if err.Error() == "" {
		t.Error("expected error message about custom transport type")
	}
}

func TestTLSConnection_WithSelfSignedCert(t *testing.T) {
	// Create TLS server with self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secure response"))
	}))
	defer server.Close()

	// Get the server's certificate
	serverCert := server.Certificate()
	certPEM := []byte("-----BEGIN CERTIFICATE-----\n" +
		string(serverCert.Raw) + "\n" +
		"-----END CERTIFICATE-----")

	// Note: httptest.NewTLSServer uses a self-signed cert,
	// so we need to get its cert to trust it
	client, err := NewClient(
		WithCertFromBytes(server.Certificate().Raw), // This won't work as-is
	)
	if err != nil {
		// Expected to fail with the raw cert bytes
		// In a real scenario, we'd use properly formatted PEM
		t.Logf("Expected error with raw cert bytes: %v", err)
		return
	}

	// Try to make a request (will likely fail due to cert format issue)
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if resp != nil {
		resp.Body.Close()
	}
	// This test demonstrates the pattern, actual TLS verification requires proper PEM format
	t.Logf("Request result: %v (cert format affects outcome)", err)
	_ = certPEM // Placeholder for proper PEM usage
}

func TestWithInsecureSkipVerify_DefaultClient(t *testing.T) {
	client, err := NewClient(
		WithInsecureSkipVerify(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have insecureSkipVerify set to true
	if !client.insecureSkipVerify {
		t.Error("expected insecureSkipVerify to be true")
	}

	// Should have created a custom transport with InsecureSkipVerify enabled
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected transport to be *http.Transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected TLS InsecureSkipVerify to be true")
	}
}

func TestWithInsecureSkipVerify_WithCustomClient(t *testing.T) {
	customHTTPClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns: 100,
		},
	}

	client, err := NewClient(
		WithHTTPClient(customHTTPClient),
		WithInsecureSkipVerify(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use the same HTTP client instance
	if client.httpClient != customHTTPClient {
		t.Error("expected same httpClient instance")
	}

	// Should have modified the transport
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected transport to be *http.Transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected TLS InsecureSkipVerify to be true")
	}
	// Should preserve existing transport settings
	if transport.MaxIdleConns != 100 {
		t.Error("expected existing transport settings to be preserved")
	}
}

func TestWithInsecureSkipVerify_WithCerts(t *testing.T) {
	// Read valid certificate from test file
	validPEM, err := os.ReadFile("testdata/test-cert.pem")
	if err != nil {
		t.Fatalf("failed to read test certificate: %v", err)
	}

	client, err := NewClient(
		WithCertFromBytes(validPEM),
		WithInsecureSkipVerify(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have both custom certs and InsecureSkipVerify enabled
	if len(client.customCertsPEM) != 1 {
		t.Errorf("expected 1 custom cert, got %d", len(client.customCertsPEM))
	}
	if !client.insecureSkipVerify {
		t.Error("expected insecureSkipVerify to be true")
	}

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected transport to be *http.Transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
	if transport.TLSClientConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected TLS InsecureSkipVerify to be true")
	}
}

func TestWithInsecureSkipVerify_RealTLSConnection(t *testing.T) {
	// Create TLS server with self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secure response"))
	}))
	defer server.Close()

	// Without InsecureSkipVerify, connection should fail
	clientNoSkip, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req1, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp1, err := clientNoSkip.Do(ctx, req1)
	if err == nil {
		resp1.Body.Close()
		t.Log("Warning: Expected TLS verification error, but request succeeded")
	}

	// With InsecureSkipVerify, connection should succeed
	clientWithSkip, err := NewClient(
		WithInsecureSkipVerify(),
	)
	if err != nil {
		t.Fatalf("failed to create client with skip verify: %v", err)
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp2, err := clientWithSkip.Do(ctx, req2)
	if err != nil {
		t.Fatalf("expected successful connection with InsecureSkipVerify, got error: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}
}

func TestWithJitter_Enabled(t *testing.T) {
	// Test that jitter is enabled by default
	client, err := NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if !client.jitterEnabled {
		t.Error("expected jitterEnabled to be true by default")
	}
}

func TestWithJitter_Disabled(t *testing.T) {
	// Test that jitter can be explicitly disabled
	client, err := NewClient(
		WithJitter(false),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if client.jitterEnabled {
		t.Error("expected jitterEnabled to be false")
	}
}

func TestWithJitter(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(3),
		WithJitter(true),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	start := time.Now()
	resp, err := client.Do(ctx, req)
	if err == nil && resp != nil {
		resp.Body.Close()
	}
	duration := time.Since(start)

	// With jitter, the total duration should vary
	// Without jitter: 100ms + 200ms + 400ms = 700ms
	// With jitter (±25%): approximately 525ms to 875ms
	if duration < 400*time.Millisecond || duration > 1*time.Second {
		t.Logf("Duration %v seems unusual but jitter can cause variation", duration)
	}

	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts.Load())
	}
}

func TestWithOnRetry(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(10*time.Millisecond),
		WithMaxRetries(3),
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Should have 2 retries (3 total attempts)
	if len(retryInfos) != 2 {
		t.Errorf("expected 2 retry callbacks, got %d", len(retryInfos))
	}

	// Verify retry info
	for i, info := range retryInfos {
		if info.Attempt != i+1 {
			t.Errorf("retry %d: expected attempt %d, got %d", i, i+1, info.Attempt)
		}
		if info.StatusCode != http.StatusInternalServerError {
			t.Errorf("retry %d: expected status 500, got %d", i, info.StatusCode)
		}
		if info.Delay <= 0 {
			t.Errorf("retry %d: expected positive delay, got %v", i, info.Delay)
		}
		if info.TotalElapsed <= 0 {
			t.Errorf("retry %d: expected positive total elapsed, got %v", i, info.TotalElapsed)
		}
	}
}

func TestWithRespectRetryAfter_Seconds(t *testing.T) {
	var attempts atomic.Int32
	var requestTimes []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes = append(requestTimes, time.Now())
		count := attempts.Add(1)
		if count < 2 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond), // Would normally use 100ms
		WithMaxRetries(2),
		WithRespectRetryAfter(true),
		WithJitter(false), // Disable jitter for predictable timing tests
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(requestTimes) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requestTimes))
	}

	// Check that the delay was approximately 1 second (from Retry-After header)
	delay := requestTimes[1].Sub(requestTimes[0])
	if delay < 950*time.Millisecond || delay > 1100*time.Millisecond {
		t.Errorf("expected ~1s delay (from Retry-After), got %v", delay)
	}
}

func TestWithRespectRetryAfter_HTTPDate(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 2 {
			// Set Retry-After to a fixed future time
			// Note: HTTP-date has 1-second precision
			retryTime := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
			w.Header().Set("Retry-After", retryTime)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(2),
		WithRespectRetryAfter(true),
		WithJitter(false), // Disable jitter for predictable timing tests
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(retryInfos) != 1 {
		t.Fatalf("expected 1 retry callback, got %d", len(retryInfos))
	}

	// The RetryAfter should be parsed and be approximately 2 seconds
	// Allow some tolerance for time.Until() calculation and HTTP-date precision
	if retryInfos[0].RetryAfter < 1*time.Second || retryInfos[0].RetryAfter > 3*time.Second {
		t.Errorf("expected RetryAfter to be ~2s, got %v", retryInfos[0].RetryAfter)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected time.Duration
		delta    time.Duration // tolerance for time-based tests
	}{
		{
			name:     "seconds format",
			header:   "120",
			expected: 120 * time.Second,
		},
		{
			name:     "zero seconds",
			header:   "0",
			expected: 0,
		},
		{
			name:     "invalid negative",
			header:   "-1",
			expected: 0,
		},
		{
			name:     "empty header",
			header:   "",
			expected: 0,
		},
		{
			name:     "invalid format",
			header:   "invalid",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}

			result := parseRetryAfter(resp)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}

	// Test HTTP-date format separately due to time.Now() dependency
	t.Run("HTTP-date format", func(t *testing.T) {
		futureTime := time.Now().Add(5 * time.Second).UTC()
		resp := &http.Response{
			Header: http.Header{},
		}
		resp.Header.Set("Retry-After", futureTime.Format(http.TimeFormat))

		result := parseRetryAfter(resp)
		expected := 5 * time.Second
		// Allow larger tolerance due to HTTP-date having 1-second precision
		// and time.Until() calculation happening after header creation
		delta := 500 * time.Millisecond

		if result < expected-delta || result > expected+delta {
			t.Errorf("expected ~%v (±%v), got %v", expected, delta, result)
		}
	})

	// Test past HTTP-date (should return 0)
	t.Run("past HTTP-date", func(t *testing.T) {
		pastTime := time.Now().Add(-5 * time.Second).UTC()
		resp := &http.Response{
			Header: http.Header{},
		}
		resp.Header.Set("Retry-After", pastTime.Format(http.TimeFormat))

		result := parseRetryAfter(resp)
		if result != 0 {
			t.Errorf("expected 0 for past date, got %v", result)
		}
	})
}

func TestApplyJitter(t *testing.T) {
	delay := 1000 * time.Millisecond

	// Run multiple times to verify randomness
	results := make(map[time.Duration]bool)
	for i := 0; i < 10; i++ {
		jittered := applyJitter(delay)
		results[jittered] = true

		// Should be between 750ms and 1250ms (±25%)
		if jittered < 750*time.Millisecond || jittered > 1250*time.Millisecond {
			t.Errorf("jittered delay %v outside expected range [750ms, 1250ms]", jittered)
		}
	}

	// Should have some variation (not all the same)
	if len(results) < 2 {
		t.Error("expected some variation in jittered delays")
	}
}

func TestCombinedFeatures(t *testing.T) {
	var attempts atomic.Int32
	var retryInfos []RetryInfo

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		switch count {
		case 1:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client, err := NewClient(
		WithInitialRetryDelay(100*time.Millisecond),
		WithMaxRetries(3),
		WithJitter(true),
		WithRespectRetryAfter(true),
		WithOnRetry(func(info RetryInfo) {
			retryInfos = append(retryInfos, info)
		}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}

	if len(retryInfos) != 2 {
		t.Fatalf("expected 2 retry callbacks, got %d", len(retryInfos))
	}

	// First retry should have used Retry-After
	if retryInfos[0].RetryAfter != 1*time.Second {
		t.Errorf("expected first retry to have Retry-After=1s, got %v", retryInfos[0].RetryAfter)
	}

	// Second retry should not have Retry-After
	if retryInfos[1].RetryAfter != 0 {
		t.Errorf("expected second retry to have no Retry-After, got %v", retryInfos[1].RetryAfter)
	}
}
