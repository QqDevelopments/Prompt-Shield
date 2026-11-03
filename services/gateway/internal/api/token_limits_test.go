package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

func TestProxyTokenLimits(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, model, limits, field string
		limit, status, budget                int
	}{
		{"gpt5 default", "chat/completions", "gpt-5.6-luna", ``, "max_completion_tokens", 4096, 200, 0},
		{"gpt5 base", "chat/completions", "gpt-5", ``, "max_completion_tokens", 4096, 200, 0},
		{"gpt5 suffix", "chat/completions", "gpt-5-mini", ``, "max_completion_tokens", 4096, 200, 0},
		{"unrelated prefix", "chat/completions", "gpt-50", ``, "max_tokens", 4096, 200, 0},
		{"gpt5 legacy", "chat/completions", "gpt-5.6-luna", `,"max_tokens":512`, "max_completion_tokens", 512, 200, 0},
		{"gpt5 explicit", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":512`, "max_completion_tokens", 512, 200, 0},
		{"gpt5 zero default", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":0`, "max_completion_tokens", 4096, 200, 0},
		{"other default", "chat/completions", "fixture-model", ``, "max_tokens", 4096, 200, 0},
		{"other legacy", "chat/completions", "fixture-model", `,"max_tokens":512`, "max_tokens", 512, 200, 0},
		{"other modern", "chat/completions", "fixture-model", `,"max_completion_tokens":512`, "max_completion_tokens", 512, 200, 0},
		{"anthropic default", "messages", "claude-fixture", ``, "max_tokens", 4096, 200, 0},
		{"gemini default", "gemini/chat/completions", "gemini-fixture", ``, "max_tokens", 4096, 200, 0},
		{"gemini explicit", "gemini/chat/completions", "gemini-fixture", `,"max_tokens":512`, "max_tokens", 512, 200, 0},
		{"gemini modern normalized", "gemini/chat/completions", "gemini-fixture", `,"max_completion_tokens":512`, "max_tokens", 512, 200, 0},
		{"gemini route ignores gpt model naming", "gemini/chat/completions", "gpt-5", ``, "max_tokens", 4096, 200, 0},
		{"gemini conflicting limits", "gemini/chat/completions", "gemini-fixture", `,"max_tokens":512,"max_completion_tokens":512`, "", 0, 400, 0},
		{"gemini quota denied", "gemini/chat/completions", "gemini-fixture", `,"max_completion_tokens":1024`, "", 0, 429, 1000},
		{"anthropic explicit", "messages", "claude-fixture", `,"max_tokens":512`, "max_tokens", 512, 200, 0},
		{"anthropic modern rejected", "messages", "claude-fixture", `,"max_completion_tokens":512`, "", 0, 400, 0},
		{"embeddings no limit", "embeddings", "embedding-fixture", ``, "", 0, 200, 0},
		{"embeddings legacy rejected", "embeddings", "embedding-fixture", `,"max_tokens":512`, "", 0, 400, 0},
		{"embeddings modern rejected", "embeddings", "embedding-fixture", `,"max_completion_tokens":512`, "", 0, 400, 0},
		{"gpt5 conflict", "chat/completions", "gpt-5.6-luna", `,"max_tokens":512,"max_completion_tokens":512`, "", 0, 400, 0},
		{"other conflict", "chat/completions", "fixture-model", `,"max_tokens":512,"max_completion_tokens":512`, "", 0, 400, 0},
		{"negative legacy", "chat/completions", "gpt-5.6-luna", `,"max_tokens":-1`, "", 0, 400, 0},
		{"negative modern", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":-1`, "", 0, 400, 0},
		{"excess legacy", "chat/completions", "gpt-5.6-luna", `,"max_tokens":32769`, "", 0, 400, 0},
		{"excess modern", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":32769`, "", 0, 400, 0},
		{"maximum modern", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":32768`, "max_completion_tokens", 32768, 200, 0},
		{"malformed modern", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":"512"`, "", 0, 400, 0},
		{"modern quota denied", "chat/completions", "gpt-5.6-luna", `,"max_completion_tokens":1024`, "", 0, 429, 1000},
		{"legacy converted quota denied", "chat/completions", "gpt-5.6-luna", `,"max_tokens":1024`, "", 0, 429, 1000},
		{"modern small quota allowed", "chat/completions", "fixture-model", `,"max_completion_tokens":512`, "max_completion_tokens", 512, 200, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				for _, field := range []string{"max_tokens", "max_completion_tokens"} {
					value, present := wire[field]
					if field != tc.field {
						if present {
							t.Errorf("unexpected upstream field %s", field)
						}
						continue
					}
					var limit int
					if !present || json.Unmarshal(value, &limit) != nil || limit != tc.limit {
						t.Errorf("upstream %s = %s, want %d", field, value, tc.limit)
					}
				}
				if strings.Contains(string(wire["messages"]), "alice@example.com") || strings.Contains(string(wire["input"]), "alice@example.com") {
					t.Error("unmasked email reached upstream")
				}
				switch tc.endpoint {
				case "messages":
					respond(w, 200, map[string]any{"content": []any{map[string]string{"type": "text", "text": "[PS_test_EMAIL_1]"}}})
				case "embeddings":
					respond(w, 200, map[string]any{"data": []any{map[string]any{"embedding": []float64{0.5}}}})
				default:
					respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "[PS_test_EMAIL_1]"}}}})
				}
			}))
			defer upstream.Close()
			s, repo := fixture(t, testEngine{}, upstream.URL)
			if tc.budget > 0 {
				s.config.DailyTokenBudget = tc.budget
			}
			key := "ps_token-limits-fixture"
			if err := repo.AddKey(context.Background(), "org-a", store.Key{ID: "token-limits", Hash: store.Hash(key), Active: true}); err != nil {
				t.Fatal(err)
			}
			input := `,"messages":[{"role":"user","content":"alice@example.com"}]`
			if tc.endpoint == "embeddings" {
				input = `,"input":"alice@example.com"`
			}
			body := `{"model":"` + tc.model + `"` + input + tc.limits + `}`
			r := httptest.NewRequest("POST", "/v1/"+tc.endpoint, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == 200 {
				if calls.Load() != 1 {
					t.Fatal("expected exactly one upstream dispatch")
				}
				if tc.endpoint != "embeddings" && !strings.Contains(w.Body.String(), "alice@example.com") {
					t.Fatal("email was not restored in response")
				}
			} else if calls.Load() != 0 {
				t.Fatal("rejected request reached upstream")
			}
		})
	}
}
