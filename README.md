# GoRelayServe

A lightweight LLM relay server that translates Anthropic API format to OpenAI format. Enables Claude Code CLI to use Together AI models.

## Quick Start

### 1. Configure Environment

```bash
cp .env.example .env
# Edit .env and add your Together AI API key
```

### 2. Run with Docker

**Option A: Relay only** (if you already have Redis running locally)
```bash
# For Linux - allows Docker to access host Redis
docker compose up -d
```

**Option B: Full stack** (Relay + Redis)
```bash
docker compose -f docker-compose.full.yml up -d
```

### 3. Configure Claude Code

```bash
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_MODEL="claude-sonnet-4-20250514"
export ANTHROPIC_API_KEY="dummy-key"
claude
```

## Networking Explained

### The Problem
Docker containers run in an isolated network. They can't access `localhost:6379` on your host machine directly.

### Solutions by OS

| OS | Solution | Config |
|----|----------|--------|
| **Linux** | `network_mode: host` | Container shares host network, can access `localhost:6379` |
| **Mac/Windows** | `host.docker.internal` | Special DNS that resolves to host machine |

### Why the difference?
- **Linux**: `network_mode: host` is native and works perfectly
- **Mac/Windows**: Docker Desktop runs in a VM, so `host.docker.internal` bridges to the host

### For Teams (Pre-built Image)

1. Build and push the image:
```bash
docker build -t your-registry/gorelay:latest .
docker push your-registry/gorelay:latest
```

2. Distribute to team:
   - `docker-compose.example.yml` 
   - `.env.example`

3. Team members configure `.env` with their Redis address and run:
```bash
docker compose -f docker-compose.example.yml up -d
```

## How It Works

```
Claude Code CLI
      │ POST /v1/messages (Anthropic format)
      ▼
  Go Relay (localhost:8080)
      │ 1. Map model names (claude-sonnet → Qwen/...)
      │ 2. Convert to OpenAI format
      │ 3. Forward to Together AI
      ▼
   Together AI
      │
      │ OpenAI format response
      ▼
  Go Relay
      │ Convert to Anthropic format
      ▼
Claude Code CLI (thinks it's Anthropic)
```

## Endpoints

- `GET /v1/models` - Model list for Claude Code validation
- `POST /v1/messages` - Chat completions (Anthropic format)
- `POST /v1/chat/completions` - Standard OpenAI endpoint

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LLM_PROVIDER_URL` | Together AI endpoint | `https://api.together.xyz` |
| `LLM_PROVIDER_KEY` | Together AI API key | Required |
| `REDIS_ADDR` | Redis connection | `localhost:6379` |
| `RELAY_PORT` | Server port | `8080` |
