package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/newtoallofthis/probe/internal/config"
	"github.com/newtoallofthis/probe/internal/prompt"
	"github.com/newtoallofthis/probe/internal/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// ProgressReporter is the interface agent uses for progress reporting.
// Implemented by output.Progress.
type ProgressReporter interface {
	StartSpinner(msg string)
	StopSpinner()
	OnToolCall(name string, args string)
	OnToolResult(name string, result string)
	OnTokenUsage(input, output, total int64)
}

// SearchResult represents a single code location found by the agent.
type SearchResult struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Reason    string `json:"reason"`
}

// AgentResult is the final output of an agent run.
type AgentResult struct {
	Results []SearchResult `json:"results"`
	Summary string         `json:"summary"`
	Turns   int            `json:"turns"`
}

// RunAgent executes the agent loop: sends the query to the LLM, handles tool
// calls iteratively, and returns results when submit_answer is called or the
// turn limit is reached.
func RunAgent(ctx context.Context, query string, cfg *config.Config, toolCtx tools.ToolContext, progress ProgressReporter) (*AgentResult, error) {
	apiKey := cfg.APIKey
	if apiKey == "" && isLocalhost(cfg.BaseURL) {
		apiKey = "ollama"
	}

	client := openai.NewClient(
		option.WithBaseURL(cfg.BaseURL),
		option.WithAPIKey(apiKey),
	)

	systemPrompt := prompt.BuildSystemPrompt(toolCtx, cfg.Model, cfg.Think)
	toolDefs := tools.ToolDefinitions()

	messages := []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String(systemPrompt),
				},
			},
		},
		{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String(query),
				},
			},
		},
	}

	var totalTokens int64
	const tokenBudget = 14000
	consecutiveEmpty := 0

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		if ctx.Err() != nil {
			return &AgentResult{Turns: turn}, ctx.Err()
		}

		params := openai.ChatCompletionNewParams{
			Model:    openai.ChatModel(cfg.Model),
			Messages: messages,
			Tools:    toolDefs,
		}

		// Force submit_answer if token budget nearly exhausted
		if totalTokens > int64(float64(tokenBudget)*0.8) {
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
					Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: "submit_answer",
					},
				},
			}
		}

		// Stream the response
		progress.StartSpinner("Searching...")
		stream := client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}

		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)
		}
		progress.StopSpinner()
		if err := stream.Err(); err != nil {
			return &AgentResult{Turns: turn}, fmt.Errorf("LLM API error: %w", err)
		}

		if len(acc.ChatCompletion.Choices) == 0 {
			return &AgentResult{Turns: turn + 1}, nil
		}

		msg := acc.ChatCompletion.Choices[0].Message
		totalTokens += acc.ChatCompletion.Usage.TotalTokens

		// No tool calls — LLM responded with text only, inject nudge to submit
		if len(msg.ToolCalls) == 0 {
			// Append the text response and nudge to call submit_answer
			if msg.Content != "" {
				messages = append(messages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &openai.ChatCompletionAssistantMessageParam{
						Content: openai.ChatCompletionAssistantMessageParamContentUnion{
							OfString: openai.String(msg.Content),
						},
					},
				})
			}
			messages = append(messages, openai.ChatCompletionMessageParamUnion{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String("You must call submit_answer to return your results. Do not respond with text. Call the submit_answer tool now."),
					},
				},
			})
			continue
		}

		// Check for submit_answer first
		for _, tc := range msg.ToolCalls {
			if tc.Function.Name == "submit_answer" {
				return parseSubmitAnswer(tc.Function.Arguments, turn+1)
			}
		}

		// Build assistant message with tool calls for history
		assistantToolCalls := make([]openai.ChatCompletionMessageToolCallParam, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			assistantToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
				ID:   tc.ID,
				Type: "function",
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				ToolCalls: assistantToolCalls,
			},
		})

		// Execute tool calls in parallel
		type toolResult struct {
			id     string
			result string
		}
		results := make([]toolResult, len(msg.ToolCalls))
		var mu sync.Mutex
		var wg sync.WaitGroup
		var execErr error

		for i, tc := range msg.ToolCalls {
			progress.OnToolCall(tc.Function.Name, tc.Function.Arguments)
			wg.Add(1)
			go func(idx int, tc openai.ChatCompletionMessageToolCall) {
				defer wg.Done()
				res, err := tools.ExecuteTool(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), toolCtx)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					var sa *tools.SubmitAnswerResult
					if errors.As(err, &sa) {
						return
					}
					execErr = err
					return
				}
				results[idx] = toolResult{id: tc.ID, result: res}
				progress.OnToolResult(tc.Function.Name, res)
			}(i, tc)
		}
		wg.Wait()

		if execErr != nil {
			return &AgentResult{Turns: turn + 1}, fmt.Errorf("tool execution failed: %w", execErr)
		}

		// Track consecutive empty results
		allEmpty := true
		for _, r := range results {
			if r.result != "" && !strings.HasPrefix(r.result, "No matches") && !strings.HasPrefix(r.result, "No files") {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			consecutiveEmpty++
		} else {
			consecutiveEmpty = 0
		}

		// Append tool result messages
		for _, r := range results {
			if r.id == "" {
				continue
			}
			messages = append(messages, openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					ToolCallID: r.id,
					Content: openai.ChatCompletionToolMessageParamContentUnion{
						OfString: openai.String(r.result),
					},
				},
			})
		}

		// Early-submit nudge in non-think mode
		if !cfg.Think && turn == 3 {
			messages = append(messages, openai.ChatCompletionMessageParamUnion{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String("You've had two turns to search but haven't submitted an answer yet, just a reminder."),
					},
				},
			})
		}

		// Nudge after 3 consecutive empty results
		if consecutiveEmpty >= 3 {
			messages = append(messages, openai.ChatCompletionMessageParamUnion{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String("Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."),
					},
				},
			})
			consecutiveEmpty = 0
		}

		progress.OnTokenUsage(
			acc.ChatCompletion.Usage.PromptTokens,
			acc.ChatCompletion.Usage.CompletionTokens,
			totalTokens,
		)
	}

	// Exhausted max turns without submit_answer
	return &AgentResult{
		Turns:   cfg.MaxTurns,
		Summary: "Agent exhausted maximum turns without submitting an answer",
	}, nil
}

// parseSubmitAnswer extracts an AgentResult from submit_answer arguments.
func parseSubmitAnswer(argsJSON string, turns int) (*AgentResult, error) {
	var args struct {
		Results []SearchResult `json:"results"`
		Summary string         `json:"summary"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return &AgentResult{Turns: turns}, fmt.Errorf("parsing submit_answer: %w", err)
	}
	return &AgentResult{
		Results: args.Results,
		Summary: args.Summary,
		Turns:   turns,
	}, nil
}

// isLocalhost checks if a URL points to localhost.
func isLocalhost(url string) bool {
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1")
}
