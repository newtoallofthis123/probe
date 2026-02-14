package modes

// AutoMode selects a delegate mode based on the query. Before SetDelegate is
// called, it behaves like LocateMode (the default fallback).
type AutoMode struct {
	delegate Mode
}

func (a *AutoMode) Name() string {
	if a.delegate != nil {
		return a.delegate.Name()
	}
	return "auto"
}

// SetDelegate sets the resolved mode after the auto-selection turn.
func (a *AutoMode) SetDelegate(m Mode) {
	a.delegate = m
}

func (a *AutoMode) SystemPromptAddition(ctx PromptContext) string {
	if a.delegate != nil {
		return a.delegate.SystemPromptAddition(ctx)
	}
	return LocateMode{}.SystemPromptAddition(ctx)
}

func (a *AutoMode) TurnPressure(turn, maxTurns, turnsUsed int) *string {
	if a.delegate != nil {
		return a.delegate.TurnPressure(turn, maxTurns, turnsUsed)
	}
	return LocateMode{}.TurnPressure(turn, maxTurns, turnsUsed)
}

func (a *AutoMode) ShouldNudge(consecutiveEmpty int) *string {
	if a.delegate != nil {
		return a.delegate.ShouldNudge(consecutiveEmpty)
	}
	return LocateMode{}.ShouldNudge(consecutiveEmpty)
}

func (a *AutoMode) ShouldForceSubmit(turn, maxTurns int, totalTokens, tokenBudget int64) bool {
	if a.delegate != nil {
		return a.delegate.ShouldForceSubmit(turn, maxTurns, totalTokens, tokenBudget)
	}
	return LocateMode{}.ShouldForceSubmit(turn, maxTurns, totalTokens, tokenBudget)
}
