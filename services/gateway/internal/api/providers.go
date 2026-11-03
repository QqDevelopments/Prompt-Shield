package api

import (
	"encoding/json"
	"io"
	"net/http"
)

const DefaultGeminiURL = "https://generativelanguage.googleapis.com/v1beta/openai"

func providerForPath(path string) string {
	switch path {
	case "/v1/messages", "/public/v1/messages":
		return "anthropic"
	case "/v1/gemini/chat/completions", "/public/v1/gemini/chat/completions":
		return "gemini"
	default:
		return "openai"
	}
}

func validProvider(provider string) bool {
	return provider == "openai" || provider == "anthropic" || provider == "gemini"
}

func (s *Server) providerConfiguration(provider string) (string, string) {
	switch provider {
	case "anthropic":
		return s.config.AnthropicURL, s.config.AnthropicKey
	case "gemini":
		return s.config.GeminiURL, s.config.GeminiKey
	default:
		return s.config.OpenAIURL, s.config.OpenAIKey
	}
}

// Google can report invalid API keys as a structured 400/403 response.
// Inspect bounded error metadata without exposing its message.
func providerKeyRejected(provider string, response *http.Response) bool {
	if response.StatusCode == http.StatusUnauthorized {
		return true
	}
	if provider != "gemini" || (response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusForbidden) {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	defer clear(raw)
	if err != nil {
		return false
	}
	var body struct {
		Error struct {
			Code    json.RawMessage `json:"code"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return false
	}
	if string(body.Error.Code) == `"API_KEY_INVALID"` {
		return true
	}
	for _, detail := range body.Error.Details {
		if detail.Reason == "API_KEY_INVALID" {
			return true
		}
	}
	return false
}
