package connector

import (
	"context"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// OpenAIConnector implements Connector using the OpenAI API (also compatible with Ollama).
type OpenAIConnector struct {
	client *openai.Client
}

// NewOpenAI creates an OpenAI connector.
func NewOpenAI(baseURL, apiKey string) *OpenAIConnector {
	if apiKey == "" && isLocalhost(baseURL) {
		apiKey = "ollama"
	}
	c := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &OpenAIConnector{client: &c}
}

func (o *OpenAIConnector) Name() string { return "openai" }

func (o *OpenAIConnector) StreamChat(ctx context.Context, messages []Message, tools []ToolSchema, opts ChatOpts) (<-chan StreamEvent, error) {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(opts.Model),
		Messages: toOpenAIMessages(messages),
		Tools:    toOpenAITools(tools),
	}

	if opts.ParallelToolCalls {
		params.ParallelToolCalls = openai.Bool(true)
	}

	if opts.ForceToolChoice != "" {
		params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
					Name: opts.ForceToolChoice,
				},
			},
		}
	}

	ch := make(chan StreamEvent, 16)
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)

	go func() {
		defer close(ch)
		acc := openai.ChatCompletionAccumulator{}

		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			// Emit text deltas
			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					ch <- StreamEvent{TextDelta: &TextDelta{Text: delta.Content}}
				}
				for _, tc := range delta.ToolCalls {
					if tc.Function.Name != "" {
						ch <- StreamEvent{ToolCallStart: &ToolCallStart{
							Index: int(tc.Index),
							ID:    tc.ID,
							Name:  tc.Function.Name,
						}}
					}
					if tc.Function.Arguments != "" {
						ch <- StreamEvent{ToolCallDelta: &ToolCallDelta{
							Index:     int(tc.Index),
							Arguments: tc.Function.Arguments,
						}}
					}
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamEvent{Error: &ErrorEvent{Err: err}}
			return
		}

		// Emit usage
		if acc.ChatCompletion.Usage.TotalTokens > 0 {
			ch <- StreamEvent{Usage: &UsageEvent{
				InputTokens:  acc.ChatCompletion.Usage.PromptTokens,
				OutputTokens: acc.ChatCompletion.Usage.CompletionTokens,
				TotalTokens:  acc.ChatCompletion.Usage.TotalTokens,
			}}
		}

		finishReason := ""
		if len(acc.ChatCompletion.Choices) > 0 {
			finishReason = string(acc.ChatCompletion.Choices[0].FinishReason)
		}
		ch <- StreamEvent{Done: &Done{FinishReason: finishReason}}
	}()

	return ch, nil
}

// toOpenAIMessages converts provider-agnostic messages to OpenAI format.
func toOpenAIMessages(msgs []Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case RoleSystem:
			out[i] = openai.ChatCompletionMessageParamUnion{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String(m.Content),
					},
				},
			}
		case RoleUser:
			out[i] = openai.ChatCompletionMessageParamUnion{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String(m.Content),
					},
				},
			}
		case RoleAssistant:
			param := &openai.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				param.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(m.Content),
				}
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]openai.ChatCompletionMessageToolCallParam, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					tcs[j] = openai.ChatCompletionMessageToolCallParam{
						ID:   tc.ID,
						Type: "function",
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				param.ToolCalls = tcs
			}
			out[i] = openai.ChatCompletionMessageParamUnion{OfAssistant: param}
		case RoleTool:
			out[i] = openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					ToolCallID: m.ToolCallID,
					Content: openai.ChatCompletionToolMessageParamContentUnion{
						OfString: openai.String(m.Content),
					},
				},
			}
		}
	}
	return out
}

// toOpenAITools converts provider-agnostic tool schemas to OpenAI format.
func toOpenAITools(schemas []ToolSchema) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, len(schemas))
	for i, s := range schemas {
		out[i] = openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        s.Name,
				Description: openai.String(s.Description),
				Parameters:  shared.FunctionParameters(s.Parameters),
			},
		}
	}
	return out
}

func isLocalhost(url string) bool {
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1")
}
