# MiniMax M2.5 Relay Server

An OpenAI-compatible API relay server for [MiniMaxAI/MiniMax-M2.5](https://www.together.ai/models/MiniMaxAI-MiniMax-M2.5) via [Together AI](https://www.together.ai/). Provides intelligent context management for long coding sessions while leveraging Together AI's efficient inference infrastructure.

## Why This Relay?

### The Problem

Modern AI coding assistants (like [OpenCode](https://opencode.ai/)) maintain long conversation histories. When these exceed model context limits (~192k tokens for MiniMax M2.5), one of three things happen:

1. **Hard failure** - Request rejected with "context too long" error
2. **Silent truncation** - Provider drops old messages without warning
3. **Expensive context loss** - Every request reprocesses full history from scratch

### The Solution

This relay acts as a **middleware layer** between your AI client and Together AI, providing:

**Request Management:**
- **Authentication handling** - Securely manages API keys, client sends "dummy", relay adds real key
- **Rate limiting foundation** - Extensible middleware structure for adding rate limits
- **Structured logging** - All requests logged with timestamps, token counts, and truncation events
- **Model mapping** - Any OpenAI model name maps to MiniMax M2.5 (drop-in replacement)

**Context Intelligence:**
- **Real-time context monitoring** - Estimates token count on every request
- **Smart truncation** - Keeps system prompt + last 7 messages when >180k tokens
- **Message size protection** - Caps individual messages at 20k tokens
- **Streaming support** - Full SSE streaming with proper error handling

**Result:** Long coding sessions continue uninterrupted, with Together AI's [Cache-Aware Disaggregation](https://www.together.ai/blog/cache-aware-disaggregated-inference) providing 80% cost savings on repeated context via their KV-cache reuse.

**Result:** Long coding sessions continue uninterrupted, with Together AI's [Cache-Aware Disaggregation](https://www.together.ai/blog/cache-aware-disaggregated-inference) providing 80% cost savings on repeated context via their KV-cache reuse.

## Architecture

```
┌─────────────┐      ┌─────────────────┐      ┌─────────────────┐      ┌─────────────┐
│   OpenCode  │ ───► │   Go Relay      │ ───► │   Together AI   │ ───► │  MiniMax    │
│   (Client)  │      │   (This Server) │      │   (Inference)   │      │  M2.5       │
└─────────────┘      └─────────────────┘      └─────────────────┘      └─────────────┘
                           │
                           ├─ 1. Receive OpenAI-format request
                           ├─ 2. Authentication & validation
                           ├─ 3. Estimate token count
                           ├─ 4. Truncate if >180k tokens
                           ├─ 5. Cap individual messages >20k
                           ├─ 6. Structured logging
                           ├─ 7. Map model name → MiniMax M2.5
                           └─ 8. Forward to Together AI
```

### Middleware Layer Capabilities

The relay acts as a **secure middleware layer** providing:

| Feature | Description | Benefit |
|---------|-------------|---------|
| **Authentication** | Client uses dummy key, relay injects real Together AI key | Secure credential management |
| **Request Logging** | Structured logs: method, path, tokens, truncations | Observability & debugging |
| **Rate Limiting** | Extensible middleware (add your own limits) | Abuse prevention |
| **Context Guardrails** | Enforces 180k total, 20k per-message limits | Prevents context overflow errors |
| **Model Normalization** | Maps any model name to MiniMax M2.5 | OpenAI-compatible drop-in |
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

| Limit | Value | Behavior |
|-------|-------|----------|
| **Total context** | 180k tokens | Truncate to system + last 7 messages |
| **Per-message** | 20k tokens | Truncate with `[...content truncated]` notice |
| **Model max** | 192k tokens | Hard limit from MiniMax M2.5 |

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
- **✓ Model mapping** - Any OpenAI model name maps to MiniMax M2.5

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

## Model Details

**MiniMax M2.5** (via Together AI):
- **Parameters:** 456B MoE (Mixture of Experts)
- **Context:** 192,000 tokens (~150k words)
- **Output:** Up to 8,192 tokens
- **Pricing:** $0.30/M input (new), $0.06/M input (cached), $1.20/M output
- **Strengths:** Coding, reasoning, long-context understanding

## Why MiniMax M2.5?

- **Massive context:** 192k tokens handles large codebases
- **MoE architecture:** Efficient inference (only activates relevant parameters)
- **Cost effective:** 80% cheaper with Together AI's context caching
- **No cold starts:** Unlike huge models that need minutes to warm up

## Troubleshooting

### "Context too long" errors
The relay should prevent this. Check logs:
```bash
docker logs go_relay | grep TRUNCATE
```

### Model returns garbled tool calls
MiniMax M2.5 has limited tool support. For file operations, ask the model to output content directly:
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

*Leveraging Together AI's Cache-Aware Disaggregation for efficient long-context inference.*
