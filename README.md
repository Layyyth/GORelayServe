# Qwen Code Relay

A lightweight OpenAI-compatible relay server for [Qwen Code](https://qwen-code.com/) (Qwen3-235B). Caches responses with Redis for faster, cheaper development.

## Quick Start

### 1. Configure

```bash
cp .env.example .env
# Edit .env and add your Together AI API key
```

### 2. Run

```bash
docker compose up -d
```

### 3. Use

Point any OpenAI-compatible client to `http://localhost:8080/v1/chat/completions`

```bash
# Example with curl
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Any model name you request gets mapped to `Qwen/Qwen3-235B-A22B-Instruct`.

## Features

- **OpenAI Compatible**: Drop-in replacement for OpenAI API
- **Model Mapping**: All requests go to Qwen Code (235B params, great for coding)
- **Redis Caching**: 7-day cache for non-streaming requests
- **Streaming Support**: Full SSE streaming for real-time responses

## Architecture

```
Your App (OpenAI format)
      │ POST /v1/chat/completions
      ▼
  Qwen Relay (localhost:8080)
      │ 1. Map any model → Qwen Code
      │ 2. Check Redis cache
      │ 3. Forward to Together AI
      ▼
   Together AI (Qwen Code)
      │
      │ Response
      ▼
  Qwen Relay
      │ Cache if not streaming
      ▼
Your App (OpenAI format)
```

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
| `REDIS_ADDR` | Redis connection | `localhost:6379` |

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
```

## Why Qwen Code?

- **235B parameters** - Powerful for coding tasks
- **A22B activation** - Efficient inference (only 22B active at a time)
- **OpenAI compatible** - No translation layer needed
- **Faster than DeepSeek-V3.1** - No 685B parameter cold starts
- **Tool calling** - Native function calling support
