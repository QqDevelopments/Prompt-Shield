package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	realengine "github.com/qqdevelopments/prompt-shield/services/gateway/internal/engine"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

func TestPinnedSDKCompatibility(t *testing.T) {
	python, address := os.Getenv("SDK_SMOKE_PYTHON"), os.Getenv("ENGINE_TEST_ADDR")
	if python == "" || address == "" {
		t.Skip("set SDK_SMOKE_PYTHON and ENGINE_TEST_ADDR")
	}
	engine, err := realengine.New(address, os.Getenv("ENGINE_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	var dispatched atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gemini/v1/chat/completions" && (r.Header.Get("Authorization") != "Bearer synthetic-gemini-sdk-key" || r.Header.Get("X-Provider-API-Key") != "") {
			t.Error("Gemini SDK credential isolation failed")
			w.WriteHeader(400)
			return
		}
		var request proxyRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid upstream request")
			w.WriteHeader(400)
			return
		}
		data, _ := json.Marshal(request)
		if strings.Contains(string(data), "alice@example.com") {
			t.Error("original reached upstream")
			w.WriteHeader(400)
			return
		}
		if !strings.Contains(string(data), "[PS_") {
			t.Error("missing sanitized value")
			w.WriteHeader(400)
			return
		}
		dispatched.Add(1)
		if r.URL.Path == "/embeddings" {
			respond(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{.25, .75}}}, "model": "fixture-model", "usage": map[string]int{"prompt_tokens": 8, "total_tokens": 8}})
			return
		}
		text := request.Messages[0].Content
		anthropic := r.URL.Path == "/messages"
		if !request.Stream {
			if anthropic {
				respond(w, 200, map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": []any{map[string]string{"type": "text", "text": text}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 8}})
			} else {
				respond(w, 200, map[string]any{"id": "chat_fixture", "object": "chat.completion", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 8, "total_tokens": 16}})
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(kind string, value any) {
			payload, _ := json.Marshal(value)
			if kind != "" {
				fmt.Fprintf(w, "event: %s\n", kind)
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		if anthropic {
			emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}})
			emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		}
		for _, piece := range []string{text[:len(text)/2], text[len(text)/2:]} {
			if anthropic {
				emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": piece}})
			} else {
				emit("", map[string]any{"id": "chat_fixture", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": piece}, "finish_reason": nil}}})
			}
		}
		if anthropic {
			emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
			emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}})
			emit("message_stop", map[string]string{"type": "message_stop"})
		} else {
			emit("", map[string]any{"id": "chat_fixture", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}})
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}))
	defer upstream.Close()
	server, repo := fixture(t, engine, upstream.URL)
	server.config.GeminiURL = upstream.URL + "/gemini/v1"
	key := "ps_" + store.Token()
	if err := repo.AddKey(context.Background(), "org-a", store.Key{ID: store.Token(), Hash: store.Hash(key), Active: true}); err != nil {
		t.Fatal(err)
	}
	gateway := httptest.NewServer(server.Handler())
	defer gateway.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "../../../../scripts/sdk-smoke.py")
	command.Env = append(os.Environ(), "SDK_BASE_URL="+gateway.URL, "SDK_API_KEY="+key)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("SDK fixture failed: %v\n%s", err, output)
	}
	t.Log(string(output))
	if dispatched.Load() != 7 {
		t.Fatal("missing provider dispatch")
	}
}
