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

// OpenAIClient implements LLMClient for OpenAI-compatible APIs.
type OpenAIClient struct {
	apiKey   string
	baseURL  string
	model    string
	httpClient *http.Client
}

// NewOpenAIClient creates a new OpenAI-compatible client.
func NewOpenAIClient(apiKey, baseURL, model string) (*OpenAIClient, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// ChatComplete sends a non-streaming chat completion request.
func (c *OpenAIClient) ChatComplete(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Model = c.model
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var respBody ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, err
	}
	return &respBody, nil
}

// ChatStream sends a streaming chat completion request.
func (c *OpenAIClient) ChatStream(ctx context.Context, req ChatCompletionRequest) (Stream, error) {
	req.Model = c.model
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return &openAIStream{resp: resp}, nil
}

// Close closes the client (no-op for HTTP client).
func (c *OpenAIClient) Close() error {
	return nil
}

// openAIStream implements Stream for OpenAI streaming responses.
type openAIStream struct {
	resp *http.Response
}

func (s *openAIStream) Recv() (*ChatCompletionResponse, error) {
	reader := NewSSEReader(s.resp.Body)
	for {
		event, data, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, err
		}
		if event == "data" {
			if data == "[DONE]" {
				return nil, io.EOF
			}
			var resp ChatCompletionResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				continue // skip malformed chunks
			}
			return &resp, nil
		}
	}
}

func (s *openAIStream) Close() error {
	return s.resp.Body.Close()
}

// sseReader parses Server-Sent Events.
type sseReader struct {
	reader *bufio.Reader
}

func NewSSEReader(r io.Reader) *sseReader {
	return &sseReader{reader: bufio.NewReader(r)}
}

func (r *sseReader) Next() (event, data string, err error) {
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if event == "" {
				event = "data"
			}
			return event, data, nil
		}
	}
}