package modes

// TraceMode follows something through the code — a call chain, data flow, or config propagation.
type TraceMode struct{}

func (TraceMode) Name() string { return "trace" }

func (TraceMode) SystemPromptAddition(_ PromptContext) string {
	return `## Search Mode: Trace

You are in TRACE mode. The user wants to follow something through the code — a call chain, data flow, or config propagation.

Strategy:
- Find the entry point first (this is a locate sub-task)
- Follow references through the dependency chain
- When grep reveals multiple files to read, read them all in one turn
- Report an ordered path with file:line references, not a bag of files
- If the chain isn't complete by 70% budget, submit what you have and note it's partial.`
}

func (TraceMode) TurnPressure(turn, maxTurns, turnsUsed int) *string {
	budget := maxTurns - turnsUsed
	used := turn - turnsUsed
	if budget > 0 && float64(used) > float64(budget)*0.7 {
		msg := "You've used over 70% of your budget. Submit what you have, note if trace is partial."
		return &msg
	}
	return nil
}

func (TraceMode) ShouldNudge(consecutiveEmpty int) *string {
	if consecutiveEmpty >= 3 {
		msg := "Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."
		return &msg
	}
	return nil
}

func (TraceMode) ShouldForceSubmit(turn, maxTurns int, totalTokens, tokenBudget int64) bool {
	remaining := maxTurns - turn - 1
	return totalTokens > int64(float64(tokenBudget)*0.8) || remaining <= 0
}
