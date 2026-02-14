package modes

import "fmt"

// Mode defines the behavior for a search mode.
type Mode interface {
	Name() string
	SystemPromptAddition(ctx PromptContext) string
	TurnPressure(turn, maxTurns, turnsUsed int) *string
	ShouldNudge(consecutiveEmpty int) *string
	ShouldForceSubmit(turn, maxTurns int, totalTokens, tokenBudget int64) bool
}

// PromptContext carries project metadata needed by mode prompt additions.
type PromptContext struct {
	ProjectTree  string
	Language     string
	FileStats    string
	HasAllowList bool
}

// Resolve returns the Mode for the given name, or an error for unknown modes.
func Resolve(name string) (Mode, error) {
	switch name {
	case "auto":
		return &AutoMode{}, nil
	case "locate":
		return LocateMode{}, nil
	case "explore":
		return ExploreMode{}, nil
	case "trace":
		return TraceMode{}, nil
	default:
		return nil, fmt.Errorf("invalid mode '%s': must be auto, locate, explore, or trace", name)
	}
}
