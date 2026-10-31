package tgbot

// AIWords holds the AI Assistant bot's own words in both languages.
type AIWords struct {
	aiWelcome, aiUsage, aiWelcomeTitle, aiHelp, aiError, aiNotConfigured, aiNotBound string
	aiDailyLimit, aiChatLimit, aiUsageToday, aiUsageChat, aiUsageLeft, aiCommands    string
}

var aiRu = AIWords{
	aiWelcome:       "🤖 Добро пожаловать в AI Assistant!\n\nЯ — ваш помощник. Могу помочь с настройкой VPN, ответить на вопросы о подписке или просто поболтать.\n\n📝 Доступные команды:\n/start — начать диалог заново (очистит историю)\n/usage — сколько запросов осталось сегодня\n\nПросто напишите сообщение, и я отвечу!",
	aiUsage:         "📊 <b>Лимиты использования</b>\n\nСегодня использовано: {used_today} из {max_per_day}\nВ этом чате: {used_chat} из {max_per_chat}\n\nОсталось сегодня: {left_today}\nВ этом чате: {left_chat}",
	aiWelcomeTitle:  "🤖 AI Assistant",
	aiHelp:          "📖 <b>Помощь</b>\n\n/start — начать диалог заново (очищает историю)\n/usage — показать оставшиеся лимиты\n\nПросто пишите сообщения — я отвечу.",
	aiError:         "⚠️ Произошла ошибка при обращении к AI. Попробуйте ещё раз.",
	aiNotConfigured: "⚙️ AI Assistant пока не настроен администратором.",
	aiNotBound:      "🔗 Ваш Telegram не привязан к подписке. Привяжите аккаунт в боте подписок.",
	aiDailyLimit:    "📅 Дневной лимит исчерпан ({max_per_day} запросов в сутки). Попробуйте завтра.",
	aiChatLimit:     "💬 Лимит сообщений в этом чате исчерпан ({max_per_chat}). Напишите /start для нового диалога.",
	aiUsageToday:    "Сегодня: {used} из {limit}",
	aiUsageChat:     "В чате: {used} из {limit}",
	aiUsageLeft:     "Осталось: {left}",
	aiCommands:      "📋 Команды:\n/start — новый диалог\n/usage — лимиты",
}

var aiEn = AIWords{
	aiWelcome:       "🤖 Welcome to AI Assistant!\n\nI'm your assistant. I can help with VPN setup, answer subscription questions, or just chat.\n\n📝 Commands:\n/start — start a new dialog (clears history)\n/usage — how many requests left today\n\nJust send a message and I'll reply!",
	aiUsage:         "📊 <b>Usage Limits</b>\n\nUsed today: {used_today} of {max_per_day}\nIn this chat: {used_chat} of {max_per_chat}\n\nLeft today: {left_today}\nIn this chat: {left_chat}",
	aiWelcomeTitle:  "🤖 AI Assistant",
	aiHelp:          "📖 <b>Help</b>\n\n/start — start a new dialog (clears history)\n/usage — show remaining limits\n\nJust send messages and I'll reply!",
	aiError:         "⚠️ An error occurred while contacting AI. Please try again.",
	aiNotConfigured: "⚙️ AI Assistant is not configured by the administrator.",
	aiNotBound:      "🔗 Your Telegram is not bound to a subscription. Link your account in the subscription bot.",
	aiDailyLimit:    "📅 Daily limit reached ({max_per_day} requests per day). Try again tomorrow.",
	aiChatLimit:     "💬 Chat message limit reached ({max_per_chat}). Use /start for a new dialog.",
	aiUsageToday:    "Today: {used} of {limit}",
	aiUsageChat:     "In chat: {used} of {limit}",
	aiUsageLeft:     "Left: {left}",
	aiCommands:      "📋 Commands:\n/start — new dialog\n/usage — limits",
}

func aiWordsFor(lang string) *AIWords {
	if lang == "en" {
		return &aiEn
	}
	return &aiRu
}
