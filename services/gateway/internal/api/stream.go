package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	pb "github.com/qqdevelopments/prompt-shield/services/gateway/gen/shieldv1"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
)

// Each response choice/content block owns a bounded partial-placeholder suffix.
type streamRestorer struct {
	mappings []*pb.Mapping
	pending  string
	replacer *strings.Replacer
}

func newRestorer(mappings []*pb.Mapping) *streamRestorer {
	pairs := make([]string, 0, len(mappings)*2)
	for _, m := range mappings {
		pairs = append(pairs, m.Placeholder, m.Original)
	}
	return &streamRestorer{mappings: mappings, replacer: strings.NewReplacer(pairs...)}
}
func (s *streamRestorer) push(text string, final bool) string {
	value := s.pending + text
	s.pending = ""
	cut := len(value)
	if !final {
		for _, m := range s.mappings {
			max := len(m.Placeholder) - 1
			if max > len(value) {
				max = len(value)
			}
			for n := max; n > 0; n-- {
				if strings.HasSuffix(value, m.Placeholder[:n]) {
					if len(value)-n < cut {
						cut = len(value) - n
					}
					break
				}
			}
		}
	}
	s.pending = value[cut:]
	return s.replacer.Replace(value[:cut])
}
func raw(value any) json.RawMessage { data, _ := json.Marshal(value); return data }

type StreamResult struct {
	Complete bool
	Usage    store.Completion
}

func streamResponse(w http.ResponseWriter, r *http.Request, body io.Reader, mappings []*pb.Mapping, anthropic bool) (result StreamResult) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	states := map[int]*streamRestorer{}
	finished := map[int]bool{}
	defer func() {
		for _, state := range states {
			state.pending = ""
			state.mappings = nil
			state.replacer = nil
		}
	}()
	send := func(event string, data []byte) error {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		if event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		return controller.Flush()
	}
	failStream := func() {
		event := ""
		if anthropic {
			event = "error"
		}
		_ = send(event, raw(map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "code": "upstream_stream_failed", "message": "upstream_stream_failed"}}))
	}
	get := func(index int) (*streamRestorer, error) {
		if index < 0 || index > 127 || finished[index] {
			return nil, errors.New("invalid_stream_index")
		}
		if states[index] == nil {
			states[index] = newRestorer(mappings)
		}
		return states[index], nil
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	event := ""
	var data strings.Builder
	total := 0
	done := false
	process := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "[DONE]" && !anthropic {
			for index, state := range states {
				if state.pending != "" || !finished[index] {
					return errors.New("unfinished_placeholder")
				}
			}
			done = true
			return send("", []byte("[DONE]"))
		}
		var value map[string]json.RawMessage
		if json.Unmarshal([]byte(payload), &value) != nil {
			return errors.New("invalid_stream_json")
		}
		if _, ok := value["error"]; ok {
			return errors.New("provider_error")
		}
		if usage, ok := value["usage"]; ok {
			parsed := readUsage(usage, anthropic)
			if parsed.InputTokens > 0 {
				result.Usage.InputTokens = parsed.InputTokens
			}
			if parsed.OutputTokens > 0 {
				result.Usage.OutputTokens = parsed.OutputTokens
			}
		}
		if anthropic {
			var kind string
			_ = json.Unmarshal(value["type"], &kind)
			var index int
			_ = json.Unmarshal(value["index"], &index)
			switch kind {
			case "content_block_start":
				var block map[string]json.RawMessage
				if json.Unmarshal(value["content_block"], &block) != nil {
					return errors.New("invalid_block")
				}
				var blockType string
				_ = json.Unmarshal(block["type"], &blockType)
				if blockType != "text" {
					return errors.New("unsupported_block")
				}
				state, err := get(index)
				if err != nil {
					return err
				}
				var text string
				_ = json.Unmarshal(block["text"], &text)
				block["text"] = raw(state.push(text, false))
				value["content_block"] = raw(block)
			case "content_block_delta":
				var delta map[string]json.RawMessage
				if json.Unmarshal(value["delta"], &delta) != nil {
					return errors.New("invalid_delta")
				}
				var deltaType, text string
				_ = json.Unmarshal(delta["type"], &deltaType)
				if deltaType != "text_delta" || json.Unmarshal(delta["text"], &text) != nil {
					return errors.New("unsupported_delta")
				}
				state, err := get(index)
				if err != nil {
					return err
				}
				delta["text"] = raw(state.push(text, false))
				value["delta"] = raw(delta)
			case "content_block_stop":
				state, err := get(index)
				if err != nil {
					return err
				}
				tail := state.push("", true)
				if tail != "" {
					if err = send("content_block_delta", raw(map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]string{"type": "text_delta", "text": tail}})); err != nil {
						return err
					}
				}
				finished[index] = true
			case "message_stop":
				for index := range states {
					if !finished[index] {
						return errors.New("unfinished_block")
					}
				}
				done = true
			case "message_start", "message_delta", "ping":
				if kind == "message_start" {
					var message map[string]json.RawMessage
					_ = json.Unmarshal(value["message"], &message)
					parsed := readUsage(message["usage"], true)
					result.Usage.InputTokens = parsed.InputTokens
				}
			default:
				return errors.New("unsupported_event")
			}
			return send(kind, raw(value))
		}
		var choices []map[string]json.RawMessage
		if json.Unmarshal(value["choices"], &choices) != nil {
			return errors.New("invalid_choices")
		}
		for _, choice := range choices {
			var index int
			if json.Unmarshal(choice["index"], &index) != nil {
				return errors.New("invalid_index")
			}
			state, err := get(index)
			if err != nil {
				return err
			}
			var delta map[string]json.RawMessage
			if json.Unmarshal(choice["delta"], &delta) != nil || delta == nil {
				return errors.New("invalid_delta")
			}
			if _, ok := delta["tool_calls"]; ok {
				return errors.New("unsupported_tools")
			}
			if _, ok := delta["function_call"]; ok {
				return errors.New("unsupported_tools")
			}
			final := len(choice["finish_reason"]) > 0 && string(choice["finish_reason"]) != "null"
			var text string
			if content, ok := delta["content"]; ok && string(content) != "null" {
				if json.Unmarshal(content, &text) != nil {
					return errors.New("invalid_content")
				}
			}
			restored := state.push(text, final)
			if restored != "" || text != "" {
				delta["content"] = raw(restored)
			}
			choice["delta"] = raw(delta)
			if final {
				finished[index] = true
			}
		}
		value["choices"] = raw(choices)
		return send("", raw(value))
	}
	for scanner.Scan() {
		if r.Context().Err() != nil {
			return
		}
		line := scanner.Text()
		total += len(line)
		if total > 8*1024*1024 {
			failStream()
			return
		}
		if line == "" {
			if err := process(); err != nil {
				failStream()
				return
			}
			event = ""
			if done {
				result.Complete = true
				return
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if len(event) > 100 {
				failStream()
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			if data.Len() > 1024*1024 {
				failStream()
				return
			}
		}
	}
	if !done {
		failStream()
	}
	return
}

// Keep a stable, non-sensitive stream identifier for diagnostics in tests.
func streamIndex(index int) string { return strconv.Itoa(index) }
