package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jpennington/llmail/internal/config"
)

type anthropicProvider struct {
	client *anthropic.Client
	model  string
	cfg    config.LLMConfig
}

func newAnthropicProvider(cfg config.LLMConfig, apiKey string) (*anthropicProvider, error) {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	client := anthropic.NewClient(opts...)
	return &anthropicProvider{
		client: &client,
		model:  cfg.Model,
		cfg:    cfg,
	}, nil
}

func (p *anthropicProvider) Name() string {
	return fmt.Sprintf("anthropic/%s", p.model)
}

func (p *anthropicProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onDelta func(StreamDelta)) (Message, Usage, error) {
	// Extract system prompt from messages
	var systemText string
	var nonSystemMsgs []Message
	for _, m := range messages {
		if m.Role == "system" {
			systemText = contentPartsToText(m.Content)
		} else {
			nonSystemMsgs = append(nonSystemMsgs, m)
		}
	}

	anthropicMsgs := convertToAnthropicMessages(nonSystemMsgs)
	anthropicTools := convertToAnthropicTools(tools)

	params := anthropic.MessageNewParams{
		Model:     p.model,
		Messages:  anthropicMsgs,
		MaxTokens: int64(p.cfg.MaxTokens),
	}
	if systemText != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemText},
		}
	}
	if len(anthropicTools) > 0 {
		params.Tools = anthropicTools
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	type toolAccum struct {
		id   string
		name string
		args string
	}

	var (
		fullText    string
		toolCalls   []ToolCall
		currentTool *toolAccum
		usage       Usage
	)

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			usage.InputTokens = int(e.Message.Usage.InputTokens)

		case anthropic.ContentBlockStartEvent:
			// Check if a new tool_use block is starting
			cb := e.ContentBlock
			if cb.Type == "tool_use" {
				tu := cb.AsToolUse()
				currentTool = &toolAccum{
					id:   tu.ID,
					name: tu.Name,
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			switch delta := e.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				fullText += delta.Text
				if onDelta != nil {
					onDelta(StreamDelta{Text: delta.Text})
				}
			case anthropic.InputJSONDelta:
				if currentTool != nil {
					currentTool.args += delta.PartialJSON
				}
			}

		case anthropic.ContentBlockStopEvent:
			if currentTool != nil {
				var args map[string]any
				if currentTool.args != "" {
					if err := json.Unmarshal([]byte(currentTool.args), &args); err != nil {
						args = map[string]any{}
					}
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:        currentTool.id,
					Name:      currentTool.name,
					Arguments: args,
				})
				currentTool = nil
			}

		case anthropic.MessageDeltaEvent:
			usage.OutputTokens = int(e.Usage.OutputTokens)
		}
	}

	if err := stream.Err(); err != nil {
		return Message{}, Usage{}, fmt.Errorf("anthropic stream: %w", err)
	}

	finalStop := "end_turn"
	if len(toolCalls) > 0 {
		finalStop = "tool_use"
	}

	if onDelta != nil {
		onDelta(StreamDelta{Done: true, StopReason: finalStop, ToolCalls: toolCalls})
	}

	msg := Message{
		Role:      "assistant",
		ToolCalls: toolCalls,
	}
	if fullText != "" {
		msg.Content = []ContentPart{{Type: "text", Text: fullText}}
	}
	return msg, usage, nil
}

func convertToAnthropicMessages(msgs []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range msgs {
		switch m.Role {
		case "user":
			blocks := contentPartsToAnthropicBlocks(m.Content)
			out = append(out, anthropic.NewUserMessage(blocks...))
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if len(m.Content) > 0 {
				blocks = append(blocks, contentPartsToAnthropicBlocks(m.Content)...)
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Arguments, tc.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		case "tool":
			if m.ToolResult != nil {
				text := contentPartsToText(m.ToolResult.Content)
				out = append(out, anthropic.NewUserMessage(
					anthropic.NewToolResultBlock(m.ToolResult.CallID, text, m.ToolResult.IsError),
				))
			}
		}
	}
	return out
}

func convertToAnthropicTools(defs []ToolDef) []anthropic.ToolUnionParam {
	tools := make([]anthropic.ToolUnionParam, len(defs))
	for i, d := range defs {
		inputSchema := anthropic.ToolInputSchemaParam{
			Properties: d.Parameters,
		}
		if len(d.Required) > 0 {
			inputSchema.Required = d.Required
		}
		tools[i] = anthropic.ToolUnionParamOfTool(inputSchema, d.Name)
		tools[i].OfTool.Description = anthropic.String(d.Description)
	}
	return tools
}

func contentPartsToAnthropicBlocks(parts []ContentPart) []anthropic.ContentBlockParamUnion {
	var out []anthropic.ContentBlockParamUnion
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, anthropic.NewTextBlock(p.Text))
		case "image":
			out = append(out, anthropic.NewImageBlockBase64(p.MIMEType, p.MediaB64))
		default:
			out = append(out, anthropic.NewTextBlock(fmt.Sprintf("[%s: %s]", p.Type, p.MIMEType)))
		}
	}
	return out
}
