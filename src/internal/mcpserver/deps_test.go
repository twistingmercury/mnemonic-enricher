package mcpserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/mcpserver"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/repository/chunk"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/mnemonic/internal/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/service/search"
)

// --- Mock: searchsvc.Service ---

type mockSearchService struct {
	mock.Mock
}

func (m *mockSearchService) SearchPatterns(ctx context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*searchsvc.SearchResult), args.Error(1)
}

// --- Mock: patternsvc.Service ---

type mockPatternService struct {
	mock.Mock
}

func (m *mockPatternService) Create(ctx context.Context, input patternsvc.CreateInput) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) GetWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
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

func (m *mockPatternService) Update(ctx context.Context, id uuid.UUID, input patternsvc.UpdateInput) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternService) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPatternService) List(ctx context.Context, filter patternrepo.Filter, opts patternsvc.ListOptions) ([]*patternrepo.Pattern, int64, error) {
	args := m.Called(ctx, filter, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*patternrepo.Pattern), args.Get(1).(int64), args.Error(2)
}

func (m *mockPatternService) SetAgentAssociations(ctx context.Context, patternID uuid.UUID, associations []patternsvc.AssociationInput) error {
	args := m.Called(ctx, patternID, associations)
	return args.Error(0)
}

func (m *mockPatternService) GetAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]patternrepo.AgentAssociation, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]patternrepo.AgentAssociation), args.Error(1)
}

func (m *mockPatternService) ResolveAgentNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uuid.UUID]string), args.Error(1)
}

func (m *mockPatternService) FindRelated(ctx context.Context, patternID uuid.UUID, limit int) ([]patternsvc.RelatedPatternResult, error) {
	args := m.Called(ctx, patternID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]patternsvc.RelatedPatternResult), args.Error(1)
}

func (m *mockPatternService) ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Chunk), args.Error(1)
}

// --- Tests ---

func TestSearchPatterns_DelegatesToSearchService(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	opts := searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.7,
		Tags:      []string{"golang"},
	}
	expected := &searchsvc.SearchResult{
		Query:            "error handling",
		TotalCandidates:  3,
		SearchDurationMs: 42,
		Matches: []*searchsvc.ChunkMatch{
			{PatternName: "go-error-handling", Similarity: 0.95},
		},
	}

	searchMock.On("SearchPatterns", ctx, opts).Return(expected, nil)

	result, err := deps.SearchPatterns(ctx, opts)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	searchMock.AssertExpectations(t)
	patternMock.AssertNotCalled(t, "FindRelated", mock.Anything, mock.Anything, mock.Anything)
}

func TestSearchPatterns_PropagatesError(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	opts := searchsvc.SearchOptions{Query: "anything"}
	expectedErr := errors.New("embedding service unavailable")

	searchMock.On("SearchPatterns", ctx, opts).Return(nil, expectedErr)

	result, err := deps.SearchPatterns(ctx, opts)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedErr, err)
	searchMock.AssertExpectations(t)
}

func TestFindRelatedPatterns_DelegatesToPatternService(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	patternID := uuid.New()
	limit := 5
	expected := []patternsvc.RelatedPatternResult{
		{
			ID:             uuid.New(),
			Name:           "related-pattern",
			Relationship:   "RELATED_TO",
			Similarity:     0.85,
			SharedConcepts: []string{"concurrency"},
		},
	}

	patternMock.On("FindRelated", ctx, patternID, limit).Return(expected, nil)

	result, err := deps.FindRelatedPatterns(ctx, patternID, limit)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	patternMock.AssertExpectations(t)
	searchMock.AssertNotCalled(t, "SearchPatterns", mock.Anything, mock.Anything)
}

func TestFindRelatedPatterns_PropagatesError(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	patternID := uuid.New()
	expectedErr := errors.New("pattern not found")

	patternMock.On("FindRelated", ctx, patternID, 5).Return(nil, expectedErr)

	result, err := deps.FindRelatedPatterns(ctx, patternID, 5)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedErr, err)
	patternMock.AssertExpectations(t)
}

func TestGetPatternWithGraph_DelegatesToPatternService(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	patternID := uuid.New()
	expectedPattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "test-pattern",
		Content:          "test content",
		EnrichmentStatus: "enriched",
	}
	expectedGraph := &patternsvc.GraphContext{
		RelatedPatterns: []patternsvc.RelatedPatternResult{
			{ID: uuid.New(), Name: "related", Relationship: "RELATED_TO", Similarity: 0.9},
		},
		Concepts: []patternsvc.ConceptResult{
			{Name: "concurrency", Type: "topic"},
		},
	}

	patternMock.On("GetWithGraph", ctx, patternID).Return(expectedPattern, expectedGraph, nil)

	p, gc, err := deps.GetPatternWithGraph(ctx, patternID)

	require.NoError(t, err)
	assert.Equal(t, expectedPattern, p)
	assert.Equal(t, expectedGraph, gc)
	patternMock.AssertExpectations(t)
	searchMock.AssertNotCalled(t, "SearchPatterns", mock.Anything, mock.Anything)
}

func TestGetPatternWithGraph_NilGraphContext(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	patternID := uuid.New()
	expectedPattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "pending-pattern",
		EnrichmentStatus: "pending",
	}

	patternMock.On("GetWithGraph", ctx, patternID).Return(expectedPattern, nil, nil)

	p, gc, err := deps.GetPatternWithGraph(ctx, patternID)

	require.NoError(t, err)
	assert.Equal(t, expectedPattern, p)
	assert.Nil(t, gc)
	patternMock.AssertExpectations(t)
}

func TestGetPatternWithGraph_PropagatesError(t *testing.T) {
	t.Parallel()

	searchMock := new(mockSearchService)
	patternMock := new(mockPatternService)
	deps := mcpserver.NewToolDependencies(searchMock, patternMock)

	ctx := context.Background()
	patternID := uuid.New()
	expectedErr := errors.New("pattern not found")

	patternMock.On("GetWithGraph", ctx, patternID).Return(nil, nil, expectedErr)

	p, gc, err := deps.GetPatternWithGraph(ctx, patternID)

	require.Error(t, err)
	assert.Nil(t, p)
	assert.Nil(t, gc)
	assert.Equal(t, expectedErr, err)
	patternMock.AssertExpectations(t)
}
