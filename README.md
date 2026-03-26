# LLM Relay Server

An **OpenAI-compatible API relay server** with intelligent context management for long coding sessions. Works with any LLM provider (Together AI, OpenAI, Anthropic, etc.) and any model. Prevents context overflow errors while providing middleware capabilities like authentication, logging, and rate limiting.

> **Default Configuration:** This relay ships with [MiniMaxAI/MiniMax-M2.5](https://www.together.ai/models/MiniMaxAI-MiniMax-M2.5) via [Together AI](https://www.together.ai/) as the default model, but **can be configured to use any model** by changing one line of code.

## Why This Relay?

### The Problem

Modern AI coding assistants (like [OpenCode](https://opencode.ai/)) maintain long conversation histories. When these exceed model context limits (~100k-200k tokens depending on model), one of three things happen:

1. **Hard failure** - Request rejected with "context too long" error
2. **Silent truncation** - Provider drops old messages without warning  
3. **Expensive context loss** - Every request reprocesses full history from scratch

### The Solution

This relay acts as a **model-agnostic middleware layer** between your AI client and any LLM provider, providing:

**Request Management:**
- **Authentication handling** - Securely manages API keys, client sends "dummy", relay adds real provider key
- **Rate limiting foundation** - Extensible middleware structure for adding custom rate limits
- **Structured logging** - All requests logged with timestamps, token counts, and truncation events
- **Model mapping** - Any OpenAI model name maps to your configured backend model

**Context Intelligence:**
- **Real-time context monitoring** - Estimates token count on every request
- **Smart truncation** - Keeps system prompt + last 7 messages when approaching limits
- **Message size protection** - Caps individual messages at 20k tokens
- **Streaming support** - Full SSE streaming with proper error handling

**Result:** Long coding sessions continue uninterrupted regardless of which model you use.

**Result:** Long coding sessions continue uninterrupted, with Together AI's [Cache-Aware Disaggregation](https://www.together.ai/blog/cache-aware-disaggregated-inference) providing 80% cost savings on repeated context via their KV-cache reuse.

## Architecture

```
┌─────────────┐      ┌─────────────────┐      ┌─────────────────┐      ┌─────────────┐
│   OpenCode  │ ───► │   Go Relay      │ ───► │   LLM Provider  │ ───► │   Any LLM   │
│   (Client)  │      │   (This Server) │      │  (Together AI,  │      │  (MiniMax,  │
└─────────────┘      └─────────────────┘      │   OpenAI, etc)  │      │  GPT-4, etc)│
                           │                  └─────────────────┘      └─────────────┘
                           │
                           ├─ 1. Receive OpenAI-format request
                           ├─ 2. Authentication & validation
                           ├─ 3. Estimate token count
                           ├─ 4. Truncate if >180k tokens
                           ├─ 5. Cap individual messages >20k
                           ├─ 6. Structured logging
                           ├─ 7. Map model name → Configured backend
                           └─ 8. Forward to LLM provider
```

### Middleware Layer Capabilities

The relay acts as a **model-agnostic secure middleware layer** providing:

| Feature | Description | Benefit |
|---------|-------------|---------|
| **Authentication** | Client uses dummy key, relay injects real provider key | Secure credential management |
| **Request Logging** | Structured logs: method, path, tokens, truncations | Observability & debugging |
| **Rate Limiting** | Extensible middleware (add your own limits) | Abuse prevention |
| **Context Guardrails** | Enforces 180k total, 20k per-message limits | Prevents context overflow errors |
| **Model Normalization** | Maps any model name to configured backend | OpenAI-compatible drop-in |
| **Streaming Proxy** | Full SSE support with error handling | Real-time responses |
| **Health Monitoring** | `/health` endpoint for load balancers | Production readiness |

### Context Management Flow

```
Request with 200 messages (~250k tokens)
              │
              ▼
    ┌──────────────────┐
    │  Per-message     │  Messages >20k tokens truncated
    │  Truncation      │  "[...content truncated]"
    └──────────────────┘
              │
              ▼
    ┌──────────────────┐
    │  Context Check   │  Total >180k tokens?
    │                  │  Yes → Keep system + last 7
    └──────────────────┘  No  → Pass through
              │
              ▼
    ┌──────────────────┐
    │  Together AI     │  CPD automatically caches
    │  CPD System      │  repeated context prefixes
    └──────────────────┘
              │
              ▼
    Streaming response back to client
```

## Quick Start

### 1. Get Together AI API Key

Sign up at [together.ai](https://www.together.ai/) and create an API key.

### 2. Configure Environment

```bash
cp .env.example .env
# Edit .env:
# LLM_PROVIDER_URL=https://api.together.xyz
# LLM_PROVIDER_KEY=your_together_api_key_here
```

### 3. Run with Docker

```bash
docker run -d \
  -p 8080:8080 \
  -e LLM_PROVIDER_URL=https://api.together.xyz \
  -e LLM_PROVIDER_KEY=your_key \
  laithaj/gorelayserve:latest
```

Or use docker-compose:

```bash
docker compose up -d
```

### 4. Configure OpenCode

Add to `~/.opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "go-relay": {
      "name": "Go Relay (Together AI)",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "dummy"
      },
      "models": {
        "gpt-4": {
          "name": "MiniMax M2.5",
          "options": { "model": "gpt-4" },
          "limit": { "context": 196608, "output": 8192 }
        }
      }
    }
  },
  "model": "go-relay/gpt-4"
}
```

### 5. Use

```bash
opencode
```

Or test with curl:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'
```

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLM_PROVIDER_URL` | Yes | - | Together AI endpoint (`https://api.together.xyz`) |
| `LLM_PROVIDER_KEY` | Yes | - | Together AI API key |
| `RELAY_PORT` | No | `8080` | Server port |

### Context Management Limits

All limits are **configurable** in `internal/proxy/proxy.go`. Default values work for 192k context models:

| Limit | Default | Behavior | Customize For... |
|-------|---------|----------|------------------|
| **Total context** | 180k tokens | Truncate to system + last 7 | 100k/128k context models |
| **Per-message** | 20k tokens | Truncate with notice | Specific use cases |
| **Messages kept** | 8 total | System + last 7 | More/less history |

**Adjust limits in code** (`internal/proxy/proxy.go`):
```go
if totalTokens > 180000 {        // Change threshold
    newMessages = messages[len(messages)-7:]  // Change 7 to keep more/less
}
```

## Features

### Middleware & Security
- **✓ Authentication proxy** - Secure API key handling (client dummy key → real provider key)
- **✓ Structured logging** - Request/response logging with token counts and truncation events
- **✓ Rate limiting ready** - Extensible middleware for adding custom rate limits
- **✓ Health checks** - HTTP `/health` endpoint for load balancers and monitoring
- **✓ Non-root container** - Runs as unprivileged user for production security

### Context Management
- **✓ Smart truncation** - Keeps system prompt + last 7 messages when approaching 180k limit
- **✓ Message size protection** - Caps individual messages at 20k tokens
- **✓ Real-time token estimation** - Monitors context size on every request
- **✓ Model mapping** - Any OpenAI model name maps to your configured backend

### API Compatibility
- **✓ OpenAI-compatible** - Drop-in replacement for OpenAI SDK
- **✓ Streaming support** - Full Server-Sent Events (SSE) for real-time responses
- **✓ Error handling** - Proper HTTP status codes and error messages
- **✓ Cost optimized** - Leverages Together AI's CPD for 80% savings on cached input

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completions (OpenAI-compatible) |
| `/health` | GET | Health check (returns `ok`) |

## Using Different Models

This relay is **model-agnostic**. To use a different model, edit `internal/proxy/proxy.go`:

```go
// Change this line to any model supported by your provider
var defaultModel = "meta-llama/Llama-3.3-70B-Instruct-Turbo"  // Example: Llama
// var defaultModel = "Qwen/Qwen2.5-72B-Instruct-Turbo"        // Example: Qwen
// var defaultModel = "gpt-4o"                                  // Example: OpenAI (via compatible endpoint)
```

Then rebuild and push:
```bash
docker build -t your-image:latest .
docker push your-image:latest
```

### Default: MiniMax M2.5

The relay ships with **MiniMax M2.5** as the default because:

- **192k context** - Handles large codebases
- **456B MoE architecture** - Efficient inference  
- **Together AI CPD** - 80% cost savings on cached context ($0.06/M vs $0.30/M)
- **No cold starts** - Unlike 600B+ parameter models

**MiniMax M2.5 Specs:**
- **Parameters:** 456B MoE
- **Context:** 192,000 tokens
- **Output:** Up to 8,192 tokens  
- **Pricing:** $0.30/M input (new), $0.06/M input (cached), $1.20/M output
- **Best for:** Coding, reasoning, long-context tasks

## Troubleshooting

### "Context too long" errors
The relay should prevent this. Check logs:
```bash
docker logs go_relay | grep TRUNCATE
```

### Model returns garbled tool calls
Some models (like MiniMax M2.5) have limited tool support. For file operations, ask the model to output content directly:
> "Output the markdown directly without using tools. I'll copy it myself."

### Check relay health
```bash
curl http://localhost:8080/health
```

## Development

```bash
# Clone
git clone https://github.com/Layyyth/GORelayServe.git
cd GORelayServe

# Build
go build -o relay ./cmd/relay/main.go

# Run
./relay
```

## License

MIT License - See [LICENSE](LICENSE) file.

## Author

Created by **Laith AbuJaafar**

---

*Compatible with any OpenAI-compatible LLM provider. Ships with Together AI + MiniMax M2.5 as default configuration.*
