package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jpennington/llmail/internal/config"
	imaplib "github.com/jpennington/llmail/internal/imap"
	"github.com/jpennington/llmail/internal/indexer"
	"github.com/jpennington/llmail/internal/llm"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var (
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	debugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
)

// ChatSession manages an interactive chat with an LLM that can call MCP tools.
type ChatSession struct {
	provider        llm.Provider
	mcpClient       *client.Client
	tools           []llm.ToolDef
	messages        []llm.Message
	cfg             *config.Config
	indexer         *indexer.Indexer
	pool            *imaplib.Pool
	debug           bool
	lastInputTokens int
}

// NewChatSession creates a new chat session. The mcpClient must already be started and initialized.
func NewChatSession(provider llm.Provider, mcpClient *client.Client, cfg *config.Config, idx *indexer.Indexer, pool *imaplib.Pool, debug bool) *ChatSession {
	return &ChatSession{
		provider:  provider,
		mcpClient: mcpClient,
		cfg:       cfg,
		indexer:   idx,
		pool:      pool,
		debug:     debug,
	}
}

// Run starts the interactive chat loop.
func (s *ChatSession) Run(ctx context.Context) error {
	// Discover available tools
	toolsResult, err := s.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("listing tools: %w", err)
	}
	s.tools = llm.ConvertTools(toolsResult.Tools)

	// Add system prompt
	systemPrompt := buildSystemPrompt(ctx, s.cfg, s.pool)
	s.messages = append(s.messages, llm.Message{
		Role:    "system",
		Content: []llm.ContentPart{{Type: "text", Text: systemPrompt}},
	})

	if s.debug {
		for _, line := range strings.Split(systemPrompt, "\n") {
			fmt.Println(debugStyle.Render(line))
		}
	}

	fmt.Printf("llmail chat (%s) - type /help for commands, /quit to exit\n\n", s.provider.Name())

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(promptStyle.Render(s.promptString()))
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle slash commands
		switch input {
		case "/quit", "/exit":
			fmt.Println("Goodbye!")
			return nil
		case "/clear":
			s.messages = s.messages[:1] // keep system prompt
			fmt.Println("Conversation cleared.")
			continue
		case "/status":
			s.printIndexStatus()
			continue
		case "/sync":
			s.syncNow(ctx)
			continue
		case "/compact":
			s.compact(ctx)
			continue
		case "/help":
			fmt.Println("Commands:")
			fmt.Println("  /quit    - exit chat")
			fmt.Println("  /clear   - clear conversation history")
			fmt.Println("  /compact - summarize and compress conversation")
			fmt.Println("  /status  - show index sync status")
			fmt.Println("  /sync    - trigger immediate index sync")
			fmt.Println("  /help    - show this help")
			continue
		}

		// Append user message
		s.messages = append(s.messages, llm.Message{
			Role:    "user",
			Content: []llm.ContentPart{{Type: "text", Text: input}},
		})

		// Tool loop: keep calling the LLM until it stops requesting tools
		if err := s.runToolLoop(ctx); err != nil {
			for _, line := range strings.Split(fmt.Sprintf("Error: %v", err), "\n") {
				fmt.Fprintln(os.Stderr, errorStyle.Render(line))
			}
		}
		fmt.Println()
	}

	return scanner.Err()
}

func (s *ChatSession) runToolLoop(ctx context.Context) error {
	for {
		fmt.Print("\n")

		response, usage, err := s.provider.ChatStream(ctx, s.messages, s.tools, func(delta llm.StreamDelta) {
			if delta.Text != "" {
				fmt.Print(delta.Text)
			}
		})
		if err != nil {
			return err
		}
		if usage.InputTokens > 0 {
			s.lastInputTokens = usage.InputTokens
		}

		// Append assistant message
		s.messages = append(s.messages, response)

		// If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			fmt.Println()
			return nil
		}

		// Execute each tool call
		fmt.Println()
		for _, tc := range response.ToolCalls {
			argsDisplay := formatToolArgs(tc.Arguments)
			fmt.Println(toolStyle.Render(fmt.Sprintf("> Calling %s(%s)...", tc.Name, argsDisplay)))

			result, err := s.callTool(ctx, tc)
			if err != nil {
				// Tool execution error - report as error result
				s.messages = append(s.messages, llm.Message{
					Role: "tool",
					ToolResult: &llm.ToolResult{
						CallID:  tc.ID,
						Content: []llm.ContentPart{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
						IsError: true,
					},
				})
				continue
			}

			s.messages = append(s.messages, llm.Message{
				Role:       "tool",
				ToolResult: result,
			})
		}

		// Loop back to send tool results to the LLM
	}
}

func (s *ChatSession) callTool(ctx context.Context, tc llm.ToolCall) (*llm.ToolResult, error) {
	if s.debug {
		reqJSON, _ := json.MarshalIndent(tc.Arguments, "  ", "  ")
		for _, line := range strings.Split(fmt.Sprintf("  ← %s request:\n  %s", tc.Name, reqJSON), "\n") {
			fmt.Println(debugStyle.Render(line))
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = tc.Name
	req.Params.Arguments = tc.Arguments

	result, err := s.mcpClient.CallTool(ctx, req)
	if err != nil {
		return nil, err
	}

	if s.debug {
		hasGuardScore :=
			result.Meta != nil &&
				result.Meta.AdditionalFields != nil &&
				result.Meta.AdditionalFields["guardScore"] != nil
		for _, c := range result.Content {
			raw, _ := json.Marshal(c)
			var tc struct {
				Text string `json:"text"`
			}
			json.Unmarshal(raw, &tc)
			if tc.Text != "" {
				// Render each line separately to avoid lipgloss adding extra blank lines
				lines := strings.Split(tc.Text, "\n")
				fmt.Print(debugStyle.Render("  →"))
				if hasGuardScore {
					score := fmt.Sprint(result.Meta.AdditionalFields["guardScore"])
					fmt.Println(debugStyle.Render(fmt.Sprintf(" (guard score: %s)", score)))
				} else if len(lines) > 1 {
					fmt.Println()
				} else {
					fmt.Print(" ")
				}
				for _, line := range lines {
					fmt.Println(debugStyle.Render(line))
				}
			}
		}
	}

	parts := convertMCPContent(result.Content)

	return &llm.ToolResult{
		CallID:  tc.ID,
		Content: parts,
		IsError: result.IsError,
	}, nil
}

func convertMCPContent(content []mcp.Content) []llm.ContentPart {
	var parts []llm.ContentPart
	for _, c := range content {
		raw, err := json.Marshal(c)
		if err != nil {
			continue
		}
		var base struct {
			Type string `json:"type"`
		}
		json.Unmarshal(raw, &base)

		switch base.Type {
		case "text":
			var tc struct {
				Text string `json:"text"`
			}
			json.Unmarshal(raw, &tc)
			parts = append(parts, llm.ContentPart{Type: "text", Text: tc.Text})
		case "image":
			var ic struct {
				Data     string `json:"data"`
				MIMEType string `json:"mimeType"`
			}
			json.Unmarshal(raw, &ic)
			parts = append(parts, llm.ContentPart{Type: "image", MediaB64: ic.Data, MIMEType: ic.MIMEType})
		case "resource":
			var rc struct {
				Resource struct {
					Text     string `json:"text,omitempty"`
					Blob     string `json:"blob,omitempty"`
					MIMEType string `json:"mimeType,omitempty"`
				} `json:"resource"`
			}
			json.Unmarshal(raw, &rc)
			if rc.Resource.Text != "" {
				parts = append(parts, llm.ContentPart{Type: "text", Text: rc.Resource.Text})
			} else if rc.Resource.Blob != "" {
				mime := rc.Resource.MIMEType
				if strings.HasPrefix(mime, "image/") {
					parts = append(parts, llm.ContentPart{Type: "image", MediaB64: rc.Resource.Blob, MIMEType: mime})
				} else if strings.HasPrefix(mime, "audio/") {
					parts = append(parts, llm.ContentPart{Type: "audio", MediaB64: rc.Resource.Blob, MIMEType: mime})
				} else {
					parts = append(parts, llm.ContentPart{Type: "text", Text: fmt.Sprintf("[Binary attachment: %s]", mime)})
				}
			}
		}
	}
	if len(parts) == 0 {
		parts = append(parts, llm.ContentPart{Type: "text", Text: "(no content)"})
	}
	return parts
}

func (s *ChatSession) compact(ctx context.Context) {
	if len(s.messages) <= 1 {
		fmt.Println("Nothing to compact.")
		return
	}

	summaryRequest := llm.Message{
		Role: "user",
		Content: []llm.ContentPart{{
			Type: "text",
			Text: "Summarize our conversation so far in a concise but complete way. Include key decisions, findings, and any ongoing tasks. This summary will replace the conversation history.",
		}},
	}

	msgs := append(s.messages, summaryRequest)

	fmt.Print("\n")
	response, _, err := s.provider.ChatStream(ctx, msgs, s.tools, func(delta llm.StreamDelta) {
		if delta.Text != "" {
			fmt.Print(delta.Text)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render(fmt.Sprintf("Error compacting: %v", err)))
		return
	}
	fmt.Println()

	// Keep system prompt, summary request, and summary response
	s.messages = []llm.Message{s.messages[0], summaryRequest, response}
	s.lastInputTokens = 0 // will be accurate again after next LLM call
	fmt.Println("Conversation compacted.")
}

func (s *ChatSession) syncNow(ctx context.Context) {
	if s.indexer == nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("Index is not enabled."))
		return
	}
	fmt.Println("Syncing index...")
	s.indexer.SyncNow(ctx, s.pool)
	fmt.Println("Sync complete.")
}

func (s *ChatSession) printIndexStatus() {
	if s.indexer == nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("Index is not enabled."))
		return
	}
	fmt.Print(s.indexer.StatusText(""))
}

func (s *ChatSession) promptString() string {
	if s.lastInputTokens == 0 {
		return "you> "
	}
	return fmt.Sprintf("you (%s)> ", formatTokenCount(s.lastInputTokens))
}

func formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	k := float64(tokens) / 1000.0
	if k < 10 {
		return fmt.Sprintf("%.1fk", k)
	}
	return fmt.Sprintf("%.0fk", k)
}

func formatToolArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for k, v := range args {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", k, val))
		default:
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		}
	}
	return strings.Join(parts, ", ")
}
