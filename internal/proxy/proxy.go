package proxy

import (
	"GoRelayServe/internal/cache"
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

type Provider struct {
	BaseURL string
	APIKey  string
}

var defaultModel = "MiniMaxAI/MiniMax-M2.5"
var rulesContent string

func init() {
	data, _ := os.ReadFile("rules.md")
	rulesContent = string(data)
}

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

func injectRules(reqData map[string]interface{}) {
	if rulesContent == "" {
		return
	}

	messages, ok := reqData["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return
	}

	firstMsg, ok := messages[0].(map[string]interface{})
	if !ok {
		return
	}

	role, _ := firstMsg["role"].(string)
	if role != "system" {
		newSystem := map[string]interface{}{
			"role":    "system",
			"content": rulesContent,
		}
		reqData["messages"] = append([]interface{}{newSystem}, messages...)
		return
	}

	content, _ := firstMsg["content"].(string)
	firstMsg["content"] = rulesContent + "\n\n" + content
}

func HandlerWrapper(proxy *httputil.ReverseProxy, rdb *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var reqData map[string]interface{}
		if err := json.Unmarshal(body, &reqData); err != nil {
			http.Error(w, `{"error": "invalid json"}`, http.StatusBadRequest)
			return
		}

		// Inject rules
		injectRules(reqData)

		// Map model name
		originalModel, _ := reqData["model"].(string)
		reqData["model"] = defaultModel

		// Check if streaming
		isStream := false
		if stream, ok := reqData["stream"].(bool); ok && stream {
			isStream = true
		}

		modifiedBody, _ := json.Marshal(reqData)

		if isStream {
			log.Printf("[STREAM] %s -> %s", originalModel, defaultModel)
			handleStreaming(w, r, modifiedBody, proxy)
			return
		}

		log.Printf("[REQUEST] %s -> %s", originalModel, defaultModel)
		handleNonStreaming(w, r, modifiedBody, proxy, rdb)
	}
}

func handleStreaming(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy) {
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
	
	// Stream the response
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[ERROR] ResponseWriter doesn't support flushing")
		return
	}
	
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			w.Write([]byte(line + "\n\n"))
			flusher.Flush()
		}
	}
	
	if err := scanner.Err(); err != nil {
		log.Printf("[ERROR] Scanner error: %v", err)
	}
}

func handleNonStreaming(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy, rdb *cache.Cache) {
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
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}
