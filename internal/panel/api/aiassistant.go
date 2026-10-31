package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"cozy/internal/panel/ai"
	"cozy/internal/panel/domain"
)

type aiAssistantConfigOutput struct {
	Body domain.AIAssistantConfigView
}

type aiAssistantConfigInput struct {
	Body domain.AIAssistantConfig
}

type aiAssistantConfigPatchInput struct {
	Body struct {
		BotToken     *string  `json:"bot_token,omitempty"`
		BotUsername  *string  `json:"bot_username,omitempty"`
		APIKey       *string  `json:"api_key,omitempty"`
		BaseURL      *string  `json:"base_url,omitempty"`
		ModelName    *string  `json:"model_name,omitempty"`
		ProviderType *string  `json:"provider_type,omitempty"`
		OllamaHost   *string  `json:"ollama_host,omitempty"`
		SystemPrompt *string  `json:"system_prompt,omitempty"`
		MaxPerChat   *int     `json:"max_per_chat,omitempty"`
		MaxPerDay    *int     `json:"max_per_day,omitempty"`
		Enabled      *bool    `json:"enabled,omitempty"`
	}
}

type aiAssistantTestInput struct {
	Body struct {
		ProviderType string `json:"provider_type" enum:"openai,ollama" doc:"Провайдер: openai или ollama"`
		APIKey       string `json:"api_key,omitempty" doc:"API ключ (для openai-совместимых)"`
		BaseURL      string `json:"base_url,omitempty" doc:"Base URL (например, https://api.xkiro.com/v1)"`
		ModelName    string `json:"model_name" doc:"Имя модели (deepseek-chat, qwen-2.5-72b, llama3.1:70b)"`
		OllamaHost   string `json:"ollama_host,omitempty" doc:"Хост Ollama (для provider_type=ollama)"`
	}
}

type aiAssistantTestOutput struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
		Model   string `json:"model,omitempty"`
	}
}

func (h *handlers) registerAIAssistant() {
	huma.Register(h.api, huma.Operation{
		OperationID: "get-ai-assistant-config",
		Method:      http.MethodGet,
		Path:        "/api/v1/ai-assistant/config",
		Summary:     "Получить конфигурацию AI Assistant",
		Tags:        []string{"ai-assistant"},
	}, h.getAIAssistantConfig)

	huma.Register(h.api, huma.Operation{
		OperationID: "update-ai-assistant-config",
		Method:      http.MethodPut,
		Path:        "/api/v1/ai-assistant/config",
		Summary:     "Обновить конфигурацию AI Assistant",
		Tags:        []string{"ai-assistant"},
	}, h.updateAIAssistantConfig)

	huma.Register(h.api, huma.Operation{
		OperationID: "test-ai-assistant-connection",
		Method:      http.MethodPost,
		Path:        "/api/v1/ai-assistant/test",
		Summary:     "Проверить подключение к модели",
		Tags:        []string{"ai-assistant"},
	}, h.testAIAssistantConnection)
}

func (h *handlers) getAIAssistantConfig(ctx context.Context, _ *struct{}) (*aiAssistantConfigOutput, error) {
	store := domain.NewAIAssistantConfigStore(h.d.Store)
	view, err := store.GetConfigView(ctx)
	if err != nil {
		return nil, err
	}
	return &aiAssistantConfigOutput{Body: *view}, nil
}

func (h *handlers) updateAIAssistantConfig(ctx context.Context, in *aiAssistantConfigPatchInput) (*aiAssistantConfigOutput, error) {
	store := domain.NewAIAssistantConfigStore(h.d.Store)

	existing, err := store.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	if in.Body.BotToken != nil {
		existing.BotToken = *in.Body.BotToken
	}
	if in.Body.BotUsername != nil {
		existing.BotUsername = *in.Body.BotUsername
	}
	if in.Body.APIKey != nil {
		existing.APIKey = *in.Body.APIKey
	}
	if in.Body.BaseURL != nil {
		existing.BaseURL = *in.Body.BaseURL
	}
	if in.Body.ModelName != nil {
		existing.ModelName = *in.Body.ModelName
	}
	if in.Body.ProviderType != nil {
		existing.ProviderType = domain.ProviderType(*in.Body.ProviderType)
	}
	if in.Body.OllamaHost != nil {
		existing.OllamaHost = *in.Body.OllamaHost
	}
	if in.Body.SystemPrompt != nil {
		existing.SystemPrompt = *in.Body.SystemPrompt
	}
	if in.Body.MaxPerChat != nil {
		existing.MaxPerChat = *in.Body.MaxPerChat
	}
	if in.Body.MaxPerDay != nil {
		existing.MaxPerDay = *in.Body.MaxPerDay
	}
	if in.Body.Enabled != nil {
		existing.Enabled = *in.Body.Enabled
	}

	if err := store.UpdateConfig(ctx, existing); err != nil {
		return nil, err
	}
	view, err := store.GetConfigView(ctx)
	if err != nil {
		return nil, err
	}
	return &aiAssistantConfigOutput{Body: *view}, nil
}

func (h *handlers) testAIAssistantConnection(ctx context.Context, in *aiAssistantTestInput) (*aiAssistantTestOutput, error) {
	providerType := in.Body.ProviderType
	if providerType == "" {
		providerType = "openai"
	}

	client, err := ai.NewClient(providerType, in.Body.APIKey, in.Body.BaseURL, in.Body.ModelName, in.Body.OllamaHost)
	if err != nil {
		return &aiAssistantTestOutput{Body: struct {
			Success bool   `json:"success"`
			Message string `json:"message,omitempty"`
			Model   string `json:"model,omitempty"`
		}{Success: false, Message: err.Error()}}, nil
	}
	defer client.Close()

	// Send a simple test message
	req := ai.ChatCompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: "user", Content: "Say 'OK' if you can hear me."},
		},
		MaxTokens: 10,
	}
	resp, err := client.ChatComplete(context.Background(), req)
	if err != nil {
		return &aiAssistantTestOutput{Body: struct {
			Success bool   `json:"success"`
			Message string `json:"message,omitempty"`
			Model   string `json:"model,omitempty"`
		}{Success: false, Message: err.Error()}}, nil
	}

	if len(resp.Choices) == 0 {
		return &aiAssistantTestOutput{Body: struct {
			Success bool   `json:"success"`
			Message string `json:"message,omitempty"`
			Model   string `json:"model,omitempty"`
		}{Success: false, Message: "empty response"}}, nil
	}

	return &aiAssistantTestOutput{Body: struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
		Model   string `json:"model,omitempty"`
	}{Success: true, Message: "Connection successful", Model: in.Body.ModelName}}, nil
}
