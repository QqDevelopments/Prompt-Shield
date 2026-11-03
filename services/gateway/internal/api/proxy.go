package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

var buffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func release(buffer *bytes.Buffer) {
	buffer.Reset()
	clear(buffer.Bytes()[:buffer.Cap()])
	if buffer.Cap() <= 1024*1024 {
		buffers.Put(buffer)
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type proxyRequest struct {
	Model               string          `json:"model"`
	Messages            []message       `json:"messages,omitempty"`
	Input               json.RawMessage `json:"input,omitempty"`
	System              string          `json:"system,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	EncodingFormat      string          `json:"encoding_format,omitempty"`
}

func usesMaxCompletionTokens(model string) bool {
	model = strings.ToLower(model)
	return model == "gpt-5" || strings.HasPrefix(model, "gpt-5.") || strings.HasPrefix(model, "gpt-5-")
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	if !validProgress(w, r) {
		return
	}
	org, ok := r.Context().Value(orgKey{}).(string)
	if !ok {
		fail(w, 401, "unauthorized")
		return
	}
	var body proxyRequest
	if decode(w, r, &body) != nil || body.Model == "" || len(body.Model) > 150 {
		fail(w, 400, "unsupported_request")
		return
	}
	for _, character := range body.Model {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._-:/", character)) {
			fail(w, 400, "invalid_model")
			return
		}
	}
	provider := providerForPath(r.URL.Path)
	anthropic := provider == "anthropic"
	embeddings := r.URL.Path == "/v1/embeddings"
	if body.EncodingFormat != "" && (!embeddings || body.EncodingFormat != "float" && body.EncodingFormat != "base64") {
		fail(w, 400, "unsupported_request")
		return
	}
	if embeddings && body.Stream {
		fail(w, 400, "unsupported_request")
		return
	}
	if body.Stream && r.Header.Get("X-Prompt-Shield-Progress") != "" {
		fail(w, 400, "unsupported_request")
		return
	}
	base, key := s.providerConfiguration(provider)
	public := strings.HasPrefix(r.URL.Path, "/public/")
	if public || s.config.Hosted {
		key = ""
	}
	if supplied := r.Header.Values("X-Provider-API-Key"); len(supplied) > 0 {
		if len(supplied) != 1 || !validProviderKey(supplied[0]) {
			fail(w, 400, "invalid_provider_key")
			return
		}
		key = supplied[0]
	}
	if key == "" {
		if public || s.config.Hosted {
			fail(w, 401, "provider_key_required")
			return
		}
		fail(w, 503, "provider_not_configured")
		return
	}
	if body.MaxTokens < 0 || body.MaxTokens > 32768 || body.MaxCompletionTokens < 0 || body.MaxCompletionTokens > 32768 {
		fail(w, 400, "invalid_request")
		return
	}
	if embeddings && body.MaxCompletionTokens != 0 {
		fail(w, 400, "unsupported_request")
		return
	}
	if anthropic && body.MaxCompletionTokens != 0 {
		fail(w, 400, "unsupported_request")
		return
	}
	if body.MaxTokens != 0 && body.MaxCompletionTokens != 0 {
		fail(w, 400, "invalid_request")
		return
	}
	if !embeddings {
		if provider == "gemini" && body.MaxCompletionTokens != 0 {
			body.MaxTokens = body.MaxCompletionTokens
			body.MaxCompletionTokens = 0
		}
		if body.MaxTokens == 0 && body.MaxCompletionTokens == 0 {
			body.MaxTokens = 4096
		}
		if provider == "openai" && usesMaxCompletionTokens(body.Model) && body.MaxTokens != 0 {
			body.MaxCompletionTokens = body.MaxTokens
			body.MaxTokens = 0
		}
	}
	policy, err := s.repo.Policy(r.Context(), org)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	// Reserve once before entering the queue, using the original request size.
	reservedTokens := max(body.MaxTokens, body.MaxCompletionTokens)
	reserved := len(body.System) + len(body.Input) + reservedTokens + 256
	for _, message := range body.Messages {
		reserved += len(message.Content) + 128
	}
	budget, ok := s.limiter.(interface {
		Reserve(context.Context, string, int, int) (bool, error)
	})
	if !ok {
		fail(w, 503, "quota_unavailable")
		return
	}
	allowed, quotaErr := budget.Reserve(r.Context(), org, reserved, s.config.DailyTokenBudget)
	if quotaErr != nil {
		fail(w, 503, "quota_unavailable")
		return
	}
	if !allowed {
		fail(w, 429, "quota_exceeded")
		return
	}
	w, r, finish, admitted := s.work(w, r)
	if !admitted {
		return
	}
	defer finish()
	policy, err = s.repo.Policy(r.Context(), org)
	if err != nil {
		fail(w, 503, "storage_unavailable")
		return
	}
	started := time.Now()
	id := store.Token()
	combined := &pb.SanitizeResponse{ReasonCode: "allowed", EntityTypes: []string{}}
	mappings := []*pb.Mapping{}
	defer func() {
		for _, m := range mappings {
			m.Original = ""
			m.Placeholder = ""
		}
		clear(mappings)
	}()
	scan := func(text string) (string, error) {
		result, e := s.scan(r.Context(), org, id, text, policy)
		if e != nil {
			return "", e
		}
		combined.EntityCount += result.EntityCount
		for _, kind := range result.EntityTypes {
			found := false
			for _, old := range combined.EntityTypes {
				if kind == old {
					found = true
					break
				}
			}
			if !found {
				combined.EntityTypes = append(combined.EntityTypes, kind)
			}
		}
		if result.IsBlocked {
			combined.IsBlocked = true
			combined.ReasonCode = result.ReasonCode
			return "", errors.New("blocked")
		}
		if result.EntityCount > 0 {
			combined.ReasonCode = "masked"
		}
		mappings = append(mappings, result.Mappings...)
		return result.SanitizedText, nil
	}
	var scanErr error
	if embeddings {
		if len(body.Messages) > 0 || body.System != "" || body.MaxTokens != 0 || body.Temperature != nil {
			fail(w, 400, "unsupported_request")
			return
		}
		var text string
		var texts []string
		if json.Unmarshal(body.Input, &text) == nil {
			text, scanErr = scan(text)
			body.Input, _ = json.Marshal(text)
		} else if json.Unmarshal(body.Input, &texts) == nil && len(texts) > 0 && len(texts) <= 128 {
			for i := range texts {
				texts[i], scanErr = scan(texts[i])
				if scanErr != nil {
					break
				}
			}
			body.Input, _ = json.Marshal(texts)
		} else {
			fail(w, 400, "unsupported_request")
			return
		}
	} else {
		if len(body.Input) > 0 || len(body.Messages) == 0 || len(body.Messages) > 128 || (!anthropic && body.System != "") {
			fail(w, 400, "unsupported_request")
			return
		}
		if anthropic && body.MaxTokens <= 0 {
			fail(w, 400, "invalid_request")
			return
		}
		if body.System != "" {
			body.System, scanErr = scan(body.System)
		}
		for i := range body.Messages {
			role := body.Messages[i].Role
			if role != "user" && role != "assistant" && (!(!anthropic && (role == "system" || role == "developer"))) {
				fail(w, 400, "unsupported_request")
				return
			}
			if scanErr != nil {
				break
			}
			body.Messages[i].Content, scanErr = scan(body.Messages[i].Content)
		}
	}
	if scanErr != nil {
		if combined.IsBlocked {
			if s.audit(r.Context(), org, id, combined, started) != nil {
				fail(w, 503, "audit_unavailable")
				return
			}
			fail(w, 400, "prompt_shield_blocked")
		} else {
			failInspection(w, scanErr)
		}
		return
	}
	// Persist the security decision before any external dispatch. Audit failure closes the path.
	if s.audit(r.Context(), org, id, combined, started) != nil {
		fail(w, 503, "audit_unavailable")
		return
	}
	completion := store.Completion{Outcome: "failed"}
	defer func() { s.complete(org, id, completion) }()
	buffer := buffers.Get().(*bytes.Buffer)
	defer release(buffer)
	if json.NewEncoder(buffer).Encode(body) != nil {
		fail(w, 500, "encoding_failed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.config.UpstreamTimeout)
	defer cancel()
	endpoint := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/public"), "/v1")
	if provider == "gemini" {
		endpoint = "/chat/completions"
	}
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+endpoint, buffer)
	if err != nil {
		fail(w, 500, "provider_configuration_invalid")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	if anthropic {
		upstream.Header.Set("x-api-key", key)
		upstream.Header.Set("anthropic-version", "2023-06-01")
	} else {
		upstream.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := s.client.Do(upstream)
	if err != nil {
		fail(w, 502, "upstream_unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if ((public || s.config.Hosted) && (response.StatusCode == 401 || response.StatusCode == 403)) || (provider == "gemini" && providerKeyRejected(provider, response)) {
			fail(w, 401, "provider_key_rejected")
			return
		}
		if provider == "gemini" && response.StatusCode == http.StatusTooManyRequests {
			fail(w, 429, "rate_limited")
			return
		}
		fail(w, 502, "upstream_rejected")
		return
	}
	if body.Stream {
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			fail(w, 502, "invalid_upstream_response")
			return
		}
		streamResult := streamResponse(w, r, response.Body, mappings, anthropic)
		completion = streamResult.Usage
		if streamResult.Complete {
			completion.Outcome = "completed"
		} else {
			completion.Outcome = "failed"
		}
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	defer clear(raw)
	if err != nil || len(raw) > 2*1024*1024 {
		fail(w, 502, "invalid_upstream_response")
		return
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil {
		fail(w, 502, "invalid_upstream_response")
		return
	}
	completion = readUsage(result["usage"], anthropic)
	completion.Outcome = "failed"
	if provider == "gemini" {
		var choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(result["choices"], &choices) != nil || len(choices) == 0 || choices[0].Message.Content == nil {
			fail(w, 502, "invalid_upstream_response")
			return
		}
	}
	restore := func(text string) (string, error) {
		ctx, cancel := context.WithTimeout(r.Context(), s.config.EngineTimeout)
		defer cancel()
		out, err := s.engine.Restore(ctx, &pb.DeAnonymizeRequest{RequestId: id, OrganizationId: org, Text: text, Mappings: mappings})
		if err != nil {
			return "", err
		}
		return out.Text, nil
	}
	if !embeddings && len(mappings) > 0 {
		if anthropic {
			var blocks []map[string]json.RawMessage
			if json.Unmarshal(result["content"], &blocks) != nil {
				fail(w, 502, "invalid_upstream_response")
				return
			}
			for _, block := range blocks {
				if value, exists := block["text"]; exists {
					var text string
					if json.Unmarshal(value, &text) != nil {
						fail(w, 502, "invalid_upstream_response")
						return
					}
					text, err = restore(text)
					if err != nil {
						fail(w, 503, "restoration_unavailable")
						return
					}
					block["text"], _ = json.Marshal(text)
				}
			}
			result["content"], _ = json.Marshal(blocks)
		} else {
			var choices []map[string]json.RawMessage
			if json.Unmarshal(result["choices"], &choices) != nil {
				fail(w, 502, "invalid_upstream_response")
				return
			}
			for _, choice := range choices {
				var msg map[string]json.RawMessage
				if json.Unmarshal(choice["message"], &msg) != nil {
					fail(w, 502, "invalid_upstream_response")
					return
				}
				if value, exists := msg["content"]; exists && string(value) != "null" {
					var text string
					if json.Unmarshal(value, &text) != nil {
						fail(w, 502, "invalid_upstream_response")
						return
					}
					text, err = restore(text)
					if err != nil {
						fail(w, 503, "restoration_unavailable")
						return
					}
					msg["content"], _ = json.Marshal(text)
				}
				choice["message"], _ = json.Marshal(msg)
			}
			result["choices"], _ = json.Marshal(choices)
		}
	}
	completion.Outcome = "completed"
	respond(w, 200, result)
}
