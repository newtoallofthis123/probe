package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/newtoallofthis/probe/internal/sandbox"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// SubmitAnswerResult is a sentinel error returned when the LLM calls submit_answer.
// The agent loop checks for this with errors.As to extract the raw arguments.
type SubmitAnswerResult struct {
	RawArgs json.RawMessage
}

func (e *SubmitAnswerResult) Error() string {
	return "submit_answer"
}

// SelectModeResult is a sentinel error returned when the LLM calls select_mode.
type SelectModeResult struct {
	Mode string
}

func (e *SelectModeResult) Error() string {
	return "select_mode"
}

// ToolContext carries shared state for tool executors.
type ToolContext struct {
	ProjectDir        string
	GitIgnore         *sandbox.GitIgnore
	MaxResultsPerGrep int
	MaxFileReadLines  int
	AllowList         []string // when non-nil, restricts tools to these files only
}

// Tool is the interface every tool implements.
type Tool interface {
	Name() string
	Schema() openai.ChatCompletionToolParam
	Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error)
}

var registry = map[string]Tool{}

// Register adds a tool to the registry.
func Register(t Tool) { registry[t.Name()] = t }

func init() {
	Register(&GrepTool{})
	Register(&FindFilesTool{})
	Register(&ReadFileTool{})
	Register(&TreeTool{})
	Register(&SubmitAnswerTool{})
	Register(&SelectModeTool{})
}

// ExecuteTool dispatches a tool call to the appropriate executor.
// Tool-level failures are returned as string results (first return value).
// Go errors (second return value) mean the harness itself is broken.
func ExecuteTool(ctx context.Context, name string, args json.RawMessage, tc ToolContext) (string, error) {
	t, ok := registry[name]
	if !ok {
		return fmt.Sprintf("Error: unknown tool '%s'", name), nil
	}
	return t.Execute(ctx, tc, args)
}

// SelectModeDefinition returns the select_mode tool for auto mode's first turn.
func SelectModeDefinition() []openai.ChatCompletionToolParam {
	return []openai.ChatCompletionToolParam{registry["select_mode"].Schema()}
}

// ToolDefinitions returns the tool schemas the LLM can call (excludes select_mode).
func ToolDefinitions() []openai.ChatCompletionToolParam {
	order := []string{"grep", "find_files", "read_file", "tree", "submit_answer"}
	defs := make([]openai.ChatCompletionToolParam, 0, len(order))
	for _, name := range order {
		if t, ok := registry[name]; ok {
			defs = append(defs, t.Schema())
		}
	}
	return defs
}

// helper to build tool params (reduces boilerplate in tool files)
func toolParam(name, description string, properties map[string]any, required []string) openai.ChatCompletionToolParam {
	params := shared.FunctionParameters{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return openai.ChatCompletionToolParam{
		Function: shared.FunctionDefinitionParam{
			Name:        name,
			Description: openai.String(description),
			Parameters:  params,
		},
	}
}
