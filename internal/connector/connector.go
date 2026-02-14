package connector

import "context"

// Connector is the provider-agnostic interface for LLM communication.
// Implementations translate between provider-specific SDKs and these types.
type Connector interface {
	// StreamChat sends messages to the LLM and returns a channel of streaming events.
	// The channel is closed when the response is complete or an error occurs.
	StreamChat(ctx context.Context, messages []Message, tools []ToolSchema, opts ChatOpts) (<-chan StreamEvent, error)

	// Name returns the provider name (e.g. "openai", "anthropic", "google").
	Name() string
}

// Role represents a message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a provider-agnostic chat message.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string // for tool result messages
}

// ToolCall represents a tool invocation from the assistant.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolSchema is a provider-agnostic tool definition.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// ChatOpts controls per-request behavior.
type ChatOpts struct {
	Model             string
	ForceToolChoice   string // if non-empty, force this tool
	ParallelToolCalls bool
}

// StreamEvent is a sum type for events from the connector.
// Exactly one field is non-nil per event.
type StreamEvent struct {
	TextDelta     *TextDelta
	ToolCallStart *ToolCallStart
	ToolCallDelta *ToolCallDelta
	Usage         *UsageEvent
	Done          *Done
	Error         *ErrorEvent
}

// TextDelta carries a chunk of assistant text.
type TextDelta struct {
	Text string
}

// ToolCallStart signals the beginning of a tool call.
type ToolCallStart struct {
	Index int
	ID    string
	Name  string
}

// ToolCallDelta carries a chunk of tool call arguments.
type ToolCallDelta struct {
	Index     int
	Arguments string
}

// UsageEvent carries token usage information.
type UsageEvent struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// Done signals the response is complete.
type Done struct {
	// FinishReason is provider-specific but typically "stop" or "tool_calls".
	FinishReason string
}

// ErrorEvent carries a streaming error.
type ErrorEvent struct {
	Err error
}

// Helper constructors for Message.

// SystemMessage creates a system message.
func SystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// UserMessage creates a user message.
func UserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// AssistantMessage creates an assistant message with optional tool calls.
func AssistantMessage(content string, toolCalls ...ToolCall) Message {
	return Message{Role: RoleAssistant, Content: content, ToolCalls: toolCalls}
}

// ToolMessage creates a tool result message.
func ToolMessage(toolCallID string, content string) Message {
	return Message{Role: RoleTool, ToolCallID: toolCallID, Content: content}
}
