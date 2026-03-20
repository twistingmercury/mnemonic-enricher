package mcpserver

// SearchPatternsInput defines the parameters for the search_patterns tool.
// The SDK infers InputSchema from jsonschema struct tags.
//
// Required vs optional:
//   - Query: required (no omitempty)
//   - All other fields: optional (omitempty)
//
// Pointer types (*int, *float64) distinguish "omitted" from "explicit zero":
//   - nil means the caller did not provide the field (apply default)
//   - non-nil zero means the caller explicitly set the value to 0
type SearchPatternsInput struct {
	Query     string   `json:"query"              jsonschema:"Natural language search query"`
	Limit     *int     `json:"limit,omitempty"    jsonschema:"Maximum number of results to return (default 10, max 50)"`
	Threshold *float64 `json:"threshold,omitempty" jsonschema:"Minimum cosine similarity score 0.0-1.0 (default 0.7)"`
	Tags      []string `json:"tags,omitempty"      jsonschema:"Conjunctive (AND) filter by tag. Pattern must contain ALL specified tags."`
	Agent     string   `json:"agent,omitempty"     jsonschema:"Filter results by agent association"`
	Language  string   `json:"language,omitempty"  jsonschema:"Optional: filter results by pattern language (e.g. 'go', 'python')"`
	Domain    string   `json:"domain,omitempty"    jsonschema:"Optional: filter results by pattern domain (e.g. 'backend', 'testing')"`
}

// FindRelatedPatternsInput defines the parameters for the find_related_patterns tool.
type FindRelatedPatternsInput struct {
	PatternID string `json:"pattern_id"      jsonschema:"UUID of the pattern to find relations for"`
	Limit     *int   `json:"limit,omitempty" jsonschema:"Maximum number of related patterns to return (default 5, max 20)"`
}

// GetPatternInput defines the parameters for the get_pattern tool.
type GetPatternInput struct {
	ID string `json:"id" jsonschema:"Pattern UUID to fetch"`
}
