package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	"github.com/twistingmercury/mnemonic/internal/service"
	patternsvc "github.com/twistingmercury/mnemonic/internal/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/service/search"
)

// --- Mock: ToolDependencies ---

type mockToolDeps struct {
	mock.Mock
}

func (m *mockToolDeps) SearchPatterns(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*searchsvc.SearchResult), args.Error(1)
}

func (m *mockToolDeps) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]patternsvc.RelatedPatternResult, error) {
	args := m.Called(ctx, patternID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]patternsvc.RelatedPatternResult), args.Error(1)
}

func (m *mockToolDeps) GetPatternWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
	args := m.Called(ctx, id)
	var p *patternrepo.Pattern
	if args.Get(0) != nil {
		p = args.Get(0).(*patternrepo.Pattern)
	}
	var gc *patternsvc.GraphContext
	if args.Get(1) != nil {
		gc = args.Get(1).(*patternsvc.GraphContext)
	}
	return p, gc, args.Error(2)
}

// noopLogger returns a zerolog.Logger that discards output.
func noopLogger() zerolog.Logger {
	return zerolog.Nop()
}

// extractTextContent extracts the text string from a CallToolResult's Content.
func extractTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, result)
	require.NotEmpty(t, result.Content)
	data, err := result.Content[0].MarshalJSON()
	require.NoError(t, err)
	return string(data)
}

// --- search_patterns handler tests ---

func TestHandleSearchPatterns_HappyPath(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:            "error handling",
		TotalCandidates:  2,
		SearchDurationMs: 42,
		Matches: []*searchsvc.ChunkMatch{
			{PatternID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), PatternName: "go-error-handling", Tags: []string{"go", "errors"}, Content: "Error handling content", Similarity: 0.92},
			{PatternID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), PatternName: "retry-logic", Tags: []string{"go", "resilience"}, Content: "Retry logic content", Similarity: 0.85},
		},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
	}).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "error handling",
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	// 2 sections from 2 distinct patterns.
	assert.Contains(t, text, "Found 2 sections across 2 patterns matching 'error handling'")
	assert.Contains(t, text, "go-error-handling (92% match)")
	assert.Contains(t, text, "retry-logic (85% match)")
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_NoResults(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:   "nonexistent topic",
		Matches: []*searchsvc.ChunkMatch{},
	}
	deps.On("SearchPatterns", mock.Anything, mock.Anything).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "nonexistent topic",
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "No patterns found matching 'nonexistent topic'")
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_WithAgentFilter(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query: "testing",
		Matches: []*searchsvc.ChunkMatch{
			{PatternName: "test-pattern", Content: "Test content", Similarity: 0.88},
		},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "testing",
		Limit:     10,
		Threshold: 0.5,
		AgentName: "go-engineer",
	}).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "testing",
		Agent: "go-engineer",
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "filtered by agent: go-engineer")
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	deps.On("SearchPatterns", mock.Anything, mock.Anything).
		Return(nil, errors.Join(service.ErrServiceUnavailable, errors.New("embedding failed")))

	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "anything",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceUnavailable)
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_InvalidLimit(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	limit := 100
	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "anything",
		Limit: &limit,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "limit must be between 1 and 50")
}

func TestHandleSearchPatterns_InvalidThreshold(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	threshold := 1.5
	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "anything",
		Threshold: &threshold,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "threshold must be between 0.0 and 1.0")
}

func TestHandleSearchPatterns_CustomLimitAndThreshold(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:   "go patterns",
		Matches: []*searchsvc.ChunkMatch{},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "go patterns",
		Limit:     5,
		Threshold: 0.9,
	}).Return(searchResult, nil)

	limit := 5
	threshold := 0.9
	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "go patterns",
		Limit:     &limit,
		Threshold: &threshold,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_WithTags(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:   "error handling",
		Matches: []*searchsvc.ChunkMatch{},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
		Tags:      []string{"go", "errors"},
	}).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "error handling",
		Tags:  []string{"go", "errors"},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_WithLanguageFilter(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:   "error handling",
		Matches: []*searchsvc.ChunkMatch{},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
		Language:  "python",
	}).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:    "error handling",
		Language: "python",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_WithDomainFilter(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	searchResult := &searchsvc.SearchResult{
		Query:   "error handling",
		Matches: []*searchsvc.ChunkMatch{},
	}
	deps.On("SearchPatterns", mock.Anything, searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
		Domain:    "backend",
	}).Return(searchResult, nil)

	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:  "error handling",
		Domain: "backend",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	deps.AssertExpectations(t)
}

func TestHandleSearchPatterns_ZeroLimit(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	limit := 0
	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query: "anything",
		Limit: &limit,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestHandleSearchPatterns_NegativeThreshold(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleSearchPatterns(deps, noopLogger(), 0.5)

	threshold := -0.1
	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "anything",
		Threshold: &threshold,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

// --- find_related_patterns handler tests ---

func TestHandleFindRelatedPatterns_HappyPath(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	patternID := uuid.New()
	relatedID := uuid.New()

	relatedResults := []patternsvc.RelatedPatternResult{
		{
			ID:             relatedID,
			Name:           "related-pattern",
			Relationship:   "RELATED_TO",
			Similarity:     0.85,
			SharedConcepts: []string{"error handling", "retry logic"},
		},
	}
	sourcePattern := &patternrepo.Pattern{
		ID:   patternID,
		Name: "source-pattern",
	}

	deps.On("FindRelatedPatterns", mock.Anything, patternID, 5).Return(relatedResults, nil)
	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(sourcePattern, nil, nil)

	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "Found 1 patterns related to 'source-pattern'")
	assert.Contains(t, text, "related-pattern (similarity: 0.85)")
	assert.Contains(t, text, "error handling, retry logic")
	deps.AssertExpectations(t)
}

func TestHandleFindRelatedPatterns_NotFound(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	patternID := uuid.New()

	deps.On("FindRelatedPatterns", mock.Anything, patternID, 5).
		Return(nil, errors.Join(service.ErrNotFound, errors.New("pattern not found")))

	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatternNotFound)
	deps.AssertExpectations(t)
}

func TestHandleFindRelatedPatterns_EmptyResults(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	patternID := uuid.New()
	sourcePattern := &patternrepo.Pattern{
		ID:   patternID,
		Name: "lonely-pattern",
	}

	deps.On("FindRelatedPatterns", mock.Anything, patternID, 5).
		Return([]patternsvc.RelatedPatternResult{}, nil)
	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(sourcePattern, nil, nil)

	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "No related patterns found for 'lonely-pattern'")
	deps.AssertExpectations(t)
}

func TestHandleFindRelatedPatterns_InvalidUUID(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: "not-a-uuid",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "invalid UUID")
}

func TestHandleFindRelatedPatterns_InvalidLimit(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	limit := 25
	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: uuid.New().String(),
		Limit:     &limit,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "limit must be between 1 and 20")
}

func TestHandleFindRelatedPatterns_CustomLimit(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	patternID := uuid.New()
	sourcePattern := &patternrepo.Pattern{
		ID:   patternID,
		Name: "test-pattern",
	}

	deps.On("FindRelatedPatterns", mock.Anything, patternID, 10).
		Return([]patternsvc.RelatedPatternResult{}, nil)
	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(sourcePattern, nil, nil)

	limit := 10
	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
		Limit:     &limit,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	deps.AssertExpectations(t)
}

func TestHandleFindRelatedPatterns_NameLookupFallsBackToUUID(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleFindRelatedPatterns(deps, noopLogger())

	patternID := uuid.New()

	deps.On("FindRelatedPatterns", mock.Anything, patternID, 5).
		Return([]patternsvc.RelatedPatternResult{}, nil)
	// Name lookup fails -- handler should fall back to UUID.
	deps.On("GetPatternWithGraph", mock.Anything, patternID).
		Return(nil, nil, errors.New("lookup failed"))

	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, patternID.String())
	deps.AssertExpectations(t)
}

// --- get_pattern handler tests ---

func TestHandleGetPattern_HappyPathWithGraph(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleGetPattern(deps, noopLogger())

	patternID := uuid.New()
	enrichedAt := time.Date(2025, 1, 10, 8, 5, 0, 0, time.UTC)
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "go-error-handling",
		Tags:             []string{"go", "errors", "best-practices"},
		Content:          "Full pattern content here",
		EnrichmentStatus: "enriched",
		EnrichedAt:       &enrichedAt,
	}
	graphCtx := &patternsvc.GraphContext{
		RelatedPatterns: []patternsvc.RelatedPatternResult{
			{Name: "related-pattern", Relationship: "RELATED_TO", Similarity: 0.85},
		},
		Concepts: []patternsvc.ConceptResult{
			{Name: "error-wrapping", Type: "practice"},
		},
	}

	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(pattern, graphCtx, nil)

	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "go-error-handling")
	assert.Contains(t, text, patternID.String())
	assert.Contains(t, text, "go, errors, best-practices")
	assert.Contains(t, text, "enriched (2025-01-10T08:05:00Z)")
	assert.Contains(t, text, "Full pattern content here")
	assert.Contains(t, text, "Related Patterns")
	assert.Contains(t, text, "related-pattern")
	assert.Contains(t, text, "Extracted Concepts")
	assert.Contains(t, text, "error-wrapping")
	deps.AssertExpectations(t)
}

func TestHandleGetPattern_PendingEnrichment(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleGetPattern(deps, noopLogger())

	patternID := uuid.New()
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "pending-pattern",
		Content:          "Some content",
		EnrichmentStatus: "pending",
	}

	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(pattern, nil, nil)

	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "pending-pattern")
	assert.Contains(t, text, "pending")
	assert.NotContains(t, text, "Related Patterns")
	assert.NotContains(t, text, "Extracted Concepts")
	deps.AssertExpectations(t)
}

func TestHandleGetPattern_FailedEnrichment(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleGetPattern(deps, noopLogger())

	patternID := uuid.New()
	errMsg := "embedding generation timed out after 30s"
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "failed-pattern",
		Content:          "Some content",
		EnrichmentStatus: "failed",
		EnrichmentError:  &errMsg,
	}

	deps.On("GetPatternWithGraph", mock.Anything, patternID).Return(pattern, nil, nil)

	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "failed -- embedding generation timed out after 30s")
	assert.NotContains(t, text, "Related Patterns")
	assert.NotContains(t, text, "Extracted Concepts")
	deps.AssertExpectations(t)
}

func TestHandleGetPattern_NotFound(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleGetPattern(deps, noopLogger())

	patternID := uuid.New()

	deps.On("GetPatternWithGraph", mock.Anything, patternID).
		Return(nil, nil, errors.Join(service.ErrNotFound, errors.New("pattern not found")))

	_, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatternNotFound)
	deps.AssertExpectations(t)
}

func TestHandleGetPattern_InvalidUUID(t *testing.T) {
	t.Parallel()

	deps := new(mockToolDeps)
	handler := handleGetPattern(deps, noopLogger())

	_, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: "bad-uuid",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "invalid UUID")
}

// --- mapServiceError tests ---

func TestMapServiceError_NotFound(t *testing.T) {
	t.Parallel()

	err := mapServiceError(errors.Join(service.ErrNotFound, errors.New("pattern 123")))
	assert.ErrorIs(t, err, ErrPatternNotFound)
}

func TestMapServiceError_InvalidInput(t *testing.T) {
	t.Parallel()

	err := mapServiceError(errors.Join(service.ErrInvalidInput, errors.New("bad data")))
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestMapServiceError_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	err := mapServiceError(errors.Join(service.ErrServiceUnavailable, errors.New("postgres down")))
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

func TestMapServiceError_UnknownError(t *testing.T) {
	t.Parallel()

	err := mapServiceError(errors.New("unexpected"))
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

// --- truncate tests ---

func TestTruncate_ShortString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", truncate("hello", 10))
}

func TestTruncate_LongString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello...", truncate("hello world", 5))
}

func TestTruncate_ExactLength(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", truncate("hello", 5))
}
