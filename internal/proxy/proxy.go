package proxy

import (
	"GoRelayServe/internal/cache"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type Provider struct {
	BaseURL string
	APIKey  string
}

// StreamOptions enables usage reporting in streaming mode
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatCompletionRequest with stream options
type ChatCompletionRequest struct {
	Model         string           `json:"model"`
	Messages      []Message        `json:"messages"`
	Stream        bool             `json:"stream"`
	StreamOptions *StreamOptions   `json:"stream_options,omitempty"`
	MaxTokens     int              `json:"max_tokens,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage tracks token consumption
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

func NewRelayProxy(p Provider, rules string, rdb *cache.Cache) (*httputil.ReverseProxy, error) {
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

func HandlerWrapper(proxy *httputil.ReverseProxy, rdb *cache.Cache, rules string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			proxy.ServeHTTP(w, r)
			return
		}

		body, _ := io.ReadAll(r.Body)

		var reqData map[string]interface{}
		if err := json.Unmarshal(body, &reqData); err != nil {
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			proxy.ServeHTTP(w, r)
			return
		}

		injectRules(&reqData, rules)

		isStreaming := false
		if stream, ok := reqData["stream"].(bool); ok && stream {
			isStreaming = true
		}

		if isStreaming {
			reqData["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
		}

		modifiedBody, _ := json.Marshal(reqData)

		if isStreaming {
			log.Printf("[STREAM] %s %s", r.Method, r.URL.Path)
			handleStreamingRequest(w, r, modifiedBody, proxy, rules, rdb)
			return
		}

		cacheKey := rdb.GenerateKey(modifiedBody)

		if cachedVal, err := rdb.Get(r.Context(), cacheKey); err == nil && cachedVal != "" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			log.Printf("[HIT] %s %s", r.Method, r.URL.Path)

			var cachedResp map[string]interface{}
			if err := json.Unmarshal([]byte(cachedVal), &cachedResp); err == nil {
				if usage, ok := cachedResp["usage"].(map[string]interface{}); ok {
					log.Printf("[TOKENS-CACHED] prompt=%v completion=%v total=%v",
						usage["prompt_tokens"],
						usage["completion_tokens"],
						usage["total_tokens"])
				}
			}

			w.Write([]byte(cachedVal))
			return
		}

		w.Header().Set("X-Cache", "MISS")
		log.Printf("[MISS] %s %s", r.Method, r.URL.Path)

		ctx := context.WithValue(r.Context(), "cacheKey", cacheKey)
		r = r.WithContext(ctx)
		r.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
		r.ContentLength = int64(len(modifiedBody))
		proxy.ServeHTTP(w, r)
	}
}

func handleStreamingRequest(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy, rules string, rdb *cache.Cache) {
	ctx := context.WithValue(r.Context(), "streaming", true)
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewBuffer(body))
	r.ContentLength = int64(len(body))

	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var totalUsage *Usage
	var fullResponse strings.Builder

	scanner := bufio.NewScanner(recorder.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				if totalUsage != nil {
					log.Printf("[TOKENS-STREAM] prompt=%d completion=%d total=%d",
						totalUsage.PromptTokens,
						totalUsage.CompletionTokens,
						totalUsage.TotalTokens)
				}
				fmt.Fprintln(w, line)
				w.(http.Flusher).Flush()
				break
			}

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if chunk.Usage != nil {
					totalUsage = chunk.Usage
				}
				fullResponse.WriteString(data)
			}

			fmt.Fprintln(w, line)
			w.(http.Flusher).Flush()
		}
	}
}

func injectRules(data *map[string]interface{}, rules string) {
	messages, ok := (*data)["messages"].([]interface{})
	if !ok {
		return
	}
	newSystemMsg := map[string]interface{}{
		"role":    "system",
		"content": rules,
	}
	(*data)["messages"] = append([]interface{}{newSystemMsg}, messages...)
}
