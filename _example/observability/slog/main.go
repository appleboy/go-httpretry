package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	retry "github.com/appleboy/go-httpretry"
)

func main() {
	// Create a structured logger using slog
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create a test server that fails twice then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Temporary failure (attempt %d)", attempts)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Success!")
	}))
	defer server.Close()

	// Create retry client with slog adapter
	client, err := retry.NewClient(
		retry.WithMaxRetries(5),
		retry.WithInitialRetryDelay(100*time.Millisecond),
		retry.WithJitter(false), // Disable for predictable demo output
		retry.WithLogger(retry.NewSlogAdapter(logger)),
	)
	if err != nil {
		logger.Error("failed to create client", "error", err)
		os.Exit(1)
	}

	// Make request
	ctx := context.Background()
	logger.Info("=== Starting HTTP request with retry and logging ===")

	resp, err := client.Get(ctx, server.URL)
	if err != nil {
		logger.Error("request failed", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	logger.Info("=== Request completed successfully ===",
		"status", resp.StatusCode,
		"total_attempts", attempts,
	)
}
