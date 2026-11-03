package main

import (
	"testing"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/api"
)

func TestGeminiHostedConfiguration(t *testing.T) {
	base := api.Config{Hosted: true, OpenAIURL: "https://api.openai.com/v1", AnthropicURL: "https://api.anthropic.com/v1", GeminiURL: api.DefaultGeminiURL}
	if err := validateHostedProviders(base); err != nil {
		t.Fatal(err)
	}
	withKey := base
	withKey.GeminiKey = "synthetic-forbidden-hosted-key"
	if err := validateHostedProviders(withKey); err == nil || err.Error() != "hosted_provider_environment_keys_forbidden" {
		t.Fatal("hosted Gemini key was not rejected")
	}
	custom := base
	custom.GeminiURL = "https://other.example.test/v1"
	if err := validateHostedProviders(custom); err == nil || err.Error() != "hosted_provider_urls_must_be_official" {
		t.Fatal("hosted custom Gemini URL was not rejected")
	}
	withKey.Hosted = false
	if err := validateHostedProviders(withKey); err != nil {
		t.Fatal("self-hosted server credential rejected")
	}
}
