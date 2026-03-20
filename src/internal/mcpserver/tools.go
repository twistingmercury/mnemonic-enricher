package mcpserver

import "github.com/modelcontextprotocol/go-sdk/mcp"

var searchPatternsTool = &mcp.Tool{
	Name:        "search_patterns",
	Description: "Semantic search over the team knowledge graph. Returns patterns ranked by vector similarity. Only enriched patterns appear in results.",
}

var findRelatedPatternsTool = &mcp.Tool{
	Name:        "find_related_patterns",
	Description: "Find patterns related to a given pattern via the knowledge graph. Traverses RELATED_TO edges in Neo4j and returns patterns ranked by concept overlap strength.",
}

var getPatternTool = &mcp.Tool{
	Name:        "get_pattern",
	Description: "Retrieve a specific pattern by ID. Returns full pattern content, metadata, related patterns, and extracted concepts. Graph relationships are omitted when enrichment is still pending.",
}
