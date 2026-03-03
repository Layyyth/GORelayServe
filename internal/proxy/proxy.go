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

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			proxy.ServeHTTP(w, r)
			return
		}

		injectRules(&data, rules)
		modifiedBody, _ := json.Marshal(data)

		if stream, ok := data["stream"].(bool); ok && stream {
			log.Printf("[STREAM] %s %s", r.Method, r.URL.Path)
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
