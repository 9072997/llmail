package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jpennington/llmail/internal/config"
	openai "github.com/sashabaranov/go-openai"
)

type openaiProvider struct {
	client *openai.Client
	model  string
	cfg    config.LLMConfig
}

func newOpenAIProvider(cfg config.LLMConfig, apiKey, baseURL string) (*openaiProvider, error) {
	ocfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		ocfg.BaseURL = baseURL
	}
	return &openaiProvider{
		client: openai.NewClientWithConfig(ocfg),
		model:  cfg.Model,
		cfg:    cfg,
	}, nil
}

func (p *openaiProvider) Name() string {
	return fmt.Sprintf("%s/%s", p.cfg.Provider, p.model)
}

func (p *openaiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, onDelta func(StreamDelta)) (Message, Usage, error) {
	oaiMessages := convertToOpenAIMessages(messages)
	oaiTools := convertToOpenAITools(tools)

	req := openai.ChatCompletionRequest{
		Model:         p.model,
		Messages:      oaiMessages,
		MaxTokens:     p.cfg.MaxTokens,
		Temperature:   float32(p.cfg.Temperature),
		Stream:        true,
		StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	}
	if len(oaiTools) > 0 {
		req.Tools = oaiTools
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		detail := openaiErrorDetail(err)
		// Dump last few messages to help debug 400 errors.
		start := 0
		if len(oaiMessages) > 4 {
			start = len(oaiMessages) - 4
		}
		tail, _ := json.MarshalIndent(oaiMessages[start:], "", "  ")
		return Message{}, Usage{}, fmt.Errorf("openai stream: %w\nLast %d messages:\n%s", detail, len(oaiMessages)-start, tail)
	}
	defer stream.Close()

	var (
		fullText   string
		toolCalls  []ToolCall
		tcArgBufs  = make(map[int]*toolCallAccum) // index -> accumulator
		stopReason string
		usage      Usage
	)

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Message{}, Usage{}, fmt.Errorf("openai stream recv: %w", err)
		}

		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.FinishReason != "" {
			stopReason = string(choice.FinishReason)
		}

		// Accumulate text
		if choice.Delta.Content != "" {
			fullText += choice.Delta.Content
			if onDelta != nil {
				onDelta(StreamDelta{Text: choice.Delta.Content})
			}
		}

		// Accumulate tool call deltas
		for _, tc := range choice.Delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			acc, ok := tcArgBufs[idx]
			if !ok {
				acc = &toolCallAccum{}
				tcArgBufs[idx] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args += tc.Function.Arguments
		}
	}

	// Build final tool calls
	for i := 0; i < len(tcArgBufs); i++ {
		acc := tcArgBufs[i]
		var args map[string]any
		if acc.args != "" {
			if err := json.Unmarshal([]byte(acc.args), &args); err != nil {
				args = map[string]any{"_raw": acc.args}
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        acc.id,
			Name:      acc.name,
			Arguments: args,
		})
	}

	finalStop := "end_turn"
	if stopReason == string(openai.FinishReasonToolCalls) || stopReason == "tool_calls" {
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

type toolCallAccum struct {
	id   string
	name string
	args string
}

func convertToOpenAIMessages(msgs []Message) []openai.ChatCompletionMessage {
	var out []openai.ChatCompletionMessage
	for _, m := range msgs {
		switch m.Role {
		case "system":
			text := contentPartsToText(m.Content)
			out = append(out, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: text,
			})
		case "user":
			parts := contentPartsToOpenAIMulti(m.Content)
			out = append(out, openai.ChatCompletionMessage{
				Role:         openai.ChatMessageRoleUser,
				MultiContent: parts,
			})
		case "assistant":
			msg := openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant,
			}
			// Always set Content to a non-empty value when there are tool calls.
			// Ollama rejects messages with null/missing content ("invalid message content type: <nil>").
			text := contentPartsToText(m.Content)
			if text == "" && len(m.ToolCalls) > 0 {
				msg.Content = " "
			} else {
				msg.Content = text
			}
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
						ID:   tc.ID,
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      tc.Name,
							Arguments: string(argsJSON),
						},
					})
				}
			}
			out = append(out, msg)
		case "tool":
			if m.ToolResult != nil {
				text := contentPartsToText(m.ToolResult.Content)
				out = append(out, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    text,
					ToolCallID: m.ToolResult.CallID,
				})
			}
		}
	}
	return out
}

func convertToOpenAITools(defs []ToolDef) []openai.Tool {
	tools := make([]openai.Tool, len(defs))
	for i, d := range defs {
		params := map[string]any{
			"type":       "object",
			"properties": d.Parameters,
		}
		if len(d.Required) > 0 {
			params["required"] = d.Required
		}
		tools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  params,
			},
		}
	}
	return tools
}

func contentPartsToText(parts []ContentPart) string {
	var text string
	for _, p := range parts {
		switch p.Type {
		case "text":
			text += p.Text
		case "image":
			text += fmt.Sprintf("[Image: %s]", p.MIMEType)
		case "audio":
			text += fmt.Sprintf("[Audio: %s]", p.MIMEType)
		}
	}
	return text
}

func contentPartsToOpenAIMulti(parts []ContentPart) []openai.ChatMessagePart {
	var out []openai.ChatMessagePart
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: p.Text,
			})
		case "image":
			out = append(out, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", p.MIMEType, p.MediaB64),
				},
			})
		default:
			out = append(out, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeText,
				Text: fmt.Sprintf("[%s: %s]", p.Type, p.MIMEType),
			})
		}
	}
	if len(out) == 0 {
		out = append(out, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeText,
			Text: "",
		})
	}
	return out
}

// openaiErrorDetail extracts additional detail from OpenAI API errors for debugging.
func openaiErrorDetail(err error) error {
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return fmt.Errorf("%w\nResponse body: %s", err, reqErr.Body)
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		extra := fmt.Sprintf("type=%s", apiErr.Type)
		if apiErr.Param != nil {
			extra += fmt.Sprintf(", param=%s", *apiErr.Param)
		}
		if apiErr.Code != nil {
			extra += fmt.Sprintf(", code=%v", apiErr.Code)
		}
		return fmt.Errorf("%w (%s)", err, extra)
	}
	return err
}
