package ai

import (
	"context"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// ChatCompletionRequest is the request sent to the LLM API.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatCompletionResponse is the non-streaming response from the LLM API.
type ChatCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
	Usage   Usage          `json:"usage,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Index        int           `json:"index"`
	Message      ChatMessage   `json:"message"`
	FinishReason string        `json:"finish_reason"`
	Delta        *ChatMessage  `json:"delta,omitempty"` // for streaming
}

// Usage contains token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Stream is a streaming response from the LLM API.
type Stream interface {
	Recv() (*ChatCompletionResponse, error)
	Close() error
}

// LLMClient is the interface for LLM providers.
type LLMClient interface {
	// ChatComplete sends a non-streaming request.
	ChatComplete(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	// ChatStream sends a streaming request.
	ChatStream(ctx context.Context, req ChatCompletionRequest) (Stream, error)
	// Close closes any underlying connections.
	Close() error
}

// NewClient creates an LLM client for the given provider.
func NewClient(providerType, apiKey, baseURL, modelName, ollamaHost string) (LLMClient, error) {
	switch providerType {
	case "ollama":
		return NewOllamaClient(baseURL, modelName)
	default:
		return NewOpenAIClient(apiKey, baseURL, modelName)
	}
}
