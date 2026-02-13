# Preset Configurations

The library provides preset configurations optimized for common use cases. Each preset is fully customizable - you can override any setting while keeping the rest of the preset defaults.

## Available Presets

- [NewRealtimeClient](#newrealtimeclient) - User-facing requests with fast response times
- [NewBackgroundClient](#newbackgroundclient) - Background tasks and scheduled jobs
- [NewRateLimitedClient](#newratelimitedclient) - APIs with strict rate limits
- [NewMicroserviceClient](#newmicroserviceclient) - Internal microservice communication
- [NewAggressiveClient](#newaggressiveclient) - Frequent transient failures
- [NewConservativeClient](#newconservativeclient) - Avoiding retry storms
- [NewWebhookClient](#newwebhookclient) - Webhook/callback scenarios
- [NewCriticalClient](#newcriticalclient) - Mission-critical operations
- [NewFastFailClient](#newfastfailclient) - Fast failure scenarios

## NewRealtimeClient

Optimized for user-facing requests that require fast response times, such as interactive UI operations and real-time search.

**Configuration:**

- Max retries: 2 (quick failure for better UX)
- Initial delay: 100ms
- Max delay: 1s
- Per-attempt timeout: 3s

**Use cases:** Search suggestions, user interactions, interactive API calls

```go
client, err := retry.NewRealtimeClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Get(context.Background(), "https://api.example.com/search?q=hello")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewBackgroundClient

Optimized for background tasks and scheduled jobs where reliability is more important than speed.

**Configuration:**

- Max retries: 10 (persistent retries)
- Initial delay: 5s
- Max delay: 60s
- Backoff multiplier: 3.0 (aggressive exponential backoff)
- Per-attempt timeout: 30s
- Jitter: enabled (prevent synchronized retries)

**Use cases:** Batch data sync, scheduled jobs, data export/import

```go
client, err := retry.NewBackgroundClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Post(context.Background(), "https://api.example.com/batch/sync")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewRateLimitedClient

Optimized for APIs with strict rate limits. Respects server-provided `Retry-After` headers and uses jitter to prevent thundering herd problems.

**Configuration:**

- Max retries: 5
- Initial delay: 2s
- Max delay: 30s
- Per-attempt timeout: 15s
- Respects Retry-After header (enabled)
- Jitter (enabled)

**Use cases:** Third-party APIs (GitHub, Stripe, AWS), rate-limited endpoints

```go
client, err := retry.NewRateLimitedClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Get(context.Background(), "https://api.github.com/users/appleboy")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewMicroserviceClient

Optimized for internal microservice communication within the same network (e.g., Kubernetes cluster).

**Configuration:**

- Max retries: 3
- Initial delay: 50ms
- Max delay: 500ms
- Per-attempt timeout: 2s
- Jitter (enabled)

**Use cases:** Kubernetes pod-to-pod communication, internal service calls, low-latency internal APIs

```go
client, err := retry.NewMicroserviceClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Get(context.Background(), "http://user-service:8080/users/123")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewAggressiveClient

Optimized for scenarios with frequent transient failures, attempting many retries with short delays.

**Configuration:**

- Max retries: 10 (many retry attempts)
- Initial delay: 100ms
- Max delay: 5s
- Per-attempt timeout: 10s
- Jitter: enabled (prevent synchronized retries)

**Use cases:** Highly unreliable networks, services with frequent transient failures

```go
client, err := retry.NewAggressiveClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Get(context.Background(), "https://unreliable-api.example.com/data")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewConservativeClient

Conservative approach with fewer retries and longer delays to avoid retry storms.

**Configuration:**

- Max retries: 2
- Initial delay: 5s
- Per-attempt timeout: 20s
- Jitter: enabled (prevent synchronized retries)

**Use cases:** Preventing retry storms, expensive operations, external APIs with strict limits

```go
client, err := retry.NewConservativeClient()
if err != nil {
    log.Fatal(err)
}

resp, err := client.Post(context.Background(), "https://api.example.com/expensive-operation")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

## NewWebhookClient

Optimized for webhook/callback scenarios where the sender typically has its own retry mechanism, so quick failure is preferred over aggressive retries.

**Configuration:**

- Max retries: 1 (single retry attempt)
- Initial delay: 500ms
- Max delay: 1s
- Per-attempt timeout: 5s
- Jitter: enabled (prevent synchronized retries)

**Use cases:** Sending webhooks to external services, third-party integration callbacks, event notification systems

```go
client, err := retry.NewWebhookClient()
if err != nil {
    log.Fatal(err)
}

// Send webhook with quick failure
webhookPayload := bytes.NewReader([]byte(`{"event":"user.created","data":{"id":123}}`))
resp, err := client.Post(context.Background(), "https://webhook.example.com/events",
    retry.WithBody("application/json", webhookPayload))
if err != nil {
    log.Printf("Webhook delivery failed: %v", err)
    // Consider queuing for later delivery
    return
}
defer resp.Body.Close()
```

## NewCriticalClient

Optimized for mission-critical operations that absolutely must complete, such as payment processing or critical data synchronization.

**Configuration:**

- Max retries: 15 (many retry attempts for maximum reliability)
- Initial delay: 1s
- Max delay: 120s (up to 2 minutes between retries)
- Backoff multiplier: 2.0 (standard exponential backoff)
- Per-attempt timeout: 60s
- Jitter: enabled (prevent synchronized retries)
- Respects Retry-After header (enabled)

**Use cases:** Payment processing, order confirmation, critical data synchronization, operations that cannot fail

```go
client, err := retry.NewCriticalClient()
if err != nil {
    log.Fatal(err)
}

// Process payment with maximum retry effort
paymentData := bytes.NewReader([]byte(`{"amount":1000,"currency":"USD"}`))
resp, err := client.Post(context.Background(), "https://api.payment-gateway.com/charge",
    retry.WithBody("application/json", paymentData),
    retry.WithHeader("Authorization", "Bearer token"),
    retry.WithHeader("Idempotency-Key", uuid.New().String()))
if err != nil {
    log.Fatalf("Critical payment operation failed: %v", err)
}
defer resp.Body.Close()
```

## NewFastFailClient

Optimized for fast failure scenarios where you need to know about failures quickly, such as health checks or service discovery.

**Configuration:**

- Max retries: 1 (single retry attempt)
- Initial delay: 50ms (minimal delay)
- Max delay: 200ms (very short maximum)
- Per-attempt timeout: 1s
- Jitter: enabled (prevent synchronized retries)

**Use cases:** Health checks, service discovery, quick availability probes, circuit breaker implementations

```go
client, err := retry.NewFastFailClient()
if err != nil {
    log.Fatal(err)
}

// Quick health check with fast failure
resp, err := client.Get(context.Background(), "http://service:8080/health")
if err != nil {
    log.Printf("Service unhealthy: %v", err)
    // Open circuit breaker
    return
}
defer resp.Body.Close()

if resp.StatusCode == http.StatusOK {
    log.Println("Service is healthy")
}
```

## Customizing Presets

All preset defaults can be overridden:

```go
// Start with realtime preset but use more retries
client, err := retry.NewRealtimeClient(
    retry.WithMaxRetries(5),                           // Override: more retries
    retry.WithInitialRetryDelay(50*time.Millisecond),  // Override: faster retry
)
if err != nil {
    log.Fatal(err)
}

// Other preset settings remain unchanged
```
