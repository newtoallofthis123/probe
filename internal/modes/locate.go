package modes

import "fmt"

// LocateMode finds WHERE something is — a file, function, type, or config key.
type LocateMode struct{}

func (LocateMode) Name() string { return "locate" }

func (LocateMode) SystemPromptAddition(_ PromptContext) string {
	return `## Search Mode: Locate

You are in LOCATE mode. The user wants to find WHERE something is — a file, function, type, or config key.

Strategy:
- For files: use tree or find_files first
- For symbols: use grep with definition patterns first
- If multiple candidate patterns exist, grep for them all in one turn
- Report the first confident match. Don't over-verify.
- Skip reading file contents unless there's genuine ambiguity.
- This should resolve in 1-3 turns. Be fast.`
}

func (LocateMode) TurnPressure(turn, maxTurns, turnsUsed int) *string {
	remaining := maxTurns - turn - 1
	if remaining <= 2 {
		msg := fmt.Sprintf("You're in locate mode with %d turns left. Submit your best match now.", remaining)
		return &msg
	}
	return nil
}

func (LocateMode) ShouldNudge(consecutiveEmpty int) *string {
	if consecutiveEmpty >= 3 {
		msg := "Your last 3 searches returned no results. Consider trying different search terms, broader patterns, or submit an empty answer if nothing is relevant."
		return &msg
	}
	return nil
}

func (LocateMode) ShouldForceSubmit(turn, maxTurns int, totalTokens, tokenBudget int64) bool {
	remaining := maxTurns - turn - 1
	return totalTokens > int64(float64(tokenBudget)*0.8) || remaining <= 0
}
