package api

import (
	"encoding/json"
	"fmt"
	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamingRestoresEveryPlaceholderSplit(t *testing.T) {
	token := "[PS_random_EMAIL_1]"
	mappings := []*pb.Mapping{{Placeholder: token, Original: "alice@example.com"}}
	for split := 0; split <= len(token); split++ {
		t.Run(streamIndex(split), func(t *testing.T) {
			var upstream strings.Builder
			for _, piece := range []string{"Hello 🌍 " + token[:split], token[split:] + "!"} {
				data := raw(map[string]any{"id": "fixture", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": piece}, "finish_reason": nil}}})
				fmt.Fprintf(&upstream, "data: %s\n\n", data)
			}
			upstream.WriteString("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			w := httptest.NewRecorder()
			streamResponse(w, httptest.NewRequest("POST", "/v1/chat/completions", nil), strings.NewReader(upstream.String()), mappings, false)
			var text strings.Builder
			for _, line := range strings.Split(w.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: {") {
					continue
				}
				var value struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &value); err != nil {
					t.Fatal(err)
				}
				for _, choice := range value.Choices {
					text.WriteString(choice.Delta.Content)
				}
			}
			if text.String() != "Hello 🌍 alice@example.com!" {
				t.Fatalf("wrong restoration at split %d", split)
			}
			if !strings.Contains(w.Body.String(), "[DONE]") {
				t.Fatal("missing termination")
			}
		})
	}
}
func TestAnthropicStreamingAndUnsafeErrorRedaction(t *testing.T) {
	input := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"[PS_x_\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"EMAIL_1]\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	w := httptest.NewRecorder()
	streamResponse(w, httptest.NewRequest("POST", "/v1/messages", nil), strings.NewReader(input), []*pb.Mapping{{Placeholder: "[PS_x_EMAIL_1]", Original: "alice@example.com"}}, true)
	if !strings.Contains(w.Body.String(), "alice@example.com") || !strings.Contains(w.Body.String(), "event: message_stop") {
		t.Fatal("Anthropic restoration failed")
	}
	w = httptest.NewRecorder()
	streamResponse(w, httptest.NewRequest("POST", "/v1/messages", nil), strings.NewReader("data: {\"error\":{\"message\":\"sensitive-upstream-error\"}}\n\n"), nil, true)
	if strings.Contains(w.Body.String(), "sensitive-upstream-error") || !strings.Contains(w.Body.String(), "upstream_stream_failed") {
		t.Fatal("unsafe upstream error")
	}
}
