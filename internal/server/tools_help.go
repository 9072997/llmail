package server

import (
	"context"
	"embed"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

//go:embed help/*.txt
var helpFS embed.FS

var helpTopics = loadHelpTopics()

func loadHelpTopics() map[string]string {
	entries, err := helpFS.ReadDir("help")
	if err != nil {
		panic("reading embedded help dir: " + err.Error())
	}

	topics := make(map[string]string, len(entries))
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".txt")
		data, err := helpFS.ReadFile("help/" + e.Name())
		if err != nil {
			panic("reading embedded help file: " + err.Error())
		}
		topics[name] = string(data)
	}
	return topics
}

func (s *Server) registerHelpTool() {
	topicList := make([]string, 0, len(helpTopics))
	for k := range helpTopics {
		topicList = append(topicList, k)
	}

	s.mcp.AddTool(
		mcp.NewTool("help",
			mcp.WithDescription("Get detailed help on a topic. Available topics: "+strings.Join(topicList, ", ")),
			mcp.WithString("topic", mcp.Required(), mcp.Description("Help topic name")),
			mcp.WithToolAnnotation(mcp.ToolAnnotation{
				Title:        "Help",
				ReadOnlyHint: mcp.ToBoolPtr(true),
			}),
		),
		s.handleHelp,
	)
}

func (s *Server) handleHelp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	topic := stringFrom(args, "topic")
	if topic == "" {
		return errorResult("topic parameter is required"), nil
	}

	content, ok := helpTopics[topic]
	if !ok {
		topicList := make([]string, 0, len(helpTopics))
		for k := range helpTopics {
			topicList = append(topicList, k)
		}
		return errorResult("unknown topic: " + topic + ". Available topics: " + strings.Join(topicList, ", ")), nil
	}

	return textResult(content), nil
}
