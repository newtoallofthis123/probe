package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/newtoallofthis/probe/internal/config"
	"github.com/newtoallofthis/probe/internal/connector"
	"github.com/newtoallofthis/probe/internal/prompt"
	"github.com/newtoallofthis/probe/internal/tools"
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
func RunAgent(ctx context.Context, query string, cfg *config.Config, toolCtx tools.ToolContext, conn connector.Connector, progress ProgressReporter) (*AgentResult, error) {
	mode := cfg.Mode
	turnsUsed := 0

	// Auto mode: first turn selects the mode
	if mode == "auto" {
		modePrompt := prompt.BuildModeSelectionPrompt(toolCtx)
		modeMessages := []connector.Message{
			connector.SystemMessage(modePrompt),
			connector.UserMessage(query),
		}

		opts := connector.ChatOpts{
			Model:           cfg.Model,
			ForceToolChoice: "select_mode",
		}

		progress.StartSpinner("Selecting mode...")
		ch, err := conn.StreamChat(ctx, modeMessages, tools.SelectModeDefinition(), opts)
		if err != nil {
			progress.StopSpinner()
			return &AgentResult{Turns: 0}, fmt.Errorf("LLM API error during mode selection: %w", err)
		}

		// Collect the response
		var toolCalls []connector.ToolCall
		if err := collectStream(ch, &toolCalls); err != nil {
			progress.StopSpinner()
			return &AgentResult{Turns: 0}, fmt.Errorf("LLM API error during mode selection: %w", err)
		}
		progress.StopSpinner()

		turnsUsed = 1
		mode = "locate" // default fallback

		for _, tc := range toolCalls {
			if tc.Name == "select_mode" {
				_, err := tools.ExecuteTool(ctx, "select_mode", json.RawMessage(tc.Arguments), toolCtx)
				var sm *tools.SelectModeResult
				if errors.As(err, &sm) {
					mode = sm.Mode
				}
				break
			}
		}
		progress.OnToolResult("select_mode", mode)
	}

	systemPrompt := prompt.BuildSystemPrompt(toolCtx, cfg.Model, cfg.Think, mode)
	toolDefs := tools.ToolDefinitions()

	messages := []connector.Message{
		connector.SystemMessage(systemPrompt),
		connector.UserMessage(query),
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
		messages = append(messages, connector.SystemMessage(turnMsg))

		opts := connector.ChatOpts{
			Model:             cfg.Model,
			ParallelToolCalls: true,
		}

		// Force submit_answer if token budget nearly exhausted or last turn
		if totalTokens > int64(float64(tokenBudget)*0.8) || remaining <= 0 {
			opts.ForceToolChoice = "submit_answer"
		}

		// Stream the response
		progress.StartSpinner("Searching...")
		ch, err := conn.StreamChat(ctx, messages, toolDefs, opts)
		if err != nil {
			progress.StopSpinner()
			return &AgentResult{Turns: turn}, fmt.Errorf("LLM API error: %w", err)
		}

		var assistantContent string
		var toolCalls []connector.ToolCall
		var usage *connector.UsageEvent

		for ev := range ch {
			switch {
			case ev.TextDelta != nil:
				assistantContent += ev.TextDelta.Text
			case ev.ToolCallStart != nil:
				// Grow slice if needed
				for len(toolCalls) <= ev.ToolCallStart.Index {
					toolCalls = append(toolCalls, connector.ToolCall{})
				}
				toolCalls[ev.ToolCallStart.Index].ID = ev.ToolCallStart.ID
				toolCalls[ev.ToolCallStart.Index].Name = ev.ToolCallStart.Name
			case ev.ToolCallDelta != nil:
				for len(toolCalls) <= ev.ToolCallDelta.Index {
					toolCalls = append(toolCalls, connector.ToolCall{})
				}
				toolCalls[ev.ToolCallDelta.Index].Arguments += ev.ToolCallDelta.Arguments
			case ev.Usage != nil:
				usage = ev.Usage
			case ev.Error != nil:
				progress.StopSpinner()
				return &AgentResult{Turns: turn}, fmt.Errorf("LLM API error: %w", ev.Error.Err)
			case ev.Done != nil:
				// response complete
			}
		}
		progress.StopSpinner()

		if usage != nil {
			totalTokens += usage.TotalTokens
		}

		// No tool calls — LLM responded with text only, inject nudge to submit
		if len(toolCalls) == 0 {
			if assistantContent != "" {
				messages = append(messages, connector.AssistantMessage(assistantContent))
			}
			messages = append(messages, connector.UserMessage("You must call submit_answer to return your results. Do not respond with text. Call the submit_answer tool now."))
			continue
		}

		// Check for submit_answer first
		for _, tc := range toolCalls {
			if tc.Name == "submit_answer" {
				return parseSubmitAnswer(tc.Arguments, turn+1)
			}
		}

		// Build assistant message with tool calls for history
		messages = append(messages, connector.AssistantMessage(assistantContent, toolCalls...))

		// Execute tool calls in parallel
		type toolResult struct {
			id     string
			name   string
			result string
		}
		results := make([]toolResult, len(toolCalls))
		var mu sync.Mutex
		var wg sync.WaitGroup
		var execErr error

		isBatch := len(toolCalls) > 1

		if isBatch {
			calls := make([]ToolCallInfo, len(toolCalls))
			for i, tc := range toolCalls {
				calls[i] = ToolCallInfo{Name: tc.Name, Args: tc.Arguments}
			}
			progress.OnToolCallBatch(calls)
		}

		for i, tc := range toolCalls {
			if !isBatch {
				progress.OnToolCall(tc.Name, tc.Arguments)
			}
			wg.Add(1)
			go func(idx int, tc connector.ToolCall) {
				defer wg.Done()
				res, err := tools.ExecuteTool(ctx, tc.Name, json.RawMessage(tc.Arguments), toolCtx)
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
				results[idx] = toolResult{id: tc.ID, name: tc.Name, result: res}
				if !isBatch {
					progress.OnToolResult(tc.Name, res)
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
			messages = append(messages, connector.ToolMessage(r.id, r.result))
		}

		// Mode-specific turn pressure
		switch mode {
		case "locate":
			if remaining <= 2 {
				messages = append(messages, connector.SystemMessage(fmt.Sprintf("You're in locate mode with %d turns left. Submit your best match now.", remaining)))
			}
		case "explore":
			budget := cfg.MaxTurns - turnsUsed
			used := turn - turnsUsed
			if budget > 0 && float64(used) > float64(budget)*0.6 {
				messages = append(messages, connector.SystemMessage("You've used over 60% of your budget. Start submitting partial results."))
			}
		case "trace":
			budget := cfg.MaxTurns - turnsUsed
			used := turn - turnsUsed
			if budget > 0 && float64(used) > float64(budget)*0.7 {
				messages = append(messages, connector.SystemMessage("You've used over 70% of your budget. Submit what you have, note if trace is partial."))
			}
		}

		// Nudge after 3 consecutive empty results
		if consecutiveEmpty >= 3 {
			messages = append(messages, connector.SystemMessage("Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."))
			consecutiveEmpty = 0
		}

		if usage != nil {
			progress.OnTokenUsage(usage.InputTokens, usage.OutputTokens, totalTokens)
		}
	}

	// Exhausted max turns without submit_answer
	return &AgentResult{
		Turns:   cfg.MaxTurns,
		Summary: "Agent exhausted maximum turns without submitting an answer",
	}, nil
}

// collectStream drains a StreamEvent channel and collects tool calls.
func collectStream(ch <-chan connector.StreamEvent, toolCalls *[]connector.ToolCall) error {
	for ev := range ch {
		switch {
		case ev.ToolCallStart != nil:
			for len(*toolCalls) <= ev.ToolCallStart.Index {
				*toolCalls = append(*toolCalls, connector.ToolCall{})
			}
			(*toolCalls)[ev.ToolCallStart.Index].ID = ev.ToolCallStart.ID
			(*toolCalls)[ev.ToolCallStart.Index].Name = ev.ToolCallStart.Name
		case ev.ToolCallDelta != nil:
			for len(*toolCalls) <= ev.ToolCallDelta.Index {
				*toolCalls = append(*toolCalls, connector.ToolCall{})
			}
			(*toolCalls)[ev.ToolCallDelta.Index].Arguments += ev.ToolCallDelta.Arguments
		case ev.Error != nil:
			return ev.Error.Err
		}
	}
	return nil
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
