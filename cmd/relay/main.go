package main

import (
	"GoRelayServe/internal/cache"
	"GoRelayServe/internal/proxy"
	"GoRelayServe/internal/rules"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	rulesContext, err := rules.LoadRules("./coding_rules")
	if err != nil {
		log.Printf("rules load failure: %v", err)
		rulesContext = ""
	}

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

	relayProxy, err := proxy.NewRelayProxy(llmProvider, rulesContext, rdb)
	if err != nil {
		log.Fatalf("proxy init failed: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", proxy.HandlerWrapper(relayProxy, rdb, rulesContext))

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
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	fmt.Printf("10x relay listening on :%s\n", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server failure: %s", err)
	}
}
