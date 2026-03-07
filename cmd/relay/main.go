package main

import (
	"GoRelayServe/internal/cache"
	"GoRelayServe/internal/proxy"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// loggingMiddleware logs all incoming requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[REQUEST] %s %s from %s (User-Agent: %s)", r.Method, r.URL.Path, r.RemoteAddr, r.UserAgent())
		next.ServeHTTP(w, r)
	})
}

func main() {
	_ = godotenv.Load()

	llmProvider := proxy.Provider{
		BaseURL: os.Getenv("LLM_PROVIDER_URL"),
		APIKey:  os.Getenv("LLM_PROVIDER_KEY"),
	}

	if llmProvider.BaseURL == "" || llmProvider.APIKey == "" {
		log.Fatal("provider config missing")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := cache.NewCache(redisAddr)

	relayProxy, err := proxy.NewRelayProxy(llmProvider, rdb)
	if err != nil {
		log.Fatalf("proxy init failed: %v", err)
	}

	mux := http.NewServeMux()

	// OpenAI-compatible endpoint
	mux.HandleFunc("/v1/chat/completions", proxy.HandlerWrapper(relayProxy, rdb))

	// Anthropic Messages API endpoint (for Claude Code)
	mux.HandleFunc("/v1/messages", proxy.AnthropicHandler(relayProxy, rdb))

	// Anthropic Models API endpoint (for Claude Code model validation)
	mux.HandleFunc("/v1/models", proxy.AnthropicModelsHandler())

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "status: active")
	})

	port := os.Getenv("RELAY_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	fmt.Printf("10x relay listening on :%s\n", port)
	fmt.Printf("OpenAI endpoint: /v1/chat/completions\n")
	fmt.Printf("Anthropic endpoint: /v1/messages (Claude Code compatible)\n")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server failure: %s", err)
	}
}
