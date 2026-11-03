package api

import (
	"context"
	"encoding/json"
	"github.com/qqdevelopments/prompt-shield/services/gateway/internal/store"
	"time"
)

func readUsage(value json.RawMessage, anthropic bool) store.Completion {
	var usage map[string]int64
	_ = json.Unmarshal(value, &usage)
	input, output := usage["prompt_tokens"], usage["completion_tokens"]
	if anthropic {
		input, output = usage["input_tokens"], usage["output_tokens"]
	}
	if input < 0 || input > 10000000 {
		input = 0
	}
	if output < 0 || output > 10000000 {
		output = 0
	}
	return store.Completion{InputTokens: input, OutputTokens: output}
}
func (s *Server) complete(org, id string, completion store.Completion) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.repo.Complete(ctx, org, id, completion) != nil {
		s.metrics.auditFailures.Add(1)
	}
}
