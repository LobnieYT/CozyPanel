package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaClient implements LLMClient for Ollama.
type OllamaClient struct {
	host     string
	model    string
	httpClient *http.Client
}

// NewOllamaClient creates a new Ollama client.
func NewOllamaClient(host, model string) (*OllamaClient, error) {
	if host == "" {
		host = "http://localhost:11434"
	}
	return &OllamaClient{
		host: host,
		model: model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Ollama can be slow on first load
		},
	}, nil
}

// ChatComplete sends a non-streaming chat completion request to Ollama.
func (c *OllamaClient) ChatComplete(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Model = c.model
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp struct {
		Message ChatMessage `json:"message"`
		Done    bool        `json:"done"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, err
	}

	return &ChatCompletionResponse{
		Model: c.model,
		Choices: []Choice{{
			Index:        0,
			Message:      ollamaResp.Message,
			FinishReason: "stop",
		}},
	}, nil
}

// ChatStream sends a streaming chat completion request to Ollama.
func (c *OllamaClient) ChatStream(ctx context.Context, req ChatCompletionRequest) (Stream, error) {
	req.Model = c.model
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Ollama API error %d: %s", resp.StatusCode, string(body))
	}

	return &ollamaStream{resp: resp}, nil
}

func (c *OllamaClient) Close() error {
	return nil
}

// ollamaStream implements Stream for Ollama streaming responses.
type ollamaStream struct {
	resp *http.Response
}

func (s *ollamaStream) Recv() (*ChatCompletionResponse, error) {
	reader := bufio.NewReader(s.resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var ollamaChunk struct {
			Message ChatMessage `json:"message"`
			Done    bool        `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &ollamaChunk); err != nil {
			continue // skip malformed lines
		}

		resp := &ChatCompletionResponse{
			Model: "ollama",
			Choices: []Choice{{
				Index:        0,
				Delta:        &ollamaChunk.Message,
				FinishReason: map[bool]string{true: "stop", false: ""}[ollamaChunk.Done],
			}},
		}
		return resp, nil
	}
}

func (s *ollamaStream) Close() error {
	return s.resp.Body.Close()
}