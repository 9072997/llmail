package llm

import "github.com/mark3labs/mcp-go/mcp"

// ConvertTools converts MCP tools to LLM ToolDefs.
func ConvertTools(mcpTools []mcp.Tool) []ToolDef {
	defs := make([]ToolDef, len(mcpTools))
	for i, t := range mcpTools {
		props := make(map[string]any)
		if t.InputSchema.Properties != nil {
			props = t.InputSchema.Properties
		}

		required := make([]string, len(t.InputSchema.Required))
		copy(required, t.InputSchema.Required)

		defs[i] = ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  props,
			Required:    required,
		}
	}
	return defs
}
