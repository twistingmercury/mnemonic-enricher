package mcpserver

import (
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchPatternsInput is the input type for the search_patterns MCP tool.
type SearchPatternsInput struct {
	Query     string   `json:"query"      jsonschema:"Natural language search query"`
	Limit     *int     `json:"limit,omitempty"     jsonschema:"Maximum number of results to return (default 10, max 50)"`
	Threshold *float64 `json:"threshold,omitempty" jsonschema:"Minimum cosine similarity score 0.0-1.0 (default 0.7)"`
	Tags      []string `json:"tags,omitempty"      jsonschema:"Conjunctive (AND) filter by tag"`
	Language  string   `json:"language,omitempty"  jsonschema:"Filter results by pattern language"`
	Domain    string   `json:"domain,omitempty"    jsonschema:"Filter results by pattern domain"`
}

// FindRelatedPatternsInput is the input type for the find_related_patterns MCP tool.
type FindRelatedPatternsInput struct {
	PatternID string `json:"pattern_id"          jsonschema:"UUID of the source pattern"`
	Limit     *int   `json:"limit,omitempty"     jsonschema:"Maximum number of results to return (default 5, max 20)"`
}

// GetPatternInput is the input type for the get_pattern MCP tool.
type GetPatternInput struct {
	ID string `json:"id" jsonschema:"UUID of the pattern to retrieve"`
}

// Sentinel errors for the MCP server layer.
var (
	// ErrInvalidInput is returned when a tool receives invalid or out-of-range parameters.
	ErrInvalidInput = errors.New("invalid input")

	// ErrPatternNotFound is returned when a requested pattern does not exist.
	ErrPatternNotFound = errors.New("pattern not found")

	// ErrServiceUnavailable is returned when an upstream service or dependency
	// is unavailable or returns an unexpected error.
	ErrServiceUnavailable = errors.New("service unavailable")
)

// MCP tool definitions registered on the server.
var (
	searchPatternsTool = &mcp.Tool{
		Name:        "search_patterns",
		Description: "Semantic search over the team knowledge graph. Returns patterns ranked by vector similarity.",
	}

	findRelatedPatternsTool = &mcp.Tool{
		Name:        "find_related_patterns",
		Description: "Find patterns related to a given pattern by UUID, ranked by graph proximity.",
	}

	getPatternTool = &mcp.Tool{
		Name:        "get_pattern",
		Description: "Retrieve a single enriched pattern by UUID, including its graph context.",
	}
)
