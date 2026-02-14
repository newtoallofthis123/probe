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
	OnToolCallBatch(calls []ToolCallInfo)
	OnToolResultBatch(results []ToolResultInfo)
	OnTokenUsage(input, output, total int64)
}

// ToolCallInfo carries a tool call summary for batch rendering.
type ToolCallInfo struct {
	Name string
	Args string
}

// ToolResultInfo carries a tool result summary for batch rendering.
type ToolResultInfo struct {
	Name   string
	Result string
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

	mode := cfg.Mode
	turnsUsed := 0

	// Auto mode: first turn selects the mode
	if mode == "auto" {
		modePrompt := prompt.BuildModeSelectionPrompt(toolCtx)
		modeMessages := []openai.ChatCompletionMessageParamUnion{
			{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String(modePrompt),
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

		params := openai.ChatCompletionNewParams{
			Model:    openai.ChatModel(cfg.Model),
			Messages: modeMessages,
			Tools:    tools.SelectModeDefinition(),
			ToolChoice: openai.ChatCompletionToolChoiceOptionUnionParam{
				OfChatCompletionNamedToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
					Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: "select_mode",
					},
				},
			},
		}

		progress.StartSpinner("Selecting mode...")
		stream := client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}
		for stream.Next() {
			acc.AddChunk(stream.Current())
		}
		progress.StopSpinner()

		if err := stream.Err(); err != nil {
			return &AgentResult{Turns: 0}, fmt.Errorf("LLM API error during mode selection: %w", err)
		}

		turnsUsed = 1
		mode = "locate" // default fallback

		if len(acc.ChatCompletion.Choices) > 0 {
			msg := acc.ChatCompletion.Choices[0].Message
			for _, tc := range msg.ToolCalls {
				if tc.Function.Name == "select_mode" {
					_, err := tools.ExecuteTool(ctx, "select_mode", json.RawMessage(tc.Function.Arguments), toolCtx)
					var sm *tools.SelectModeResult
					if errors.As(err, &sm) {
						mode = sm.Mode
					}
					break
				}
			}
		}
		progress.OnToolResult("select_mode", mode)
	}

	systemPrompt := prompt.BuildSystemPrompt(toolCtx, cfg.Model, cfg.Think, mode)
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

	for turn := turnsUsed; turn < cfg.MaxTurns; turn++ {
		if ctx.Err() != nil {
			return &AgentResult{Turns: turn}, ctx.Err()
		}

		remaining := cfg.MaxTurns - turn - 1

		// Inject turn count as system message
		turnMsg := fmt.Sprintf("[Turn %d — %d remaining]", turn+1, remaining)
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String(turnMsg),
				},
			},
		})

		params := openai.ChatCompletionNewParams{
			Model:             openai.ChatModel(cfg.Model),
			Messages:          messages,
			Tools:             toolDefs,
			ParallelToolCalls: openai.Bool(true),
		}

		// Force submit_answer if token budget nearly exhausted or last turn
		if totalTokens > int64(float64(tokenBudget)*0.8) || remaining <= 0 {
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
			name   string
			result string
		}
		results := make([]toolResult, len(msg.ToolCalls))
		var mu sync.Mutex
		var wg sync.WaitGroup
		var execErr error

		isBatch := len(msg.ToolCalls) > 1

		if isBatch {
			calls := make([]ToolCallInfo, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				calls[i] = ToolCallInfo{Name: tc.Function.Name, Args: tc.Function.Arguments}
			}
			progress.OnToolCallBatch(calls)
		}

		for i, tc := range msg.ToolCalls {
			if !isBatch {
				progress.OnToolCall(tc.Function.Name, tc.Function.Arguments)
			}
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
				results[idx] = toolResult{id: tc.ID, name: tc.Function.Name, result: res}
				if !isBatch {
					progress.OnToolResult(tc.Function.Name, res)
				}
			}(i, tc)
		}
		wg.Wait()

		if isBatch {
			infos := make([]ToolResultInfo, 0, len(results))
			for _, r := range results {
				if r.id != "" {
					infos = append(infos, ToolResultInfo{Name: r.name, Result: r.result})
				}
			}
			progress.OnToolResultBatch(infos)
		}

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

		// Mode-specific turn pressure
		switch mode {
		case "locate":
			if remaining <= 2 {
				messages = append(messages, systemMsg("You're in locate mode with %d turns left. Submit your best match now.", remaining))
			}
		case "explore":
			budget := cfg.MaxTurns - turnsUsed
			used := turn - turnsUsed
			if budget > 0 && float64(used) > float64(budget)*0.6 {
				messages = append(messages, systemMsg("You've used over 60%% of your budget. Start submitting partial results."))
			}
		case "trace":
			budget := cfg.MaxTurns - turnsUsed
			used := turn - turnsUsed
			if budget > 0 && float64(used) > float64(budget)*0.7 {
				messages = append(messages, systemMsg("You've used over 70%% of your budget. Submit what you have, note if trace is partial."))
			}
		}

		// Nudge after 3 consecutive empty results
		if consecutiveEmpty >= 3 {
			messages = append(messages, systemMsg("Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."))
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

// systemMsg creates a system message for injection into the conversation.
func systemMsg(format string, args ...any) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfSystem: &openai.ChatCompletionSystemMessageParam{
			Content: openai.ChatCompletionSystemMessageParamContentUnion{
				OfString: openai.String(fmt.Sprintf(format, args...)),
			},
		},
	}
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
