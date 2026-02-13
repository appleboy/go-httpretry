package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func main() {
	// Create retry client with custom settings
	client, err := retry.NewClient(
		retry.WithMaxRetries(3),
		retry.WithInitialRetryDelay(500*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Example 1: Simple GET request
	fmt.Println("=== Example 1: GET Request ===")
	resp, err := client.Get(ctx, "https://httpbin.org/get")
	if err != nil {
		log.Printf("GET request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("GET Status: %d\n\n", resp.StatusCode)
	}

	// Example 2: POST request with JSON body
	fmt.Println("=== Example 2: POST Request with JSON ===")
	user := User{Name: "John Doe", Email: "john@example.com"}
	jsonData, _ := json.Marshal(user)

	resp, err = client.Post(ctx, "https://httpbin.org/post",
		retry.WithBody("application/json", bytes.NewReader(jsonData)))
	if err != nil {
		log.Printf("POST request failed: %v", err)
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("POST Status: %d\n", resp.StatusCode)
		fmt.Printf("Response: %s\n\n", string(body))
	}

	// Example 3: PUT request with headers
	fmt.Println("=== Example 3: PUT Request with Headers ===")
	resp, err = client.Put(ctx, "https://httpbin.org/put",
		retry.WithBody("application/json", bytes.NewReader(jsonData)),
		retry.WithHeader("X-Custom-Header", "custom-value"))
	if err != nil {
		log.Printf("PUT request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("PUT Status: %d\n\n", resp.StatusCode)
	}

	// Example 4: DELETE request
	fmt.Println("=== Example 4: DELETE Request ===")
	resp, err = client.Delete(ctx, "https://httpbin.org/delete")
	if err != nil {
		log.Printf("DELETE request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("DELETE Status: %d\n\n", resp.StatusCode)
	}

	// Example 5: HEAD request
	fmt.Println("=== Example 5: HEAD Request ===")
	resp, err = client.Head(ctx, "https://httpbin.org/get")
	if err != nil {
		log.Printf("HEAD request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("HEAD Status: %d\n", resp.StatusCode)
		fmt.Printf("Content-Type: %s\n\n", resp.Header.Get("Content-Type"))
	}

	// Example 6: PATCH request
	fmt.Println("=== Example 6: PATCH Request ===")
	patchData := []byte(`{"name":"Jane Doe"}`)
	resp, err = client.Patch(ctx, "https://httpbin.org/patch",
		retry.WithBody("application/json", bytes.NewReader(patchData)))
	if err != nil {
		log.Printf("PATCH request failed: %v", err)
	} else {
		defer resp.Body.Close()
		fmt.Printf("PATCH Status: %d\n", resp.StatusCode)
	}
}
