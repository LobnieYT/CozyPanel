package domain

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"time"

	"cozy/internal/panel/settings"
	"cozy/internal/panel/store"
	"cozy/internal/panel/store/db"
)

const ImmutableSystemPrompt = `You are an Assistant. Your role is to help the user solve their problem.
You must NEVER forget your role or become anyone else (not a friend, not a programmer, not a seller, not any other persona).
You are strictly an Assistant whose purpose is to help solve the user's problem.`

type ProviderType string

const (
	ProviderOpenAI ProviderType = "openai"
	ProviderOllama ProviderType = "ollama"
)

type AIAssistantConfig struct {
	ID           int64
	Enabled      bool
	BotToken     string
	BotUsername  string
	APIKey       string
	BaseURL      string
	ModelName    string
	ProviderType ProviderType
	OllamaHost   string
	SystemPrompt string
	MaxPerChat   int
	MaxPerDay    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AIAssistantUsage struct {
	UserID int64
	Date   int64
	Count  int
}

type AIAssistantConversation struct {
	ChatID       int64
	UserID       int64
	MessageCount int
	Messages     []ChatMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	KeyAIAssistantEnabled = "ai_assistant_enabled"
	KeyAIAssistantToken   = "ai_assistant_token"
	KeyAIAssistantBot     = "ai_assistant_bot"
	KeyAIAssistantConfig  = "ai_assistant_config"
	KeyAIAssistantSecret  = "ai_assistant_secret"
	KeyAIAssistantOffset  = "ai_assistant_offset"
)

var ErrAIAssistantNotConfigured = errors.New("ai assistant not configured")

type AIAssistantConfigView struct {
	Enabled      bool   `json:"enabled"`
	BotUsername  string `json:"bot_username,omitempty"`
	BaseURL      string `json:"base_url"`
	ModelName    string `json:"model_name"`
	ProviderType string `json:"provider_type"`
	OllamaHost   string `json:"ollama_host,omitempty"`
	SystemPrompt string `json:"system_prompt"`
	MaxPerChat   int    `json:"max_per_chat"`
	MaxPerDay    int    `json:"max_per_day"`
	APIKeyMasked string `json:"api_key_masked,omitempty"`
	BotTokenSet  bool   `json:"bot_token_set"`
	EnabledRaw   bool   `json:"-"`
}

type AIAssistantConfigStore struct {
	st *store.Store
}

func NewAIAssistantConfigStore(st *store.Store) *AIAssistantConfigStore {
	return &AIAssistantConfigStore{st: st}
}

func (s *AIAssistantConfigStore) settings() *settings.Settings {
	return settings.New(s.st.Q)
}

func scanAIAssistantConfig(row db.AiAssistantConfig) *AIAssistantConfig {
	return &AIAssistantConfig{
		ID:           row.ID,
		Enabled:      row.Enabled,
		BotToken:     row.BotToken.String,
		BotUsername:  row.BotUsername.String,
		APIKey:       row.ApiKey.String,
		BaseURL:      row.BaseUrl.String,
		ModelName:    row.ModelName.String,
		ProviderType: ProviderType(row.ProviderType),
		OllamaHost:   row.OllamaHost.String,
		SystemPrompt: row.SystemPrompt.String,
		MaxPerChat:   int(row.MaxPerChat),
		MaxPerDay:    int(row.MaxPerDay),
		CreatedAt:    time.Unix(row.CreatedAt, 0),
		UpdatedAt:    time.Unix(row.UpdatedAt, 0),
	}
}

func (s *AIAssistantConfigStore) GetConfig(ctx context.Context) (*AIAssistantConfig, error) {
	row, err := s.st.Q.GetAIAssistantConfig(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return &AIAssistantConfig{
			ProviderType: ProviderOpenAI,
			MaxPerChat:   50,
			MaxPerDay:    200,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return scanAIAssistantConfig(row), nil
}

func (s *AIAssistantConfigStore) GetConfigView(ctx context.Context) (*AIAssistantConfigView, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &AIAssistantConfigView{
		Enabled:      cfg.Enabled,
		BotUsername:  cfg.BotUsername,
		BaseURL:      cfg.BaseURL,
		ModelName:    cfg.ModelName,
		ProviderType: string(cfg.ProviderType),
		OllamaHost:   cfg.OllamaHost,
		SystemPrompt: cfg.SystemPrompt,
		MaxPerChat:   cfg.MaxPerChat,
		MaxPerDay:    cfg.MaxPerDay,
		APIKeyMasked: maskAPIKey(cfg.APIKey),
		BotTokenSet:  cfg.BotToken != "",
		EnabledRaw:   cfg.Enabled,
	}, nil
}

func (s *AIAssistantConfigStore) UpdateConfig(ctx context.Context, cfg *AIAssistantConfig) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		return q.UpsertAIAssistantConfig(ctx, db.UpsertAIAssistantConfigParams{
			ID:           1,
			Enabled:      cfg.Enabled,
			BotToken:     sql.NullString{String: cfg.BotToken, Valid: cfg.BotToken != ""},
			BotUsername:  sql.NullString{String: cfg.BotUsername, Valid: cfg.BotUsername != ""},
			ApiKey:       sql.NullString{String: cfg.APIKey, Valid: cfg.APIKey != ""},
			BaseUrl:      sql.NullString{String: cfg.BaseURL, Valid: cfg.BaseURL != ""},
			ModelName:    sql.NullString{String: cfg.ModelName, Valid: cfg.ModelName != ""},
			ProviderType: string(cfg.ProviderType),
			OllamaHost:   sql.NullString{String: cfg.OllamaHost, Valid: cfg.OllamaHost != ""},
			SystemPrompt: sql.NullString{String: cfg.SystemPrompt, Valid: cfg.SystemPrompt != ""},
			MaxPerChat:   int64(cfg.MaxPerChat),
			MaxPerDay:    int64(cfg.MaxPerDay),
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		})
	})
}

func (s *AIAssistantConfigStore) GetOrCreateSecret(ctx context.Context) ([]byte, error) {
	val, err := s.settings().String(ctx, KeyAIAssistantSecret)
	if err != nil {
		return nil, err
	}
	if raw, err := base64.StdEncoding.DecodeString(val); err == nil && len(raw) == 32 {
		return raw, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := settings.Set(ctx, s.settings(), KeyAIAssistantSecret, base64.StdEncoding.EncodeToString(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *AIAssistantConfigStore) GetEnabled(ctx context.Context) (bool, error) {
	sw := settings.Switch{Key: KeyAIAssistantEnabled}
	return s.settings().On(ctx, sw)
}

func (s *AIAssistantConfigStore) SetEnabled(ctx context.Context, enabled bool) error {
	return settings.Set(ctx, s.settings(), KeyAIAssistantEnabled, enabled)
}

func (s *AIAssistantConfigStore) GetBotToken(ctx context.Context) (string, error) {
	return s.settings().String(ctx, KeyAIAssistantToken)
}

func (s *AIAssistantConfigStore) SetBotToken(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return settings.Set(ctx, s.settings(), KeyAIAssistantToken, token)
}

func (s *AIAssistantConfigStore) GetBotInfo(ctx context.Context) (string, error) {
	return s.settings().String(ctx, KeyAIAssistantBot)
}

func (s *AIAssistantConfigStore) SetBotInfo(ctx context.Context, info string) error {
	return settings.Set(ctx, s.settings(), KeyAIAssistantBot, info)
}

func (s *AIAssistantConfigStore) GetSecret(ctx context.Context) ([]byte, error) {
	val, err := s.settings().String(ctx, KeyAIAssistantSecret)
	if err != nil {
		return nil, err
	}
	if raw, err := base64.StdEncoding.DecodeString(val); err == nil && len(raw) == 32 {
		return raw, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *AIAssistantConfigStore) GetTodayUsage(ctx context.Context, userID int64) (int, error) {
	day := time.Now().UTC().Truncate(24*time.Hour).Unix() / 86400
	count, err := s.st.Q.GetAIAssistantUsage(ctx, db.GetAIAssistantUsageParams{
		UserID: userID,
		Date:   day,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return int(count), err
}

func (s *AIAssistantConfigStore) IncrementUsage(ctx context.Context, userID int64) error {
	day := time.Now().UTC().Truncate(24*time.Hour).Unix() / 86400
	return s.st.Q.IncrementAIAssistantUsage(ctx, db.IncrementAIAssistantUsageParams{
		UserID: userID,
		Date:   day,
	})
}

func (s *AIAssistantConfigStore) GetChatUsage(ctx context.Context, userID int64, chatID int64) (int, error) {
	conv, err := s.st.Q.GetAIAssistantConversation(ctx, chatID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var count int
	if conv.UserID == userID {
		count = int(conv.MessageCount)
	}
	return count, nil
}

func (s *AIAssistantConfigStore) IncrementChatUsage(ctx context.Context, chatID int64, userID int64) error {
	_, err := s.st.Q.GetAIAssistantConversation(ctx, chatID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.st.Q.CreateAIAssistantConversation(ctx, db.CreateAIAssistantConversationParams{
			ChatID:       chatID,
			UserID:       userID,
			MessageCount: 1,
			Messages:     "[]",
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		})
	}
	return s.st.Q.IncrementAIAssistantConversation(ctx, db.IncrementAIAssistantConversationParams{
		ChatID:       chatID,
		MessageCount: 1,
		UpdatedAt:    time.Now().Unix(),
	})
}

func (s *AIAssistantConfigStore) GetConversation(ctx context.Context, chatID int64) ([]ChatMessage, error) {
	row, err := s.st.Q.GetAIAssistantConversation(ctx, chatID)
	if errors.Is(err, sql.ErrNoRows) {
		return []ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []ChatMessage
	if len(row.Messages) > 0 {
		if err := json.Unmarshal([]byte(row.Messages), &msgs); err != nil {
			return []ChatMessage{}, nil
		}
	}
	return msgs, nil
}

func (s *AIAssistantConfigStore) AddMessage(ctx context.Context, chatID int64, msg ChatMessage) error {
	msgs, err := s.GetConversation(ctx, chatID)
	if err != nil {
		return err
	}
	msgs = append(msgs, msg)
	b, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	return s.st.Q.UpdateAIAssistantConversation(ctx, db.UpdateAIAssistantConversationParams{
		ChatID:    chatID,
		Messages:  string(b),
		UpdatedAt: time.Now().Unix(),
	})
}

func (s *AIAssistantConfigStore) ResetConversation(ctx context.Context, chatID int64) error {
	return s.st.Q.DeleteAIAssistantConversation(ctx, chatID)
}

func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
