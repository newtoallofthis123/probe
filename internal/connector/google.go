package connector

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

// GoogleConnector implements Connector using the Google Gemini API.
type GoogleConnector struct {
	apiKey string
}

// NewGoogle creates a Google Gemini connector.
func NewGoogle(apiKey string) *GoogleConnector {
	return &GoogleConnector{apiKey: apiKey}
}

func (g *GoogleConnector) Name() string { return "google" }

func (gc *GoogleConnector) StreamChat(ctx context.Context, messages []Message, tools []ToolSchema, opts ChatOpts) (<-chan StreamEvent, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  gc.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("creating Google AI client: %w", err)
	}

	// Separate system instruction from conversation
	var systemInstruction string
	var contents []*genai.Content

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			if systemInstruction != "" {
				systemInstruction += "\n\n"
			}
			systemInstruction += m.Content
		case RoleUser:
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{genai.NewPartFromText(m.Content)},
			})
		case RoleAssistant:
			parts := []*genai.Part{}
			if m.Content != "" {
				parts = append(parts, genai.NewPartFromText(m.Content))
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				// Parse arguments JSON into map
				if tc.Arguments != "" {
					var parsed map[string]any
					if err := jsonUnmarshal([]byte(tc.Arguments), &parsed); err == nil {
						args = parsed
					}
				}
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						Name: tc.Name,
						Args: args,
					},
				})
			}
			contents = append(contents, &genai.Content{
				Role:  "model",
				Parts: parts,
			})
		case RoleTool:
			parts := []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						Name:     m.ToolCallID, // Google uses function name, not call ID
						Response: map[string]any{"result": m.Content},
					},
				},
			}
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: parts,
			})
		}
	}

	// Build tool declarations
	var toolDecls []*genai.Tool
	if len(tools) > 0 {
		funcDecls := make([]*genai.FunctionDeclaration, len(tools))
		for i, t := range tools {
			funcDecls[i] = &genai.FunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  toGeminiSchema(t.Parameters),
			}
		}
		toolDecls = []*genai.Tool{{FunctionDeclarations: funcDecls}}
	}

	config := &genai.GenerateContentConfig{
		Tools: toolDecls,
	}

	if systemInstruction != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemInstruction)},
		}
	}

	if opts.ForceToolChoice != "" {
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode:                 genai.FunctionCallingConfigModeAny,
				AllowedFunctionNames: []string{opts.ForceToolChoice},
			},
		}
	}

	ch := make(chan StreamEvent, 16)

	go func() {
		defer close(ch)

		iter := client.Models.GenerateContentStream(ctx, opts.Model, contents, config)
		var totalInputTokens, totalOutputTokens int64

		for resp, err := range iter {
			if err != nil {
				ch <- StreamEvent{Error: &ErrorEvent{Err: err}}
				return
			}

			if resp.UsageMetadata != nil {
				totalInputTokens = int64(resp.UsageMetadata.PromptTokenCount)
				totalOutputTokens = int64(resp.UsageMetadata.CandidatesTokenCount)
			}

			for _, candidate := range resp.Candidates {
				if candidate.Content == nil {
					continue
				}
				for partIdx, part := range candidate.Content.Parts {
					if part.Text != "" {
						ch <- StreamEvent{TextDelta: &TextDelta{Text: part.Text}}
					}
					if part.FunctionCall != nil {
						ch <- StreamEvent{ToolCallStart: &ToolCallStart{
							Index: partIdx,
							ID:    fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, partIdx),
							Name:  part.FunctionCall.Name,
						}}
						// Google returns args as a map, serialize to JSON
						if argsJSON, err := jsonMarshal(part.FunctionCall.Args); err == nil {
							ch <- StreamEvent{ToolCallDelta: &ToolCallDelta{
								Index:     partIdx,
								Arguments: string(argsJSON),
							}}
						}
					}
				}
			}
		}

		ch <- StreamEvent{Usage: &UsageEvent{
			InputTokens:  totalInputTokens,
			OutputTokens: totalOutputTokens,
			TotalTokens:  totalInputTokens + totalOutputTokens,
		}}
		ch <- StreamEvent{Done: &Done{FinishReason: "stop"}}
	}()

	return ch, nil
}

// toGeminiSchema converts a JSON Schema map to a Gemini Schema.
func toGeminiSchema(params map[string]any) *genai.Schema {
	if params == nil {
		return nil
	}

	schema := &genai.Schema{
		Type: genai.TypeObject,
	}

	if props, ok := params["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				schema.Properties[name] = propertyToSchema(propMap)
			}
		}
	}

	if req, ok := params["required"].([]string); ok {
		schema.Required = req
	} else if req, ok := params["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

func propertyToSchema(prop map[string]any) *genai.Schema {
	s := &genai.Schema{}

	if t, ok := prop["type"].(string); ok {
		switch t {
		case "string":
			s.Type = genai.TypeString
		case "integer":
			s.Type = genai.TypeInteger
		case "number":
			s.Type = genai.TypeNumber
		case "boolean":
			s.Type = genai.TypeBoolean
		case "array":
			s.Type = genai.TypeArray
			if items, ok := prop["items"].(map[string]any); ok {
				s.Items = propertyToSchema(items)
			}
		case "object":
			s.Type = genai.TypeObject
			if props, ok := prop["properties"].(map[string]any); ok {
				s.Properties = make(map[string]*genai.Schema)
				for name, p := range props {
					if pm, ok := p.(map[string]any); ok {
						s.Properties[name] = propertyToSchema(pm)
					}
				}
			}
		}
	}

	if desc, ok := prop["description"].(string); ok {
		s.Description = desc
	}

	if enum, ok := prop["enum"].([]string); ok {
		s.Enum = enum
	} else if enum, ok := prop["enum"].([]any); ok {
		for _, e := range enum {
			if str, ok := e.(string); ok {
				s.Enum = append(s.Enum, str)
			}
		}
	}

	return s
}
