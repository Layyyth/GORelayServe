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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[%s] %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
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
		log.Fatal("Missing LLM_PROVIDER_URL or LLM_PROVIDER_KEY")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := cache.NewCache(redisAddr)

	relayProxy, err := proxy.NewRelayProxy(llmProvider, rdb)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", proxy.HandlerWrapper(relayProxy, rdb))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	port := os.Getenv("RELAY_PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	fmt.Println("==================================================")
	fmt.Println("MiniMax M2.5 Relay Server by Laith AbuJaafar")
	fmt.Printf("Listening on :%s\n", port)
	fmt.Println("Endpoint: POST /v1/chat/completions")
	fmt.Println("Model: MiniMax M2.5 FP4 (196.6K context)")
	fmt.Println("==================================================")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %s", err)
	}
}
