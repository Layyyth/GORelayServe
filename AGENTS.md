# AGENTS.md - Development Guidelines for GORelayServe

An OpenAI-compatible API relay server with intelligent context management for long coding sessions.

## Build & Run Commands

### Build the Application
```bash
go build -o relay ./cmd/relay/main.go
```

### Run the Server
```bash
./relay
# Or with environment variables:
LLM_PROVIDER_URL=https://api.together.xyz LLM_PROVIDER_KEY=your_key ./relay
```

### Run with Docker
```bash
docker build -t gorelayserve:latest .
docker run -d -p 8080:8080 -e LLM_PROVIDER_URL=https://api.together.xyz -e LLM_PROVIDER_KEY=your_key gorelayserve:latest
docker compose up -d
```

### Run Tests
```bash
go test ./...                  # Run all tests
go test -v ./...               # Verbose output
go test -v ./internal/proxy    # Specific package
go test -v -run TestName ./... # Single test function
go test -cover ./...          # With coverage
```

### Lint & Format
```bash
go fmt ./...   # Format code
go vet ./...   # Vet code
go mod tidy    # Tidy dependencies
golangci-lint run  # Lint (if installed)
```

## Code Style Guidelines

### Go Version
- **Go 1.25**

### Imports
Standard library first, then third-party, with blank line between groups:
```go
import (
    "encoding/json"
    "log"
    "net/http"
    "net/http/httputil"
    "net/url"
    "os"
    "time"

    "github.com/joho/godotenv"
)
```

### Formatting
- Use `gofmt` for automatic formatting
- 4-space indentation, lines under 100 characters
- No trailing whitespace

### Naming Conventions
- **Functions/Variables:** CamelCase (`NewRelayProxy`, `baseURL`)
- **Unexported:** lowercase (`estimateTokens`, `truncateLargeMessages`)
- **Files:** lowercase with underscores (`proxy.go`, `proxy_test.go`)
- **Constants:** `MaxMsgTokens = 20000`

### Types
- `string` for text, `bool` for booleans
- `int` for integers (use `int64` when needed)
- `map[string]interface{}` for dynamic JSON objects
- `[]interface{}` for dynamic JSON arrays

### Error Handling
Always check errors and return/propagate:
```go
if err != nil {
    return nil, err
}
```
For HTTP errors, return JSON:
```go
http.Error(w, `{"error": "invalid json"}`, http.StatusBadRequest)
```

### Logging
Use structured tags in brackets:
```go
log.Printf("[REQUEST] %s -> %s", originalModel, defaultModel)
log.Printf("[TRUNCATE] Context %d tokens > 180k", totalTokens)
log.Printf("[ERROR] Backend request failed: %v", err)
```
Tags: `[REQUEST]`, `[TRUNCATE]`, `[ERROR]`, `[TOKENS]`, `[STREAM]`, `[ADJUST]`

### HTTP Server Patterns
Use `http.HandlerFunc` with appropriate timeouts:
```go
server := &http.Server{
    ReadTimeout:  30 * time.Second,
    WriteTimeout: 5 * time.Minute,
}
```
Set response headers before writing status.

### Streaming Support
Set SSE headers:
```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
```
Use `http.Flusher` and `bufio.Scanner` for SSE.

### Middleware Pattern
```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Pre-processing
        next.ServeHTTP(w, r)
        // Post-processing
    })
}
```

### Configuration
Required environment variables:
- `LLM_PROVIDER_URL` - LLM provider endpoint
- `LLM_PROVIDER_KEY` - API key
- `RELAY_PORT` - Server port (default: 8080)

Use `.env` files with `godotenv`.

### Context Management Constants (in internal/proxy/proxy.go)
- `maxMsgTokens = 20000` (per message)
- Context threshold: `180000` tokens
- Messages to keep: 8 (system + last 7)
- Default model: `"MiniMaxAI/MiniMax-M2.5"`

### Testing Guidelines
Test files: `*_test.go` in same package. Use table-driven tests:
```go
func TestEstimateTokens(t *testing.T) {
    tests := []struct {
        name     string
        messages []interface{}
        want     int
    }{
        {"empty", []interface{}{}, 0},
        {"simple", []interface{}{map[string]interface{}{"content": "hello"}}, 1},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := estimateTokens(tt.messages); got != tt.want {
                t.Errorf("estimateTokens() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Project Structure
```
GORelayServe/
├── cmd/relay/main.go      # Entry point
├── internal/proxy/proxy.go # Core logic
├── .env.example           # Environment template
├── Dockerfile            # Container build
├── docker-compose.yml    # Docker Compose
├── go.mod                # Go module
└── README.md             # Documentation
```

### API Endpoints
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completions (OpenAI-compatible) |
| `/health` | GET | Health check (returns `ok`) |

### Dependencies
- **github.com/joho/godotenv** - Environment variable loading

### Adding New Dependencies
```bash
go get github.com/package/name
go mod tidy
```

### Security
- Never log API keys or secrets
- Client sends dummy key; relay injects real provider key
- Run containers as non-root user (alpine base)
