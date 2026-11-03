package store

import (
	"context"
	"time"
)

type Completion struct {
	Outcome      string `json:"outcome"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

func (m *Memory) Complete(ctx context.Context, org, id string, c Completion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.events[org] {
		if m.events[org][i].ID == id {
			m.events[org][i].Completion = c
			return nil
		}
	}
	return ErrNotFound
}
func (p *Postgres) Complete(ctx context.Context, org, id string, c Completion) error {
	_, err := p.Pool.Exec(ctx, "UPDATE audit_events SET outcome=$3,input_tokens=$4,output_tokens=$5 WHERE organization_id=$1 AND id=$2", org, id, c.Outcome, c.InputTokens, c.OutputTokens)
	return err
}
func (p *Postgres) Prune(ctx context.Context, before time.Time) (int64, error) {
	tag, err := p.Pool.Exec(ctx, "DELETE FROM audit_events WHERE created_at<$1", before)
	return tag.RowsAffected(), err
}
