package main

import (
	"errors"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/api"
)

func validateHostedProviders(config api.Config) error {
	if !config.Hosted {
		return nil
	}
	if config.OpenAIKey != "" || config.AnthropicKey != "" || config.GeminiKey != "" {
		return errors.New("hosted_provider_environment_keys_forbidden")
	}
	if config.OpenAIURL != "https://api.openai.com/v1" || config.AnthropicURL != "https://api.anthropic.com/v1" || config.GeminiURL != api.DefaultGeminiURL {
		return errors.New("hosted_provider_urls_must_be_official")
	}
	return nil
}
