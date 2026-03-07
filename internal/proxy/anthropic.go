package proxy

import (
	"GoRelayServe/internal/cache"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
)

// Anthropic types
type AnthropicMessage struct {
	Model     string              `json:"model"`
	Messages  []AnthropicContent  `json:"messages"`
	System    interface{}         `json:"system,omitempty"`  // Can be string or array of content blocks
	MaxTokens int                 `json:"max_tokens"`
	Stream    bool                `json:"stream,omitempty"`
	Tools     []AnthropicTool     `json:"tools,omitempty"`
}

type AnthropicContent struct {
	Role    string `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Model mapping: Anthropic model names -> Together AI model names
var modelMapping = map[string]string{
	"claude-3-5-sonnet-20241022":     "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"claude-3-5-sonnet-latest":       "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"claude-3-opus-20240229":         "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"claude-3-haiku-20240307":        "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"claude-3-7-sonnet-20250219":     "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"claude-sonnet-4-20250514":       "Qwen/Qwen3-Next-80B-A3B-Instruct",
	"Sonnet 4":                       "Qwen/Qwen3-Next-80B-A3B-Instruct",
}

// mapModel converts Anthropic model names to Together AI model names
func mapModel(anthropicModel string) string {
	if togetherModel, ok := modelMapping[anthropicModel]; ok {
		return togetherModel
	}
	// If no mapping found, return as-is (might be already a Together AI model)
	return anthropicModel
}

// isClaudeCodeRequest detects if request is from Claude Code CLI
func isClaudeCodeRequest(r *http.Request) bool {
	userAgent := r.Header.Get("User-Agent")
	anthropicVersion := r.Header.Get("anthropic-version")
	
	return strings.Contains(userAgent, "ClaudeCode") || 
		   strings.Contains(userAgent, "claude") ||
		   anthropicVersion != ""
}

// anthropicToOpenAI converts Anthropic Messages API format to OpenAI
func anthropicToOpenAI(anthropicReq *AnthropicMessage) map[string]interface{} {
	// Map Anthropic model name to Together AI model name
	togetherModel := mapModel(anthropicReq.Model)
	
	openAIReq := map[string]interface{}{
		"model":    togetherModel,
		"messages": []map[string]interface{}{},
	}

	if anthropicReq.MaxTokens > 0 {
		openAIReq["max_tokens"] = anthropicReq.MaxTokens
	}

	if anthropicReq.Stream {
		openAIReq["stream"] = true
		openAIReq["stream_options"] = map[string]interface{}{
			"include_usage": true,
		}
	}

	// Handle system message (can be string or array of content blocks)
	if anthropicReq.System != nil {
		systemContent := ""
		switch s := anthropicReq.System.(type) {
		case string:
			systemContent = s
		case []interface{}:
			// Array of content blocks - concatenate text content
			var parts []string
			for _, block := range s {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
			systemContent = strings.Join(parts, "\n")
		}
		if systemContent != "" {
			openAIReq["messages"] = append(openAIReq["messages"].([]map[string]interface{}), map[string]interface{}{
				"role":    "system",
				"content": systemContent,
			})
		}
	}

	// Convert messages
	for _, msg := range anthropicReq.Messages {
		openAIMsg := map[string]interface{}{
			"role": msg.Role,
		}

		switch content := msg.Content.(type) {
		case string:
			openAIMsg["content"] = content
		case []interface{}:
			// Handle tool_use and tool_result blocks
			openAIContent := []map[string]interface{}{}
			for _, block := range content {
				if blockMap, ok := block.(map[string]interface{}); ok {
					blockType, _ := blockMap["type"].(string)
					
					switch blockType {
					case "text":
						openAIContent = append(openAIContent, map[string]interface{}{
							"type": "text",
							"text": blockMap["text"],
						})
					case "tool_use":
						// Convert to OpenAI tool_calls format
						openAIMsg["tool_calls"] = []map[string]interface{}{
							{
								"id":   blockMap["id"],
								"type": "function",
								"function": map[string]interface{}{
									"name":      blockMap["name"],
									"arguments": blockMap["input"],
								},
							},
						}
					case "tool_result":
						// Tool results are sent as user messages with content
						openAIMsg["content"] = blockMap["content"]
					}
				}
			}
			if len(openAIContent) > 0 {
				openAIMsg["content"] = openAIContent
			}
		default:
			openAIMsg["content"] = ""
		}

		openAIReq["messages"] = append(openAIReq["messages"].([]map[string]interface{}), openAIMsg)
	}

	// Convert tools
	if len(anthropicReq.Tools) > 0 {
		openAITools := []map[string]interface{}{}
		for _, tool := range anthropicReq.Tools {
			openAITools = append(openAITools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  tool.InputSchema,
				},
			})
		}
		openAIReq["tools"] = openAITools
	}

	return openAIReq
}

// openAIToAnthropicStream converts OpenAI streaming response to Anthropic SSE format
func openAIToAnthropicStream(openAIResp []byte, anthropicModel string) ([]byte, error) {
	var openAIMap map[string]interface{}
	if err := json.Unmarshal(openAIResp, &openAIMap); err != nil {
		return nil, err
	}

	// Create Anthropic message_start event
	anthropicEvent := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":      openAIMap["id"],
			"type":    "message",
			"role":    "assistant",
			"content": []interface{}{},
			"model":   anthropicModel,
		},
	}

	// Add usage if present
	if usage, ok := openAIMap["usage"].(map[string]interface{}); ok {
		anthropicEvent["message"].(map[string]interface{})["usage"] = map[string]interface{}{
			"input_tokens":  usage["prompt_tokens"],
			"output_tokens": usage["completion_tokens"],
		}
	}

	return json.Marshal(anthropicEvent)
}

// AnthropicHandler handles /v1/messages endpoint for Claude Code
func AnthropicHandler(proxy *httputil.ReverseProxy, rdb *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read and parse Anthropic request
		body, _ := io.ReadAll(r.Body)
		
		var anthropicReq AnthropicMessage
		if err := json.Unmarshal(body, &anthropicReq); err != nil {
			log.Printf("[ANTHROPIC] JSON parse error: %v", err)
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		// Convert to OpenAI format
		openAIReq := anthropicToOpenAI(&anthropicReq)

		modifiedBody, _ := json.Marshal(openAIReq)
		
		estimatedTokens := len(modifiedBody) / 4
		log.Printf("[REQUEST] model=%s stream=%v estimated_input_tokens=%d", anthropicReq.Model, anthropicReq.Stream, estimatedTokens)

		if !anthropicReq.Stream {
			cacheKey := rdb.GenerateKey(modifiedBody)
			
			if cachedVal, err := rdb.Get(r.Context(), cacheKey); err == nil && cachedVal != "" {
				anthropicResp := openAIToAnthropic([]byte(cachedVal), anthropicReq.Model)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				log.Printf("[CACHE HIT] model=%s", anthropicReq.Model)
				w.Write(anthropicResp)
				return
			}

			log.Printf("[CACHE MISS] model=%s", anthropicReq.Model)
			
			resp, err := makeOpenAIRequest(r, modifiedBody, proxy)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			go rdb.Set(r.Context(), cacheKey, string(resp), 7*24*60*60)

			anthropicResp := openAIToAnthropic(resp, anthropicReq.Model)
			w.Header().Set("Content-Type", "application/json")
			w.Write(anthropicResp)
			return
		}

		// Handle streaming
		handleAnthropicStreaming(w, r, modifiedBody, anthropicReq.Model, proxy)
	}
}

// openAIToAnthropic converts OpenAI response to Anthropic format
func openAIToAnthropic(openAIResp []byte, model string) []byte {
	log.Printf("[DEBUG] openAIToAnthropic input: %s", string(openAIResp))
	
	var openAI map[string]interface{}
	json.Unmarshal(openAIResp, &openAI)

	choices, _ := openAI["choices"].([]interface{})
	content := []map[string]interface{}{}
	
	for _, choice := range choices {
		if choiceMap, ok := choice.(map[string]interface{}); ok {
			if message, ok := choiceMap["message"].(map[string]interface{}); ok {
				// Handle tool_calls
				if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						if tcMap, ok := tc.(map[string]interface{}); ok {
							function, _ := tcMap["function"].(map[string]interface{})
							content = append(content, map[string]interface{}{
								"type":  "tool_use",
								"id":    tcMap["id"],
								"name":  function["name"],
								"input": function["arguments"],
							})
						}
					}
				}
				
				// Handle text content
				if text, ok := message["content"].(string); ok && text != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				}
			}
		}
	}

	anthropicResp := map[string]interface{}{
		"id":      openAI["id"],
		"type":    "message",
		"role":    "assistant",
		"content": content,
		"model":   model,
	}

	// Convert usage and log token counts
	if usage, ok := openAI["usage"].(map[string]interface{}); ok {
		promptTokens, _ := usage["prompt_tokens"].(float64)
		completionTokens, _ := usage["completion_tokens"].(float64)
		totalTokens, _ := usage["total_tokens"].(float64)
		log.Printf("[TOKENS] prompt=%.0f completion=%.0f total=%.0f", promptTokens, completionTokens, totalTokens)
		anthropicResp["usage"] = map[string]interface{}{
			"input_tokens":  usage["prompt_tokens"],
			"output_tokens": usage["completion_tokens"],
		}
	}

	result, _ := json.Marshal(anthropicResp)
	return result
}

// makeOpenAIRequest forwards request to OpenAI-compatible backend
func makeOpenAIRequest(r *http.Request, body []byte, proxy *httputil.ReverseProxy) ([]byte, error) {
	// Create a new request to forward - the Director will set the URL
	req, _ := http.NewRequest("POST", "http://placeholder/v1/chat/completions", bytes.NewReader(body))
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity") // Disable compression
	
	// Use the proxy's director to set up the request (sets Host, Scheme, etc.)
	if proxy.Director != nil {
		proxy.Director(req)
	}

	log.Printf("[DEBUG] Forwarding to: %s", req.URL.String())

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[DEBUG] Request error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("[DEBUG] Response status: %d, body length: %d", resp.StatusCode, len(bodyBytes))
	return bodyBytes, nil
}

// handleAnthropicStreaming handles SSE streaming for Anthropic format
func handleAnthropicStreaming(w http.ResponseWriter, r *http.Request, body []byte, model string, proxy *httputil.ReverseProxy) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send message_start
	messageStart := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":      "msg_" + generateID(),
			"type":    "message",
			"role":    "assistant",
			"content": []interface{}{},
			"model":   model,
			"usage":   map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
		},
	}
	writeSSE(w, messageStart)

	// Send content_block_start
	contentStart := map[string]interface{}{
		"type":         "content_block_start",
		"index":        0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	writeSSE(w, contentStart)

	// Forward to OpenAI streaming and translate
	req, _ := http.NewRequest("POST", "http://localhost:8080/v1/chat/completions", bytes.NewReader(body))
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Type", "application/json")

	// Make request through proxy
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// Read and translate SSE events
	scanner := bufio.NewScanner(resp.Body)
	var fullText strings.Builder
	
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var openAIChunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &openAIChunk); err != nil {
			continue
		}

		// Extract text from OpenAI delta
		if choices, ok := openAIChunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok {
						fullText.WriteString(content)
						
						// Send content_block_delta
						deltaEvent := map[string]interface{}{
							"type":  "content_block_delta",
							"index": 0,
							"delta": map[string]interface{}{
								"type": "text_delta",
								"text": content,
							},
						}
						writeSSE(w, deltaEvent)
					}
				}
			}
		}
	}

	// Send content_block_stop
	writeSSE(w, map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	})

	// Send message_delta with usage
	writeSSE(w, map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason": "end_turn",
		},
		"usage": map[string]interface{}{
			"output_tokens": len(fullText.String()) / 4, // Rough estimate
		},
	})

	// Send message_stop
	writeSSE(w, map[string]interface{}{
		"type": "message_stop",
	})
}

// writeSSE writes a Server-Sent Event
func writeSSE(w http.ResponseWriter, data interface{}) {
	jsonData, _ := json.Marshal(data)
	w.Write([]byte("data: "))
	w.Write(jsonData)
	w.Write([]byte("\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// generateID creates a simple unique ID
func generateID() string {
	// Simple implementation - in production use proper UUID
	return "random"
}

// AnthropicModel represents a model in the Anthropic models list
type AnthropicModel struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	CreatedAt     string `json:"created_at"`
}

// AnthropicModelsResponse represents the /v1/models response
type AnthropicModelsResponse struct {
	Data    []AnthropicModel `json:"data"`
	HasMore bool             `json:"has_more"`
	FirstID string           `json:"first_id"`
	LastID  string           `json:"last_id"`
}

// AnthropicModelsHandler handles /v1/models endpoint
func AnthropicModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[ANTHROPIC] /v1/models request from %s", r.UserAgent())
		
		models := []AnthropicModel{
			{
				Type:        "model",
				ID:          "claude-3-5-sonnet-20241022",
				DisplayName: "Claude 3.5 Sonnet (2024-10-22)",
				CreatedAt:   "2024-10-22T00:00:00Z",
			},
			{
				Type:        "model",
				ID:          "claude-3-5-sonnet-latest",
				DisplayName: "Claude 3.5 Sonnet (Latest)",
				CreatedAt:   "2024-10-22T00:00:00Z",
			},
			{
				Type:        "model",
				ID:          "claude-3-opus-20240229",
				DisplayName: "Claude 3 Opus",
				CreatedAt:   "2024-02-29T00:00:00Z",
			},
			{
				Type:        "model",
				ID:          "claude-3-haiku-20240307",
				DisplayName: "Claude 3 Haiku",
				CreatedAt:   "2024-03-07T00:00:00Z",
			},
			{
				Type:        "model",
				ID:          "claude-3-7-sonnet-20250219",
				DisplayName: "Claude 3.7 Sonnet",
				CreatedAt:   "2025-02-19T00:00:00Z",
			},
			{
				Type:        "model",
				ID:          "claude-sonnet-4-20250514",
				DisplayName: "Claude Sonnet 4",
				CreatedAt:   "2025-05-14T00:00:00Z",
			},
		}
		
		response := AnthropicModelsResponse{
			Data:    models,
			HasMore: false,
			FirstID: models[0].ID,
			LastID:  models[len(models)-1].ID,
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}
