-- +goose Up
-- AI Assistant: global config (singleton)
CREATE TABLE ai_assistant_config (
    id                  INTEGER PRIMARY KEY DEFAULT 1,  -- singleton
    enabled             BOOLEAN NOT NULL DEFAULT FALSE,
    bot_token           TEXT,                            -- encrypted at rest
    bot_username        TEXT,
    api_key             TEXT,                            -- encrypted
    base_url            TEXT,                            -- e.g., https://api.xkiro.com/v1
    model_name          TEXT,                            -- e.g., deepseek-chat, qwen-2.5-72b
    provider_type       TEXT NOT NULL DEFAULT 'openai',  -- 'openai' | 'ollama'
    ollama_host         TEXT,                            -- for ollama: http://localhost:11434
    system_prompt       TEXT,                            -- admin additions to immutable core
    max_per_chat        INTEGER NOT NULL DEFAULT 50,
    max_per_day         INTEGER NOT NULL DEFAULT 200,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

-- Daily usage per user
CREATE TABLE ai_assistant_usage (
    user_id     INTEGER NOT NULL,
    date        INTEGER NOT NULL,    -- Unix day (UTC midnight)
    count       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, date)
);

-- Per-conversation tracking
CREATE TABLE ai_assistant_conversation (
    chat_id         INTEGER PRIMARY KEY,        -- Telegram chat ID
    user_id         INTEGER NOT NULL,
    message_count   INTEGER NOT NULL DEFAULT 0,
    messages        TEXT NOT NULL DEFAULT '[]', -- JSON array of {role, content}
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- Settings for the AI Assistant bot (singleton stored in settings table)
-- Keys: ai_assistant_enabled, ai_assistant_token, ai_assistant_bot, ai_assistant_config, ai_assistant_secret

-- +goose Down
DROP TABLE ai_assistant_conversation;
DROP TABLE ai_assistant_usage;
DROP TABLE ai_assistant_config;