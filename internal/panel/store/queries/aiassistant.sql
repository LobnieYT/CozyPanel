-- AI Assistant Config (singleton)

-- name: GetAIAssistantConfig :one
SELECT id, enabled, bot_token, bot_username, api_key, base_url, model_name, provider_type, ollama_host, system_prompt, max_per_chat, max_per_day, created_at, updated_at
FROM ai_assistant_config
WHERE id = 1;

-- name: UpsertAIAssistantConfig :exec
INSERT INTO ai_assistant_config (id, enabled, bot_token, bot_username, api_key, base_url, model_name, provider_type, ollama_host, system_prompt, max_per_chat, max_per_day, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    enabled = excluded.enabled,
    bot_token = excluded.bot_token,
    bot_username = excluded.bot_username,
    api_key = excluded.api_key,
    base_url = excluded.base_url,
    model_name = excluded.model_name,
    provider_type = excluded.provider_type,
    ollama_host = excluded.ollama_host,
    system_prompt = excluded.system_prompt,
    max_per_chat = excluded.max_per_chat,
    max_per_day = excluded.max_per_day,
    updated_at = excluded.updated_at;

-- AI Assistant Usage (daily per user)

-- name: GetAIAssistantUsage :one
SELECT count FROM ai_assistant_usage WHERE user_id = ? AND date = ?;

-- name: IncrementAIAssistantUsage :exec
INSERT INTO ai_assistant_usage (user_id, date, count) VALUES (?, ?, 1)
ON CONFLICT(user_id, date) DO UPDATE SET count = count + 1;

-- AI Assistant Conversation (per chat)

-- name: GetAIAssistantConversation :one
SELECT chat_id, user_id, message_count, messages, created_at, updated_at
FROM ai_assistant_conversation WHERE chat_id = ?;

-- name: CreateAIAssistantConversation :exec
INSERT INTO ai_assistant_conversation (chat_id, user_id, message_count, messages, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: IncrementAIAssistantConversation :exec
UPDATE ai_assistant_conversation SET message_count = message_count + ?, updated_at = ? WHERE chat_id = ?;

-- name: UpdateAIAssistantConversation :exec
UPDATE ai_assistant_conversation SET messages = ?, updated_at = ? WHERE chat_id = ?;

-- name: DeleteAIAssistantConversation :exec
DELETE FROM ai_assistant_conversation WHERE chat_id = ?;