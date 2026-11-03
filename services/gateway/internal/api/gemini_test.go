package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

func geminiRequest(t *testing.T, s *Server, public, stream bool, text, key string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/v1/gemini/chat/completions"
	if public {
		path = "/public" + path
	}
	body, err := json.Marshal(proxyRequest{Model: "gemini-fixture", Messages: []message{{Role: "user", Content: text}}, Stream: stream})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	if !public {
		r.Header.Set("Authorization", "Bearer ps_gemini_application")
	}
	if key != "" {
		r.Header.Set("X-Provider-API-Key", key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func geminiFixture(t *testing.T, e Engine, upstream string) (*Server, *store.Memory) {
	t.Helper()
	s, repo := fixture(t, e, "http://unused-provider.invalid")
	s.config.GeminiURL = upstream + "/v1beta/openai"
	s.config.GeminiKey = "synthetic-gemini-server-key"
	enablePublic(s)
	if err := repo.AddKey(context.Background(), "org-a", store.Key{ID: "gemini", Hash: store.Hash("ps_gemini_application"), Active: true}); err != nil {
		t.Fatal(err)
	}
	return s, repo
}

func TestGeminiJSONAndStreamingUseIsolatedCredentialsAndRestore(t *testing.T) {
	for _, public := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("public=%t/stream=%t", public, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1beta/openai/chat/completions" || r.Header.Get("Authorization") != "Bearer synthetic-gemini-request-key" || r.Header.Get("X-Provider-API-Key") != "" || r.Header.Get("x-api-key") != "" || r.Header.Get("Cookie") != "" {
						t.Error("provider route or credential isolation failed")
					}
					var body proxyRequest
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) != 1 || strings.Contains(body.Messages[0].Content, "alice@example.com") || body.MaxTokens != 4096 {
						t.Error("sanitization or output limit failed")
						w.WriteHeader(500)
						return
					}
					text := body.Messages[0].Content
					if body.Stream {
						w.Header().Set("Content-Type", "text/event-stream")
						for _, part := range []string{text[:12], text[12:]} {
							data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": part}}}})
							fmt.Fprintf(w, "data: %s\n\n", data)
						}
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						return
					}
					respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": text}}}})
				}))
				defer upstream.Close()
				s, repo := geminiFixture(t, testEngine{}, upstream.URL)
				s.config.Hosted = true
				w := geminiRequest(t, s, public, stream, "🌍 alice@example.com twice: alice@example.com", "synthetic-gemini-request-key")
				if w.Code != 200 || strings.Count(w.Body.String(), "alice@example.com") != 2 || strings.Contains(w.Body.String(), "PS_test") {
					t.Fatalf("restoration: %d %s", w.Code, w.Body.String())
				}
				if stream && !strings.Contains(w.Body.String(), "[DONE]") {
					t.Fatal("stream did not complete")
				}
				fresh := geminiRequest(t, s, public, false, "A fresh clean request.", "synthetic-gemini-request-key")
				if fresh.Code != 200 || strings.Contains(fresh.Body.String(), "alice@example.com") {
					t.Fatal("fresh request inherited prior restoration data")
				}
				events, err := repo.Events(context.Background(), "org-a", 100, 0)
				raw, _ := json.Marshal(events)
				if err != nil || strings.Contains(string(raw), "alice@example.com") || strings.Contains(string(raw), "synthetic-gemini-request-key") {
					t.Fatal("raw data entered audit metadata")
				}
			})
		}
	}
}

func TestGeminiFailClosedAndPublicCannotUseServerKey(t *testing.T) {
	for _, tc := range []struct {
		name, text, key string
		fail, hosted    bool
		status          int
	}{
		{"critical secret", "sk-fixture", "synthetic-key", false, false, 400},
		{"inspection unavailable", "hello", "synthetic-key", true, false, 503},
		{"public requires own key", "hello", "", false, false, 401},
		{"hosted requires own key", "hello", "", false, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			s, _ := geminiFixture(t, testEngine{fail: tc.fail}, upstream.URL)
			s.config.Hosted = tc.hosted
			w := geminiRequest(t, s, !tc.hosted, false, tc.text, tc.key)
			if w.Code != tc.status || calls.Load() != 0 {
				t.Fatalf("unsafe dispatch: status=%d calls=%d", w.Code, calls.Load())
			}
		})
	}
}

func TestGeminiProviderErrorsAreSafeForModelsAndChat(t *testing.T) {
	for _, tc := range []struct {
		name, body             string
		upstream, models, chat int
	}{
		{"invalid key", `{"error":{"code":400,"message":"unsafe-private-message","details":[{"reason":"API_KEY_INVALID"}]}}`, 400, 401, 401},
		{"invalid key code", `{"error":{"code":"API_KEY_INVALID"}}`, 403, 401, 401},
		{"unauthorized", `{"error":{"message":"unsafe-private-message"}}`, 401, 401, 401},
		{"forbidden", `{"error":{"message":"unsafe-private-message"}}`, 403, 403, 401},
		{"quota", `{"error":{"message":"unsafe-private-message"}}`, 429, 429, 429},
		{"bad request", `{"error":{"message":"unsafe-private-message"}}`, 400, 502, 502},
		{"malformed", `not-json`, 200, 502, 502},
		{"empty response", `{}`, 200, 502, 502},
		{"empty model list", `{"data":[]}`, 200, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.upstream)
				fmt.Fprint(w, tc.body)
			}))
			defer upstream.Close()
			s, _ := geminiFixture(t, testEngine{}, upstream.URL)
			modelResponse := modelRequest(s, "gemini", "synthetic-gemini-key")
			chatResponse := geminiRequest(t, s, true, false, "hello", "synthetic-gemini-key")
			if modelResponse.Code != tc.models || chatResponse.Code != tc.chat {
				t.Fatalf("statuses: models=%d chat=%d", modelResponse.Code, chatResponse.Code)
			}
			if strings.Contains(modelResponse.Body.String()+chatResponse.Body.String(), "unsafe-private-message") {
				t.Fatal("provider error message leaked")
			}
		})
	}
}
