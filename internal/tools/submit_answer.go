package tools

import (
	"context"
	"encoding/json"

	"github.com/newtoallofthis/probe/internal/connector"
)

// SubmitAnswerTool submits the final answer with file locations.
type SubmitAnswerTool struct{}

func (s *SubmitAnswerTool) Name() string { return "submit_answer" }

func (s *SubmitAnswerTool) Schema() connector.ToolSchema {
	return toolSchema("submit_answer", "Submit the final answer with file locations and a summary. Call this when you have found the relevant code.", map[string]any{
		"results": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "File path relative to project root.",
					},
					"start_line": map[string]any{
						"type":        "integer",
						"description": "Start line of relevant section.",
					},
					"end_line": map[string]any{
						"type":        "integer",
						"description": "End line of relevant section.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Why this location is relevant.",
					},
				},
				"required": []string{"file", "start_line", "end_line", "reason"},
			},
			"description": "Array of file locations with line ranges and reasons.",
		},
		"summary": map[string]any{
			"type":        "string",
			"description": "Brief summary of findings.",
		},
	}, []string{"results", "summary"})
}

func (s *SubmitAnswerTool) Execute(_ context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	return "", &SubmitAnswerResult{RawArgs: args}
}
