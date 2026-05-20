package core

import (
	"context"
	"encoding/json"

	"github.com/haichen-zhang/customize-agents/llm"
)

type Tool struct {
	Definition llm.ToolDef
	Execute    func(ctx context.Context, input json.RawMessage) (string, error)
}
