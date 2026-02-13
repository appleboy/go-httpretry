package main

import (
	"context"
	"fmt"
	"log"

	retry "github.com/appleboy/go-httpretry"
)

func main() {
	// Create a retry client with default settings:
	// - 3 max retries
	// - 1 second initial delay
	// - 10 second max delay
	// - 2.0x exponential multiplier
	// - Jitter enabled (±25% randomization)
	// - Retry-After header respected
	client, err := retry.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Simple GET request
	resp, err := client.Get(ctx, "https://httpbin.org/status/200")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response status: %d\n", resp.StatusCode)
}
