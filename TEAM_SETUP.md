# Team Setup (No Clone Required)

Just download these 2 files and run:

## 1. Download

```bash
# Download the compose file
curl -O https://raw.githubusercontent.com/Layyyth/GORelayServe/master/docker-compose.yml

# Download the env template  
curl -O https://raw.githubusercontent.com/Layyyth/GORelayServe/master/.env.example

# Rename to .env and edit
mv .env.example .env
```

## 2. Configure

Edit `.env`:
```bash
LLM_PROVIDER_URL=https://api.together.xyz
LLM_PROVIDER_KEY=your_together_api_key_here
REDIS_ADDR=localhost:6379
RELAY_PORT=8080
```

## 3. Run

```bash
docker compose up -d

# Check it's working
curl http://localhost:8080/health
# → status: active
```

## 4. Configure Claude Code

Add to `~/.zshrc` or `~/.bashrc`:
```bash
export ANTHROPIC_BASE_URL="http://localhost:8080"
export ANTHROPIC_MODEL="claude-sonnet-4-20250514"
export ANTHROPIC_API_KEY="dummy-key"
```

Then: `source ~/.zshrc` and run `claude`

---

**That's it!** No git clone needed.
