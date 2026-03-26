# MiniMax M2.5 Relay Server

A lightweight OpenAI-compatible relay server for [MiniMaxAI/MiniMax-M2.5](https://www.together.ai/models/MiniMaxAI-MiniMax-M2.5) via Together AI. Features rules injection and model mapping for seamless integration with opencode and other OpenAI-compatible tools.

## Quick Start

### 1. Clone and Configure

```bash
git clone https://github.com/Layyith/GORelayServe.git
cd GORelayServe
cp .env.example .env
# Edit .env and add your Together AI API key
```

### 2. Run

```bash
docker compose up -d
```

### 3. Configure Opencode

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
          "name": "MiniMax M2.5 (Together AI)",
          "options": { "model": "gpt-4" },
          "limit": { "context": 196608, "output": 8192 }
        }
      }
    }
  },
  "model": "go-relay/gpt-4"
}
```

### 4. Use

```bash
opencode
```

Or test with curl:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}'
```

Any model name you request gets mapped to `MiniMaxAI/MiniMax-M2.5`.

## Features

- **OpenAI Compatible**: Drop-in replacement for OpenAI API
- **Model Mapping**: All requests go to MiniMax M2.5 (456B MoE, great for coding)
- **Rules Injection**: Automatically prepends rules.md to system messages
- **Streaming Support**: Full SSE streaming for real-time responses
- **Redis Caching**: Optional caching layer (currently disabled for streaming)

## Architecture

```
┌──────────┐      ┌─────────────┐      ┌─────────────────┐      ┌──────────┐
│ opencode │ ───► │ Go Relay    │ ───► │ Together AI     │ ───► │ MiniMax  │
│ (client) │      │ (localhost) │      │ (together.xyz)  │      │ M2.5     │
└──────────┘      └─────────────┘      └─────────────────┘      └──────────┘
```

1. **Opencode** sends OpenAI-format request to `localhost:8080`
2. **Go Relay** injects rules from `rules.md` and maps model name
3. **Together AI** routes to MiniMax M2.5 inference
4. **Streaming response** flows back through relay to client

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /v1/chat/completions` | Chat completions (OpenAI format) |
| `GET /health` | Health check |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LLM_PROVIDER_URL` | Together AI endpoint | `https://api.together.xyz` |
| `LLM_PROVIDER_KEY` | Together AI API key | Required |
| `REDIS_ADDR` | Redis connection | `redis:6379` |

## Docker Compose

```yaml
services:
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
  
  relay:
    image: laithaj/gorelayserve:latest
    ports: ["8080:8080"]
    environment:
      - LLM_PROVIDER_URL=https://api.together.xyz
      - LLM_PROVIDER_KEY=${TOGETHER_API_KEY}
      - REDIS_ADDR=redis:6379
    volumes:
      - ./rules.md:/app/rules.md:ro
```

## Why MiniMax M2.5?

- **456B parameters** - Massive MoE model for coding
- **196K context** - Long conversations without truncation
- **Fast inference** - Together AI optimized infrastructure
- **Reasoning + Response** - Shows thinking process before answering
- **Cheap** - ~$0.80/million input tokens

## Author

Created by **Laith AbuJaafar**
