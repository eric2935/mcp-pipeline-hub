package protocol

import "encoding/json"

const (
	ProtocolDefault = "2024-11-05"
	ServerName      = "agentic-mcp-hub"
)

// ServerVersion is the MCP serverInfo version. It is a var so release builds can set it with -ldflags -X.
var ServerVersion = "0.1.0"

// ToolDef is one entry in tools/list.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Executor runs a tool call for the stdio server.
type Executor interface {
	Definitions() []ToolDef
	Invoke(name string, args json.RawMessage) (any, error)
}

// Negotiate returns the client version when it is supported.
func Negotiate(client string) string {
	switch client {
	case "2024-11-05", "2025-03-26":
		return client
	default:
		return ProtocolDefault
	}
}
