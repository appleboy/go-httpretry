# Large File Upload Example

This example demonstrates the **correct** way to upload large files with retry support, and highlights common mistakes to avoid.

## The Problem

The convenience methods like `WithBody()` and `WithJSON()` buffer the **entire request body in memory** to support retries. This is perfect for small payloads (JSON API requests), but **disastrous** for large files.

### Memory Usage Comparison

```go
// ❌ WRONG: Uploads a 100MB file
// Memory used: 100MB+ (entire file buffered in memory)
largeFile := make([]byte, 100*1024*1024) // 100MB
client.Post(ctx, url, retry.WithBody("application/octet-stream",
    bytes.NewReader(largeFile)))
```

```go
// ✅ RIGHT: Uploads a 100MB file
// Memory used: ~32KB (streaming with retry support)
file, _ := os.Open("100mb-file.dat")
req, _ := http.NewRequestWithContext(ctx, "POST", url, file)
req.GetBody = func() (io.ReadCloser, error) {
    return os.Open("100mb-file.dat") // Reopen for each retry
}
client.Do(req)
```

## When to Use What

### Use `WithBody()` or `WithJSON()` when

- ✅ Uploading small JSON/XML data (<1MB)
- ✅ Simple API requests
- ✅ Data already in memory
- ✅ Convenience is more important than memory

### Use `Do()` with `GetBody` when

- ✅ Uploading files >10MB
- ✅ Uploading from disk
- ✅ Streaming data
- ✅ Memory efficiency is critical
- ✅ Very large JSON documents

## Examples in This Demo

### Example 1: Wrong Way ❌

Shows why you shouldn't use `WithBody()` for large files.

### Example 2: Right Way - In-Memory Data ✅

Demonstrates using `Do()` with `GetBody` for large in-memory data.

### Example 3: Right Way - File Upload ✅

The most common pattern: uploading from a file on disk.

**Key Points:**

- Open file initially for the first request
- Set `req.GetBody` to reopen the file for each retry
- File is streamed, not buffered in memory

### Example 4: Right Way - Custom Body Generator ✅

Advanced pattern for when you need custom logic to regenerate the body.

**Use Cases:**

- Fetching data from database for each attempt
- Generating data on-the-fly
- Multi-part uploads with boundary regeneration

## Running the Example

```bash
go run main.go
```

The example creates a local test server, so no internet connection is needed.

## Key Takeaways

1. **`WithBody()` buffers everything in memory** - only use for small payloads
2. **Always set `req.GetBody`** when uploading large files to enable retries
3. **For files, reopen them** in `GetBody` - don't seek to beginning (may not work for all sources)
4. **Test with realistic file sizes** in development to catch memory issues early

## Real-World Pattern

```go
func uploadLargeFile(client *retry.Client, filePath string) error {
    ctx := context.Background()

    // Open file and get size
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }

    stat, err := file.Stat()
    if err != nil {
        file.Close()
        return err
    }

    // Create request
    req, err := http.NewRequestWithContext(ctx, "POST",
        "https://api.example.com/upload", file)
    if err != nil {
        file.Close()
        return err
    }

    req.Header.Set("Content-Type", "application/octet-stream")
    req.ContentLength = stat.Size()

    // CRITICAL: Enable retries by setting GetBody
    req.GetBody = func() (io.ReadCloser, error) {
        return os.Open(filePath)
    }

    // Execute with retry support
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    // Handle response...
    return nil
}
```

## Performance Tips

1. **Set `Content-Length`**: Helps servers allocate resources efficiently
2. **Use appropriate buffer sizes**: Default is usually fine, but you can tune for your use case
3. **Consider compression**: For text files, compress before upload
4. **Implement progress tracking**: Use `io.TeeReader` to track upload progress
5. **Handle cleanup**: Always close files and response bodies

## Common Mistakes

### ❌ Seeking instead of reopening

```go
// DON'T: This won't work for all io.Reader types
req.GetBody = func() (io.ReadCloser, error) {
    if seeker, ok := file.(io.Seeker); ok {
        seeker.Seek(0, 0)
    }
    return file, nil
}
```

### ✅ Always reopen

```go
// DO: Always reopen/regenerate
req.GetBody = func() (io.ReadCloser, error) {
    return os.Open(filePath)
}
```

### ❌ Forgetting to set GetBody

```go
// DON'T: Retries will fail with empty body
file, _ := os.Open(filePath)
req, _ := http.NewRequestWithContext(ctx, "POST", url, file)
client.Do(req) // Retries won't work!
```

### ✅ Always set GetBody for retryable uploads

```go
// DO: Enable retries
file, _ := os.Open(filePath)
req, _ := http.NewRequestWithContext(ctx, "POST", url, file)
req.GetBody = func() (io.ReadCloser, error) {
    return os.Open(filePath)
}
client.Do(req) // Retries will work!
```

## See Also

- [Basic Example](../basic) - Start here if you're new
- [Request Options](../request_options) - Learn about `WithBody()`, `WithJSON()`, etc.
- [Advanced Example](../advanced) - Production-ready patterns
