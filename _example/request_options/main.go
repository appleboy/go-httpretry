package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"

	retry "github.com/appleboy/go-httpretry"
)

func main() {
	client, err := retry.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Using WithBody with Content-Type
	fmt.Println("=== Example 1: WithBody with Content-Type ===")
	jsonData := `{"message":"Hello, World!"}`
	resp, err := client.Post(ctx, "https://httpbin.org/post",
		retry.WithBody("application/json", strings.NewReader(jsonData)))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 2: Using WithBody without Content-Type
	fmt.Println("=== Example 2: WithBody without Content-Type ===")
	resp, err = client.Post(ctx, "https://httpbin.org/post",
		retry.WithBody("", strings.NewReader("plain text data")))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 3: Using WithHeader for single header
	fmt.Println("=== Example 3: WithHeader for Authentication ===")
	resp, err = client.Get(ctx, "https://httpbin.org/headers",
		retry.WithHeader("Authorization", "Bearer fake-token-12345"))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 4: Using WithHeaders for multiple headers
	fmt.Println("=== Example 4: WithHeaders for Multiple Headers ===")
	resp, err = client.Get(ctx, "https://httpbin.org/headers",
		retry.WithHeaders(map[string]string{
			"X-Request-ID":  "req-12345",
			"X-API-Version": "v2",
			"Accept":        "application/json",
		}))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 5: Using WithJSON for automatic JSON marshaling
	fmt.Println("=== Example 5: WithJSON for Automatic Marshaling ===")
	type RequestPayload struct {
		Action string `json:"action"`
		UserID int    `json:"user_id"`
	}
	payload := RequestPayload{Action: "update", UserID: 42}
	resp, err = client.Post(ctx, "https://httpbin.org/post",
		retry.WithJSON(payload))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 6: Combining multiple options
	fmt.Println("=== Example 6: Combining Multiple Options ===")
	resp, err = client.Post(ctx, "https://httpbin.org/post",
		retry.WithBody("application/json", bytes.NewReader([]byte(jsonData))),
		retry.WithHeader("Authorization", "Bearer token"),
		retry.WithHeader("X-Request-ID", "req-67890"),
		retry.WithHeader("User-Agent", "go-httpretry-example"))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n\n", resp.StatusCode)
	}

	// Example 7: WithJSON combined with other options
	fmt.Println("=== Example 7: WithJSON with Headers ===")
	resp, err = client.Put(ctx, "https://httpbin.org/put",
		retry.WithJSON(payload),
		retry.WithHeader("Authorization", "Bearer secret-token"),
		retry.WithHeader("X-Idempotency-Key", "unique-123"))
	if err != nil {
		log.Printf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("Status: %d\n", resp.StatusCode)
	}
}
