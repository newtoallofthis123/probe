package modes

// ExploreMode understands HOW something works across the codebase.
type ExploreMode struct{}

func (ExploreMode) Name() string { return "explore" }

func (ExploreMode) SystemPromptAddition(_ PromptContext) string {
	return `## Search Mode: Explore

You are in EXPLORE mode. The user wants to understand HOW something works across the codebase.

Strategy:
- Start broad: tree or find_files to identify candidate files
- Read multiple candidate files in a single turn — don't read them one at a time
- Synthesize findings across files — the value is in connecting dots
- Return multiple files with context, not just paths
- Be thorough. Check related files — imports, configs, tests.`
}

func (ExploreMode) TurnPressure(turn, maxTurns, turnsUsed int) *string {
	budget := maxTurns - turnsUsed
	used := turn - turnsUsed
	if budget > 0 && float64(used) > float64(budget)*0.6 {
		msg := "You've used over 60% of your budget. Start submitting partial results."
		return &msg
	}
	return nil
}

func (ExploreMode) ShouldNudge(consecutiveEmpty int) *string {
	if consecutiveEmpty >= 3 {
		msg := "Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."
		return &msg
	}
	return nil
}

func (ExploreMode) ShouldForceSubmit(turn, maxTurns int, totalTokens, tokenBudget int64) bool {
	remaining := maxTurns - turn - 1
	return totalTokens > int64(float64(tokenBudget)*0.8) || remaining <= 0
}
