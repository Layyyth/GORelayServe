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

var defaultModel = "Qwen/Qwen3-Coder-Next-FP8"
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
		req.URL.Path = ""
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Encoding", "identity")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode != http.StatusOK {
			return nil
		}

		if _, isStreaming := resp.Request.Context().Value("streaming").(bool); isStreaming {
			return nil
		}

		cacheKey, ok := resp.Request.Context().Value("cacheKey").(string)
		if !ok {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}

		var apiResp map[string]interface{}
		if err := json.Unmarshal(body, &apiResp); err == nil {
			if usage, ok := apiResp["usage"].(map[string]interface{}); ok {
				log.Printf("[TOKENS] prompt=%v completion=%v total=%v",
					usage["prompt_tokens"],
					usage["completion_tokens"],
					usage["total_tokens"])
			}
		}

		resp.Body = io.NopCloser(bytes.NewBuffer(body))

		go func(key string, data string) {
			rdb.Set(context.Background(), key, data, 7*24*time.Hour)
		}(cacheKey, string(body))

		return nil
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
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			proxy.ServeHTTP(w, r)
			return
		}

		injectRules(reqData)

		originalModel, _ := reqData["model"].(string)
		reqData["model"] = defaultModel

		isStream := false
		if stream, ok := reqData["stream"].(bool); ok && stream {
			isStream = true
			reqData["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
		}

		modalPayload := map[string]interface{}{"item": reqData}
		modifiedBody, _ := json.Marshal(modalPayload)
		cacheKey := rdb.GenerateKey(modifiedBody)

		if cachedVal, err := rdb.Get(r.Context(), cacheKey); err == nil && cachedVal != "" {
			if isStream {
				writeStreamFromCache(w, cachedVal)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.Write([]byte(cachedVal))
			}
			log.Printf("[CACHE HIT]")
			return
		}

		if isStream {
			handleStreaming(w, r, modifiedBody, cacheKey, proxy, rdb, originalModel)
			return
		}

		log.Printf("[REQUEST] %s -> %s", originalModel, defaultModel)
		w.Header().Set("X-Cache", "MISS")

		ctx := context.WithValue(r.Context(), "cacheKey", cacheKey)
		r = r.WithContext(ctx)
		r.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
		r.ContentLength = int64(len(modifiedBody))
		proxy.ServeHTTP(w, r)
	}
}

func handleStreaming(w http.ResponseWriter, r *http.Request, body []byte, cacheKey string, proxy *httputil.ReverseProxy, rdb *cache.Cache, originalModel string) {
	log.Printf("[STREAM] %s -> %s", originalModel, defaultModel)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	req, _ := http.NewRequest("POST", "http://placeholder/v1/chat/completions", bytes.NewReader(body))
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")

	if director := proxy.Director; director != nil {
		director(req)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var fullContent strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			w.Write([]byte("data: [DONE]\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			break
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok {
						fullContent.WriteString(content)
					}
				}
			}
		}

		w.Write([]byte(line + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	go rdb.Set(context.Background(), cacheKey, buildCachedResponse(fullContent.String()), 7*24*time.Hour)
}

func buildCachedResponse(content string) string {
	resp := map[string]interface{}{
		"id":      "cached_" + time.Now().Format("20060102150405"),
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

func writeStreamFromCache(w http.ResponseWriter, cachedVal string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(cachedVal), &resp); err != nil {
		return
	}

	choices, _ := resp["choices"].([]interface{})
	if len(choices) == 0 {
		return
	}

	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})
	content, _ := message["content"].(string)

	words := strings.Fields(content)
	for _, word := range words {
		chunk := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"delta": map[string]interface{}{
						"content": word + " ",
					},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(10 * time.Millisecond)
	}

	w.Write([]byte("data: [DONE]\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
