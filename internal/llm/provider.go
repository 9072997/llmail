package llm

import (
	"context"
	"fmt"

	"github.com/jpennington/llmail/internal/config"
)

// ContentPart represents a piece of content in a message.
type ContentPart struct {
	Type     string // "text", "image", or "audio"
	Text     string
	MediaB64 string // base64 data for image or audio parts
	MIMEType string
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	CallID  string
	Content []ContentPart
	IsError bool
}

// Message represents a conversation message.
type Message struct {
	Role       string        // system, user, assistant, tool
	Content    []ContentPart
	ToolCalls  []ToolCall
	ToolResult *ToolResult
}

// Usage represents token usage from an LLM response.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// StreamDelta represents an incremental update during streaming.
type StreamDelta struct {
	Text       string
	ToolCalls  []ToolCall // accumulated
	Done       bool
	StopReason string // "end_turn", "tool_use"
}

// ToolDef defines a tool available to the LLM.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema properties
	Required    []string
}

// Provider is the interface for LLM backends.
type Provider interface {
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onDelta func(StreamDelta)) (Message, Usage, error)
	Name() string
}

// NewProvider creates a Provider from the given config.
func NewProvider(cfg config.LLMConfig) (Provider, error) {
	apiKey := resolveAPIKey(cfg)

	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.7
	}

	switch cfg.Provider {
	case "openai", "openrouter", "ollama", "openai-compatible":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			switch cfg.Provider {
			case "openrouter":
				baseURL = "https://openrouter.ai/api/v1"
			case "ollama":
				baseURL = "http://localhost:11434/v1"
			}
		}
		return newOpenAIProvider(cfg, apiKey, baseURL)
	case "anthropic":
		return newAnthropicProvider(cfg, apiKey)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
	}
}

func resolveAPIKey(cfg config.LLMConfig) string {
	if cfg.Provider == "ollama" {
		return "" // no key needed
	}
	if cfg.APIKeyStorage == "" {
		return ""
	}
	key, err := config.RetrieveAPIKey(cfg.Provider, cfg.APIKeyStorage, cfg.EncryptedAPIKey)
	if err != nil {
		return ""
	}
	return key
}
