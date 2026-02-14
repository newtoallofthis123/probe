package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
)

// SelectModeTool lets the LLM choose a search mode.
type SelectModeTool struct{}

func (s *SelectModeTool) Name() string { return "select_mode" }

func (s *SelectModeTool) Schema() openai.ChatCompletionToolParam {
	return toolParam("select_mode", "Choose the search mode for this query. Call this first before using any other tools.", map[string]any{
		"mode": map[string]any{
			"type":        "string",
			"enum":        []string{"locate", "explore", "trace"},
			"description": "locate: find where something is. explore: understand how something works across files. trace: follow a call/data path through the code.",
		},
	}, []string{"mode"})
}

func (s *SelectModeTool) Execute(_ context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	var params struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments: %s", err), nil
	}
	return "", &SelectModeResult{Mode: params.Mode}
}
