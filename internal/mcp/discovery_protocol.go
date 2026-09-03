package mcp

// ToolDefinition is the protocol-level representation of a discoverable MCP tool.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

const ToolsListMethod = "tools/list"
