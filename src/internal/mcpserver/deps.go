// Package mcpserver provides the MCP (Model Context Protocol) server implementation.
// ToolDependencies is a thin facade over the search and pattern services, exposing
// only the three operations required by MCP tool handlers: search, find-related,
// and get-with-graph.
package mcpserver

import (
	"context"

	"github.com/google/uuid"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/mnemonic/internal/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/service/search"
)

// ToolDependencies defines the interface that MCP tool handlers require.
// Each method maps 1:1 to an MCP tool:
//   - SearchPatterns    -> search_patterns tool
//   - FindRelatedPatterns -> find_related_patterns tool
//   - GetPatternWithGraph -> get_pattern tool
type ToolDependencies interface {
	SearchPatterns(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error)
	FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]patternsvc.RelatedPatternResult, error)
	GetPatternWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error)
}

// toolDeps delegates each call to the underlying service.
type toolDeps struct {
	search  searchsvc.Service
	pattern patternsvc.Service
}

// NewToolDependencies creates a ToolDependencies facade backed by the given
// search and pattern services.
func NewToolDependencies(search searchsvc.Service, pattern patternsvc.Service) ToolDependencies {
	return &toolDeps{search: search, pattern: pattern}
}

// SearchPatterns delegates to searchsvc.Service.SearchPatterns.
func (t *toolDeps) SearchPatterns(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	return t.search.SearchPatterns(ctx, opts)
}

// FindRelatedPatterns delegates to patternsvc.Service.FindRelated.
func (t *toolDeps) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]patternsvc.RelatedPatternResult, error) {
	return t.pattern.FindRelated(ctx, patternID, limit)
}

// GetPatternWithGraph delegates to patternsvc.Service.GetWithGraph.
func (t *toolDeps) GetPatternWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
	return t.pattern.GetWithGraph(ctx, id)
}
