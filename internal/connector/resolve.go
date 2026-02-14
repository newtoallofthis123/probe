package connector

import (
	"fmt"
	"strings"

	"github.com/newtoallofthis/probe/internal/config"
)

// Resolve creates the appropriate Connector based on config.
func Resolve(cfg *config.Config) (Connector, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg)
	}

	switch provider {
	case "openai", "":
		return NewOpenAI(cfg.BaseURL, cfg.APIKey), nil
	case "anthropic":
		return NewAnthropic(cfg.APIKey), nil
	case "google":
		return NewGoogle(cfg.APIKey), nil
	default:
		return nil, fmt.Errorf("unknown provider '%s': must be openai, anthropic, or google", provider)
	}
}

func detectProvider(cfg *config.Config) string {
	if strings.HasPrefix(cfg.APIKey, "sk-ant-") {
		return "anthropic"
	}
	if isLocalhost(cfg.BaseURL) {
		return "openai"
	}
	return "openai"
}
