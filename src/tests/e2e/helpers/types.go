package helpers

// ErrorResponse represents RFC 7807 Problem Details error response.
// OpenAPI: api/openapi/mnemonic-v1.yaml:152 (ErrorResponse)
type ErrorResponse struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	TraceID  string       `json:"traceId"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// FieldError represents individual field validation error.
// OpenAPI: api/openapi/mnemonic-v1.yaml:229 (FieldError)
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Pagination represents cursor-based pagination metadata.
// OpenAPI: api/openapi/mnemonic-v1.yaml:265 (Pagination)
type Pagination struct {
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// Agent represents the full agent definition.
// OpenAPI: api/openapi/mnemonic-v1.yaml:315 (Agent)
type Agent struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Version      string   `json:"version,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

// AgentCreate represents request body for creating a new agent.
// Required: name, system_prompt, model.
// Optional: description, allowed_tools, version.
type AgentCreate struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Version      string   `json:"version,omitempty"`
}

// AgentUpdate represents request body for updating an agent.
// Same structure as AgentCreate.
type AgentUpdate struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	Version      string   `json:"version,omitempty"`
}

// AgentSummary represents agent summary without system_prompt, returned in list responses.
type AgentSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AgentList represents paginated list of agents.
// OpenAPI: api/openapi/mnemonic-v1.yaml:514 (AgentList)
type AgentList struct {
	Data       []AgentSummary `json:"data"`
	Pagination Pagination     `json:"pagination"`
}

// AgentAssociation represents association between pattern and agent.
// OpenAPI: api/openapi/mnemonic-v1.yaml:529 (AgentAssociation)
type AgentAssociation struct {
	AgentName string  `json:"agent_name"`
	Relevance float64 `json:"relevance"`
}

// Pattern represents the full pattern definition.
// OpenAPI: api/openapi/mnemonic-v1.yaml:550 (Pattern)
type Pattern struct {
	ID                string             `json:"id,omitempty"`
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	Content           string             `json:"content"`
	Tags              []string           `json:"tags,omitempty"`
	AgentAssociations []AgentAssociation `json:"agent_associations,omitempty"`
	CreatedAt         string             `json:"created_at,omitempty"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
	EnrichmentStatus  string             `json:"enrichment_status,omitempty"`
	EnrichmentError   string             `json:"enrichment_error,omitempty"`
	EnrichedAt        string             `json:"enriched_at,omitempty"`
	EntityType        string             `json:"entity_type,omitempty"`
	Language          string             `json:"language,omitempty"`
	Domain            string             `json:"domain,omitempty"`
	Version           string             `json:"version,omitempty"`
	RelatedPatterns   []string           `json:"related_patterns,omitempty"`
}

// PatternCreate represents request body for creating a new pattern.
// OpenAPI: api/openapi/mnemonic-v1.yaml:644 (PatternCreate)
type PatternCreate struct {
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	Content           string             `json:"content"`
	Tags              []string           `json:"tags,omitempty"`
	AgentAssociations []AgentAssociation `json:"agent_associations,omitempty"`
	EntityType        string             `json:"entity_type,omitempty"`
	Language          string             `json:"language,omitempty"`
	Domain            string             `json:"domain,omitempty"`
	Version           string             `json:"version,omitempty"`
	RelatedPatterns   []string           `json:"related_patterns,omitempty"`
}

// PatternUpdate represents request body for updating a pattern.
// OpenAPI: api/openapi/mnemonic-v1.yaml:683 (PatternUpdate)
type PatternUpdate struct {
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	Content           string             `json:"content"`
	Tags              []string           `json:"tags,omitempty"`
	AgentAssociations []AgentAssociation `json:"agent_associations,omitempty"`
	EntityType        string             `json:"entity_type,omitempty"`
	Language          string             `json:"language,omitempty"`
	Domain            string             `json:"domain,omitempty"`
	Version           string             `json:"version,omitempty"`
	RelatedPatterns   []string           `json:"related_patterns,omitempty"`
}

// PatternSummary represents pattern summary without content.
// OpenAPI: api/openapi/mnemonic-v1.yaml:711 (PatternSummary)
type PatternSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// PatternList represents paginated list of patterns.
// OpenAPI: api/openapi/mnemonic-v1.yaml:752 (PatternList)
type PatternList struct {
	Data       []PatternSummary `json:"data"`
	Pagination Pagination       `json:"pagination"`
}

// PatternSearchResult represents a single chunk-based semantic search result.
type PatternSearchResult struct {
	PatternID    string   `json:"pattern_id"`
	PatternName  string   `json:"pattern_name"`
	EntityType   string   `json:"entity_type"`
	Language     string   `json:"language"`
	Domain       string   `json:"domain"`
	Tags         []string `json:"tags"`
	SectionTitle string   `json:"section_title"`
	ChunkIndex   int      `json:"chunk_index"`
	Content      string   `json:"content"`
	Similarity   float64  `json:"similarity"`
}

// PatternSearchMetadata contains metadata about a semantic search operation.
// OpenAPI: api/openapi/mnemonic-v1.yaml:752 (PatternSearchResponse.metadata)
type PatternSearchMetadata struct {
	Query            string `json:"query"`
	TotalCandidates  int    `json:"total_candidates"`
	SearchDurationMs int    `json:"search_duration_ms"`
}

// PatternSearchResponse represents the full semantic search response.
// OpenAPI: api/openapi/mnemonic-v1.yaml:740 (PatternSearchResponse)
type PatternSearchResponse struct {
	Results  []PatternSearchResult `json:"results"`
	Metadata PatternSearchMetadata `json:"metadata"`
}

// ChunkSummary represents a pattern chunk summary (chunk_index, section_title, enrichment_status).
type ChunkSummary struct {
	ChunkIndex       int    `json:"chunk_index"`
	SectionTitle     string `json:"section_title"`
	EnrichmentStatus string `json:"enrichment_status"`
}

// ChunkListResponse is the response body for GET /v1/api/patterns/:id/chunks.
type ChunkListResponse struct {
	Chunks []ChunkSummary `json:"chunks"`
	Count  int            `json:"count"`
}

// PatternAgentAssociations represents the request/response for pattern-agent associations.
// OpenAPI: api/openapi/mnemonic-v1.yaml:768 (PatternAgentAssociations)
type PatternAgentAssociations struct {
	Associations []AgentAssociation `json:"associations"`
}

// Skill represents the full skill definition.
// OpenAPI: api/openapi/mnemonic-v1.yaml (Skill)
type Skill struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Version       string            `json:"version,omitempty"`
	CreatedAt     string            `json:"created_at,omitempty"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
}

// SkillCreate represents request body for creating a new skill.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillCreate)
type SkillCreate struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Version       string            `json:"version,omitempty"`
}

// SkillUpdate represents request body for updating a skill.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillUpdate)
type SkillUpdate struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Version       string            `json:"version,omitempty"`
}

// SkillList represents paginated list of skills.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillList)
type SkillList struct {
	Data       []Skill    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// SkillFile represents a file (script, reference, or asset) attached to a skill.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillFile)
type SkillFile struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Size        int64  `json:"size,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// SkillFileCreate represents request body for uploading a skill file.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillFileCreate)
type SkillFileCreate struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding,omitempty"`
}

// SkillFileUpdate represents request body for replacing a skill file.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillFileUpdate)
type SkillFileUpdate struct {
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding,omitempty"`
}

// SkillFileList represents list of skill file summaries.
// OpenAPI: api/openapi/mnemonic-v1.yaml (SkillFileList)
type SkillFileList struct {
	Data []SkillFile `json:"data"`
}

// HealthResponse represents health check response.
// OpenAPI: api/openapi/mnemonic-v1.yaml:1185 (HealthResponse)
type HealthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
	Reason string            `json:"reason,omitempty"`
}

// VersionResponse represents version information response from GET /version.
// Fields match the handler in internal/handlers/operations/operations.go.
type VersionResponse struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	Commit    string `json:"commit"`
}

// JSON-RPC / MCP types

// JSONRPCRequest is the envelope for a JSON-RPC 2.0 request sent to the MCP endpoint.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// ToolCallParams carries the tool name and its arguments for a tools/call request.
type ToolCallParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

// JSONRPCError represents the error object inside a JSON-RPC 2.0 error response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPContent is a single content item returned inside an MCP tool result.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MCPToolResult is the result payload of a successful tools/call response.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError"`
}

// JSONRPCResponse is the envelope for a JSON-RPC 2.0 response from the MCP endpoint.
type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Result  *MCPToolResult `json:"result,omitempty"`
	Error   *JSONRPCError  `json:"error,omitempty"`
}

// ToolDefinition describes a single tool exposed by the MCP server.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolsListResult is the result payload of a tools/list response.
type ToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

// JSONRPCToolsListResp is the envelope for a JSON-RPC 2.0 tools/list response.
type JSONRPCToolsListResp struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int              `json:"id"`
	Result  *ToolsListResult `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}
