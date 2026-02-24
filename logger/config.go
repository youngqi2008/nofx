package logger

// Config is the logger configuration (simplified version)
type Config struct {
	Level string `json:"level"` // Log level: debug, info, warn, error (default: info)
}

// SetDefaults sets default values
func (c *Config) SetDefaults() {
	if c.Level == "" {
		c.Level = "info"
	}
}

// TelegramConfig is the configuration for Telegram log hook (used by telegram_hook.go).
type TelegramConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}
