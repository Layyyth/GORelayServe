# Claude Code + Together AI Integration

A Go-based relay server that tricks Claude Code CLI into using Together AI models by translating Anthropic's API format to OpenAI format.

## What We Built

A proxy server that:
- Exposes Anthropic-compatible endpoints (`/v1/messages`, `/v1/models`)
- Translates requests to OpenAI format for Together AI
- Maps Claude model names to Together AI models
- Returns responses in Anthropic format

## The Challenge

Claude Code CLI has strict client-side validation:
1. Validates model names against a hardcoded list before making API calls
2. Expects `/v1/messages` endpoint (not `/v1/chat/completions`)
3. Uses Anthropic's message format (different from OpenAI)

## How We Tricked Claude Code

### 1. Base URL Without `/v1`
```bash
# WRONG: Claude Code appends /v1/messages, resulting in /v1/v1/messages
export ANTHROPIC_BASE_URL="http://localhost:8080/v1"

# CORRECT: Let Claude Code add the /v1 prefix
export ANTHROPIC_BASE_URL="http://localhost:8080"
```

### 2. Model Name Mapping
Claude Code validates model names client-side, so we use names it recognizes:
```go
var modelMapping = map[string]string{
    "claude-sonnet-4-20250514": "Qwen/Qwen3-Next-80B-A3B-Instruct",
    // ... other mappings
}
```

The CLI thinks it's using "Sonnet 4" but actually hits Qwen via Together AI.

### 3. System Field Handling
Claude Code sends `system` as an **array** of content blocks, not a string:
```json
"system": [
  {"type": "text", "text": "You are Claude Code..."},
  {"type": "text", "text": "Additional instructions..."}
]
```

We parse this and concatenate the text content:
```go
switch s := anthropicReq.System.(type) {
case string:
    systemContent = s
case []interface{}:
    // Concatenate all text blocks
}
```

### 4. Request/Response Translation

**Anthropic → OpenAI:**
- Model name mapping
- Message format conversion
- Tool schema conversion

**OpenAI → Anthropic:**
- Content block conversion
- Usage stats mapping
- Tool call format conversion

### 5. Gzip Handling
Together AI returns gzip-compressed responses by default. We disable this:
```go
req.Header.Set("Accept-Encoding", "identity")
```

## Configuration

### Environment Variables
```bash
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_MODEL="claude-sonnet-4-20250514"
export ANTHROPIC_API_KEY="sk-ant-api03-dummy-key"
```

### Docker Compose
```yaml
relay:
  build: .
  ports:
    - "8080:8080"
  environment:
    - LLM_PROVIDER_URL=https://api.together.xyz
    - LLM_PROVIDER_KEY=${TOGETHER_API_KEY}
```

## Endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /v1/models` | Model list for Claude Code validation |
| `POST /v1/messages` | Chat completions (Anthropic format) |
| `POST /v1/chat/completions` | Standard OpenAI endpoint |

## Claude Code Detection

The relay detects Claude Code via User-Agent and skips rule injection (since Claude Code reads CLAUDE.md locally):
```go
func isClaudeCodeRequest(r *http.Request) bool {
    return strings.Contains(r.UserAgent(), "claude-cli") || 
           r.Header.Get("anthropic-version") != ""
}
```

## Result

Claude Code now works seamlessly with Together AI models, thinking it's talking to Anthropic's API while actually using Qwen models at a fraction of the cost.

```
▐▛███▜▌   Claude Code v2.1.71
▝▜█████▛▘  Sonnet 4 · API Usage Billing  ← Thinks it's Sonnet 4
  ▘▘ ▝▝    ~/Documents/GoRelayServe

❯ hello

● Hello! How can I assist you today?  ← Actually Qwen via Together AI
```

## Architecture

```
Claude Code CLI
      │
      │ POST /v1/messages (Anthropic format)
      ▼
  Go Relay Server
      │
      │ 1. Parse Anthropic request
      │ 2. Map model name
      │ 3. Convert to OpenAI format
      │ 4. Forward to Together AI
      ▼
   Together AI
      │
      │ OpenAI format response
      ▼
  Go Relay Server
      │
      │ Convert to Anthropic format
      ▼
Claude Code CLI (thinks it's Anthropic)
```
