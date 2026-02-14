package connector

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicConnector implements Connector using the Anthropic API.
type AnthropicConnector struct {
	client anthropic.Client
}

// NewAnthropic creates an Anthropic connector.
func NewAnthropic(apiKey string) *AnthropicConnector {
	client := anthropic.NewClient(
		anthropicoption.WithAPIKey(apiKey),
	)
	return &AnthropicConnector{client: client}
}

func (a *AnthropicConnector) Name() string { return "anthropic" }

func (a *AnthropicConnector) StreamChat(ctx context.Context, messages []Message, tools []ToolSchema, opts ChatOpts) (<-chan StreamEvent, error) {
	// Separate system message from conversation messages
	var systemBlocks []anthropic.TextBlockParam
	var convMessages []anthropic.MessageParam

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: m.Content})
		case RoleUser:
			convMessages = append(convMessages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input json.RawMessage
				if tc.Arguments != "" {
					input = json.RawMessage(tc.Arguments)
				} else {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tc.ID,
						Name:  tc.Name,
						Input: input,
					},
				})
			}
			convMessages = append(convMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: blocks,
			})
		case RoleTool:
			convMessages = append(convMessages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(opts.Model),
		MaxTokens: int64(4096),
		Messages:  convMessages,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	if len(tools) > 0 {
		params.Tools = toAnthropicTools(tools)
	}

	if opts.ForceToolChoice != "" {
		params.ToolChoice = anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: opts.ForceToolChoice,
			},
		}
	}

	ch := make(chan StreamEvent, 16)

	go func() {
		defer close(ch)

		stream := a.client.Messages.NewStreaming(ctx, params)

		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case "content_block_start":
				if event.ContentBlock.Type == "tool_use" {
					ch <- StreamEvent{ToolCallStart: &ToolCallStart{
						Index: int(event.Index),
						ID:    event.ContentBlock.ID,
						Name:  event.ContentBlock.Name,
					}}
				}
			case "content_block_delta":
				if event.Delta.Type == "text_delta" {
					ch <- StreamEvent{TextDelta: &TextDelta{Text: event.Delta.Text}}
				} else if event.Delta.Type == "input_json_delta" {
					ch <- StreamEvent{ToolCallDelta: &ToolCallDelta{
						Index:     int(event.Index),
						Arguments: event.Delta.PartialJSON,
					}}
				}
			case "message_delta":
				ch <- StreamEvent{Usage: &UsageEvent{
					OutputTokens: int64(event.Usage.OutputTokens),
				}}
			case "message_start":
				if event.Message.Usage.InputTokens > 0 {
					ch <- StreamEvent{Usage: &UsageEvent{
						InputTokens: int64(event.Message.Usage.InputTokens),
						TotalTokens: int64(event.Message.Usage.InputTokens),
					}}
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamEvent{Error: &ErrorEvent{Err: err}}
			return
		}

		ch <- StreamEvent{Done: &Done{FinishReason: "stop"}}
	}()

	return ch, nil
}

func toAnthropicTools(schemas []ToolSchema) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, len(schemas))
	for i, s := range schemas {
		inputSchema := anthropic.ToolInputSchemaParam{}
		if props, ok := s.Parameters["properties"]; ok {
			inputSchema.Properties = props
		}
		if req, ok := s.Parameters["required"].([]string); ok {
			inputSchema.Required = req
		} else if req, ok := s.Parameters["required"].([]any); ok {
			strs := make([]string, 0, len(req))
			for _, r := range req {
				if str, ok := r.(string); ok {
					strs = append(strs, str)
				}
			}
			inputSchema.Required = strs
		}
		out[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        s.Name,
				Description: anthropic.String(s.Description),
				InputSchema: inputSchema,
			},
		}
	}
	return out
}
