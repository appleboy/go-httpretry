package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

func main() {
	// Create a test server that simulates upload endpoints
	server := createTestServer()
	defer server.Close()

	fmt.Println("=== Large File Upload Examples ===")

	// Example 1: WRONG WAY - Using WithBody for large files
	fmt.Println("Example 1: ❌ WRONG - Using WithBody for large files")
	fmt.Println("(This buffers the entire file in memory!)")
	wrongWayExample(server.URL)

	// Example 2: RIGHT WAY - Using Do() with GetBody for large files
	fmt.Println("\nExample 2: ✅ RIGHT - Using Do() with GetBody for large files")
	rightWayExample(server.URL)

	// Example 3: Uploading from a file that can be reopened
	fmt.Println("\nExample 3: ✅ RIGHT - Uploading from a file with retry support")
	fileUploadExample(server.URL)

	// Example 4: Streaming upload with custom GetBody
	fmt.Println("\nExample 4: ✅ RIGHT - Streaming upload with custom logic")
	streamingUploadExample(server.URL)
}

// wrongWayExample demonstrates the INCORRECT way to upload large files.
// This will buffer the entire file in memory, which is not suitable for large files.
func wrongWayExample(baseURL string) {
	client, err := retry.NewClient(
		retry.WithMaxRetries(2),
		retry.WithInitialRetryDelay(100*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create a large payload (simulating a 10MB file)
	largeData := bytes.Repeat([]byte("x"), 10*1024*1024) // 10MB

	ctx := context.Background()

	// ❌ WRONG: This will buffer all 10MB in memory!
	// Don't use WithBody for large files.
	resp, err := client.Post(ctx, baseURL+"/upload",
		retry.WithBody("application/octet-stream", bytes.NewReader(largeData)))

	if err != nil {
		log.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("  Status: %d (but used %d MB of memory!)\n", resp.StatusCode, len(largeData)/(1024*1024))
}

// rightWayExample demonstrates the CORRECT way to upload large files.
// Uses Do() method with a custom GetBody function.
func rightWayExample(baseURL string) {
	client, err := retry.NewClient(
		retry.WithMaxRetries(2),
		retry.WithInitialRetryDelay(100*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Create a large payload (simulating a large file)
	largeData := bytes.Repeat([]byte("x"), 10*1024*1024) // 10MB

	// ✅ CORRECT: Use Do() with a custom GetBody function
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/upload", nil)
	if err != nil {
		log.Fatal(err)
	}

	// Set the initial body
	req.Body = io.NopCloser(bytes.NewReader(largeData))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(largeData))

	// IMPORTANT: Set GetBody to allow retries
	// This function is called for each retry to get a fresh body reader
	req.GetBody = func() (io.ReadCloser, error) {
		// For each retry, create a new reader from the same data
		// In a real scenario, you might reopen a file here
		return io.NopCloser(bytes.NewReader(largeData)), nil
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("  ✅ Status: %d (memory efficient with retry support)\n", resp.StatusCode)
}

// fileUploadExample demonstrates uploading from a real file.
// This is the most common real-world scenario.
func fileUploadExample(baseURL string) {
	client, err := retry.NewClient(
		retry.WithMaxRetries(2),
		retry.WithInitialRetryDelay(100*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create a temporary file to upload
	tmpFile, err := os.CreateTemp("", "upload-test-*.dat")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up

	// Write some data to the file
	testData := bytes.Repeat([]byte("test data "), 1024*100) // ~1MB
	if _, err := tmpFile.Write(testData); err != nil {
		log.Fatal(err)
	}
	tmpFile.Close()

	filePath := tmpFile.Name()

	ctx := context.Background()

	// ✅ CORRECT: Open file, set GetBody to reopen for retries
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close() // Ensure file is closed after request completes

	stat, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/upload", file)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = stat.Size()

	// CRITICAL: Set GetBody to reopen the file for each retry
	req.GetBody = func() (io.ReadCloser, error) {
		// Reopen the file for each retry
		return os.Open(filePath)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("  ✅ Status: %d (uploaded %d KB from file with retry support)\n",
		resp.StatusCode, stat.Size()/1024)
}

// streamingUploadExample demonstrates a custom upload with GetBody.
// This shows how to handle more complex scenarios.
func streamingUploadExample(baseURL string) {
	client, err := retry.NewClient(
		retry.WithMaxRetries(2),
		retry.WithInitialRetryDelay(100*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Function to generate body content (called for initial request and each retry)
	generateBody := func() (io.ReadCloser, error) {
		// In a real scenario, this might:
		// - Reopen a file
		// - Regenerate data from a database query
		// - Fetch from another API
		data := []byte("Generated upload data")
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	// Create initial body
	body, err := generateBody()
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/upload", body)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.GetBody = generateBody // Set GetBody for retries

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("  ✅ Status: %d (custom body generator with retry support)\n", resp.StatusCode)
}

// createTestServer creates a test HTTP server that simulates file uploads
func createTestServer() *httptest.Server {
	attemptCount := 0

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++

		// Read and discard the body (simulating upload processing)
		_, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Always succeed for this demo
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Upload successful (attempt %d)", attemptCount)
	}))
}
