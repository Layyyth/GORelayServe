package proxy

import (
	"GoRelayServe/internal/cache"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

type Provider struct {
	BaseURL string
	APIKey  string
}

var defaultModel = "MiniMaxAI/MiniMax-M2.5"

func NewRelayProxy(p Provider, rdb *cache.Cache) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	proxy.Director = func(req *http.Request) {
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/v1/chat/completions"
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Encoding", "identity")
	}

	return proxy, nil
}

func truncateMessages(reqData map[string]interface{}) {
	messages, ok := reqData["messages"].([]interface{})
	if !ok || len(messages) <= 10 {
		return
	}

	// Estimate tokens (4 chars ≈ 1 token)
	totalChars := 0
	for _, msg := range messages {
		if m, ok := msg.(map[string]interface{}); ok {
			if content, ok := m["content"].(string); ok {
				totalChars += len(content)
			}
		}
	}

	estimatedTokens := totalChars / 4

	// If >150k estimated, keep system + last 10 messages
	if estimatedTokens > 150000 {
		log.Printf("[TRUNCATE] Context %d tokens -> trimming to last 10 messages", estimatedTokens)

		newMessages := []interface{}{messages[0]} // Keep system message
		// Keep last 9 user/assistant messages
		start := len(messages) - 9
		if start < 1 {
			start = 1
		}
		newMessages = append(newMessages, messages[start:]...)
		reqData["messages"] = newMessages
	}
}

func adjustMaxTokens(reqData map[string]interface{}) {
	messages, _ := reqData["messages"].([]interface{})
	totalChars := 0
	for _, msg := range messages {
		if m, ok := msg.(map[string]interface{}); ok {
			if content, ok := m["content"].(string); ok {
				totalChars += len(content)
			}
		}
	}

	estimatedInput := totalChars / 4
	requestedMax, _ := reqData["max_tokens"].(float64)

	// If input > 180k, force max_tokens to 4k to fit in 196k limit
	if estimatedInput > 180000 && requestedMax > 4000 {
		reqData["max_tokens"] = 4000
		log.Printf("[ADJUST] Input %d tokens, reduced max_tokens to 4000", estimatedInput)
	}
}

func HandlerWrapper(proxy *httputil.ReverseProxy, rdb *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var reqData map[string]interface{}
		if err := json.Unmarshal(body, &reqData); err != nil {
			http.Error(w, `{"error": "invalid json"}`, http.StatusBadRequest)
			return
		}

		// Context management
		truncateMessages(reqData)
		adjustMaxTokens(reqData)

		// Map model name
		originalModel, _ := reqData["model"].(string)
		reqData["model"] = defaultModel

		// Check if streaming
		isStream := false
		if stream, ok := reqData["stream"].(bool); ok && stream {
			isStream = true
		}

		modifiedBody, _ := json.Marshal(reqData)

		// Check cache for all requests
		cacheKey := rdb.GenerateKey(modifiedBody)
		if cached, err := rdb.Get(r.Context(), cacheKey); err == nil && cached != "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(cached))
			log.Printf("[CACHE HIT]")
			return
		}

		if isStream {
			log.Printf("[STREAM] %s -> %s", originalModel, defaultModel)
			handleStreaming(w, r, modifiedBody, proxy, rdb, cacheKey)
			return
		}

		log.Printf("[REQUEST] %s -> %s", originalModel, defaultModel)
		handleNonStreaming(w, r, modifiedBody, proxy, rdb, cacheKey)
	}
}

func handleStreaming(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy, rdb *cache.Cache, cacheKey string) {
	// Create request to backend
	targetURL := os.Getenv("LLM_PROVIDER_URL") + "/v1/chat/completions"

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error": "failed to create request"}`, http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("LLM_PROVIDER_KEY"))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")

	// Make request with timeout
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ERROR] Backend request failed: %v", err)
		http.Error(w, `{"error": "backend failed"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[ERROR] Backend returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, string(body), resp.StatusCode)
		return
	}

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Stream the response and collect for caching
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[ERROR] ResponseWriter doesn't support flushing")
		return
	}

	var fullContent strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			// Extract content from SSE for caching
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if data != "[DONE]" {
					var chunk map[string]interface{}
					if err := json.Unmarshal([]byte(data), &chunk); err == nil {
						if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
							if choice, ok := choices[0].(map[string]interface{}); ok {
								if delta, ok := choice["delta"].(map[string]interface{}); ok {
									if content, ok := delta["content"].(string); ok {
										fullContent.WriteString(content)
									}
								}
							}
						}
					}
				}
			}
			w.Write([]byte(line + "\n\n"))
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[ERROR] Scanner error: %v", err)
	}

	// Cache the aggregated response
	if fullContent.Len() > 0 {
		go rdb.Set(context.Background(), cacheKey, buildCachedResponse(fullContent.String()), 7*24*time.Hour)
	}
}

func handleNonStreaming(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy, rdb *cache.Cache, cacheKey string) {
	// Create request to backend
	targetURL := os.Getenv("LLM_PROVIDER_URL") + "/v1/chat/completions"

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error": "failed to create request"}`, http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("LLM_PROVIDER_KEY"))

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ERROR] Backend request failed: %v", err)
		http.Error(w, `{"error": "backend failed"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error": "failed to read response"}`, http.StatusInternalServerError)
		return
	}

	// Log token usage
	var apiResp map[string]interface{}
	if err := json.Unmarshal(respBody, &apiResp); err == nil {
		if usage, ok := apiResp["usage"].(map[string]interface{}); ok {
			log.Printf("[TOKENS] prompt=%v completion=%v total=%v",
				usage["prompt_tokens"],
				usage["completion_tokens"],
				usage["total_tokens"])
		}
	}

	// Cache response
	go rdb.Set(context.Background(), cacheKey, string(respBody), 7*24*time.Hour)

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func buildCachedResponse(content string) string {
	resp := map[string]interface{}{
		"id":      "chatcmpl-cached",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   defaultModel,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": len(content) / 4,
			"total_tokens":      len(content) / 4,
		},
	}
	data, _ := json.Marshal(resp)
	return string(data)
}
