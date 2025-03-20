# Go-Ichiran Development Guide

## Build & Test Commands
- Run tests: `ICHIRAN_MANUAL_TEST=1 go test ./...`
- Run specific test: `ICHIRAN_MANUAL_TEST=1 go test -run TestName`
- Start containers only: `docker compose up`
- Force container rebuild: Use `InitRecreate(ctx, true)` in code
- Format code: `gofmt -w *.go`
- Lint code: `go vet ./...`

## Code Style Guidelines
- Format with gofmt
- Errors: Use `fmt.Errorf("message: %w", err)` for error wrapping
- Logging: Use zerolog package (`github.com/rs/zerolog`)
- Variable naming: camelCase for private, PascalCase for exported
- Struct fields alignment: Align adjacent field names and tags
- Error handling: Check all errors, don't use panic
- Documentation: Add doc comments for all exported functions/types
- Imports: Group standard library, third-party, and local imports
- Testing: Use testify/assert for assertions
- Constants: Use package-level const/var blocks for related values
- Unicode handling: Use unescapeUnicodeString for string processing

## Ichiran Lisp Integration
- Execute Lisp code via `docker exec -it ichiran-main-1 ichiran-cli -e '(expression)'`
- Always escape user input with shellescape.Quote
- Extract JSON from output using extractJSONFromDockerOutput to handle warnings
- For complex operations, chain multiple Lisp expressions in a single call

## Modern API Usage (Multiple Instances Support)
```go
// Create a new Ichiran manager with custom settings
ctx := context.Background()
manager, err := ichiran.NewManager(ctx, 
    ichiran.WithProjectName("ichiran-custom"),
    ichiran.WithQueryTimeout(10 * time.Minute))
if err != nil {
    log.Fatal(err)
}

// Initialize the Docker container
if err := manager.Init(ctx); err != nil {
    log.Fatal(err)
}

// Analyze text using the manager
result, err := manager.Analyze(ctx, "こんにちは")
if err != nil {
    log.Fatal(err)
}

// Clean up when done
defer manager.Close()
```

## Code management
- do not git add or revert go.mod or go.sum
- do not git diff or git pull
- do not write commit messages in the "convential commit" style
- you must briefly mention all noteworthy changes within the "main" message of git commit and separate them using semicolons.

## Context Guidelines
- Always pass context as first parameter to functions
- Use timeout-wrapped contexts for external API calls
- Never store context in struct fields
- For long-running operations, derive child contexts with appropriate timeouts