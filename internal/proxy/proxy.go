package proxy

import (
	"GoRelayServe/internal/cache"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type Provider struct {
	BaseURL string
	APIKey  string
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

func HandlerWrapper(proxy *httputil.ReverseProxy, rdb *cache.Cache) http.HandlerFunc {
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

		if stream, ok := reqData["stream"].(bool); ok && stream {
			reqData["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
			log.Printf("[STREAM] %s %s", r.Method, r.URL.Path)
		}

		modifiedBody, _ := json.Marshal(reqData)

		if stream, ok := reqData["stream"].(bool); ok && stream {
			ctx := context.WithValue(r.Context(), "streaming", true)
			r = r.WithContext(ctx)
			r.Body = io.NopCloser(bytes.NewBuffer(modifiedBody))
			r.ContentLength = int64(len(modifiedBody))
			proxy.ServeHTTP(w, r)
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
