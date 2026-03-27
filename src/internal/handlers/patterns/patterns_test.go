package patterns_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic-enricher/internal/config"
	"github.com/twistingmercury/mnemonic-enricher/internal/handlers/patterns"
	chunkrepo "github.com/twistingmercury/mnemonic-enricher/internal/repository/chunk"
	patternrepo "github.com/twistingmercury/mnemonic-enricher/internal/repository/pattern"
	"github.com/twistingmercury/mnemonic-enricher/internal/service"
	patternsvc "github.com/twistingmercury/mnemonic-enricher/internal/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic-enricher/internal/service/search"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Mock PatternService ---

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
	var g *patternsvc.GraphContext
	if args.Get(0) != nil {
		p = args.Get(0).(*patternrepo.Pattern)
	}
	if args.Get(1) != nil {
		g = args.Get(1).(*patternsvc.GraphContext)
	}
	return p, g, args.Error(2)
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

// --- Mock SearchService ---

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

// --- Helpers ---

// testVocab contains the canonical vocabulary used across tests.
var testVocab = config.VocabularyConfig{
	Languages: []string{"agnostic", "go", "python", "csharp", "shell", "typescript", "javascript", "sql", "cypher"},
	Domains:   []string{"api-design", "backend", "frontend", "testing", "devops", "cli", "data-design", "documentation"},
}

func newTestRouter(psvc patternsvc.Service, ssvc searchsvc.Service) *gin.Engine {
	router := gin.New()
	h := patterns.New(psvc, ssvc, testVocab)
	v1 := router.Group("/v1/api")
	h.RegisterRoutes(v1)
	return router
}

func makePattern(name string) *patternrepo.Pattern {
	desc := "Test pattern description"
	return &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             name,
		Description:      &desc,
		Content:          "# Test Pattern\n\nContent here",
		Tags:             []string{"go", "test"},
		EntityType:       "go-pattern",
		Language:         "go",
		Domain:           "backend",
		RelatedPatterns:  []string{},
		EnrichmentStatus: "pending",
		CreatedAt:        time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC),
	}
}

// --- Tests ---

func TestPatternCreate_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("Create", mock.Anything, mock.AnythingOfType("pattern.CreateInput")).Return(pattern, nil)

	body := `{
		"name": "go-error-handling",
		"description": "Test pattern description",
		"content": "# Test Pattern\n\nContent here",
		"tags": ["go", "test"],
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/v1/api/patterns/")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])
	assert.Equal(t, "pending", resp["enrichment_status"])
}

func TestPatternCreate_Conflict(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	psvc.On("Create", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, "go-error-handling"))

	body := `{
		"name": "go-error-handling",
		"content": "content",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestPatternCreate_BadRequest(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	// Missing required content field.
	body := `{"name": "test"}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPatternCreate_InvalidEntityType(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "Invalid Type",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPatternCreate_InvalidLanguage(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "go-pattern",
		"language": "INVALID_LANG",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPatternCreate_InvalidLanguageValue(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "go-pattern",
		"language": "brainfuck",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errs := resp["errors"].([]any)
	require.NotEmpty(t, errs)
	first := errs[0].(map[string]any)
	assert.Equal(t, "language", first["field"])
	assert.Equal(t, "INVALID_VALUE", first["code"])
}

func TestPatternCreate_InvalidDomainValue(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "not-a-valid-domain"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/patterns", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errs := resp["errors"].([]any)
	require.NotEmpty(t, errs)
	first := errs[0].(map[string]any)
	assert.Equal(t, "domain", first["field"])
	assert.Equal(t, "INVALID_VALUE", first["code"])
}

func TestPatternUpdate_InvalidLanguageValue(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "go-pattern",
		"language": "brainfuck",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/patterns/"+id.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errs := resp["errors"].([]any)
	require.NotEmpty(t, errs)
	first := errs[0].(map[string]any)
	assert.Equal(t, "language", first["field"])
	assert.Equal(t, "INVALID_VALUE", first["code"])
}

func TestPatternUpdate_InvalidDomainValue(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()

	body := `{
		"name": "go-error-handling",
		"content": "# Test Pattern\n\nContent here",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "not-a-valid-domain"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/patterns/"+id.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errs := resp["errors"].([]any)
	require.NotEmpty(t, errs)
	first := errs[0].(map[string]any)
	assert.Equal(t, "domain", first["field"])
	assert.Equal(t, "INVALID_VALUE", first["code"])
}

func TestPatternGet_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("GetWithGraph", mock.Anything, pattern.ID).Return(pattern, (*patternsvc.GraphContext)(nil), nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).
		Return([]*chunkrepo.Chunk{}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String(), nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])
}

func TestPatternGet_InvalidUUID(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/not-a-uuid", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPatternGet_NotFound(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("GetWithGraph", mock.Anything, id).
		Return(nil, nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPatternList_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	p1 := makePattern("pattern-a")
	p2 := makePattern("pattern-b")
	psvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Return([]*patternrepo.Pattern{p1, p2}, int64(2), nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns?limit=20", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]any)
	assert.Len(t, data, 2)
}

func TestPatternUpdate_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	psvc.On("Update", mock.Anything, pattern.ID, mock.Anything).Return(pattern, nil)

	body := `{
		"name": "go-error-handling",
		"content": "Updated content",
		"entity_type": "go-pattern",
		"language": "go",
		"domain": "backend"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/patterns/"+pattern.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())

	psvc.AssertExpectations(t)
}

func TestPatternDelete_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Delete", mock.Anything, id).Return(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestPatternDelete_NotFound(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Delete", mock.Anything, id).
		Return(fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/patterns/"+id.String(), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSearch_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	ssvc.On("SearchPatterns", mock.Anything, mock.AnythingOfType("search.SearchOptions")).
		Return(&searchsvc.SearchResult{
			Matches: []*searchsvc.ChunkMatch{
				{
					PatternID:    uuid.New(),
					PatternName:  "go-error-handling",
					EntityType:   "go-pattern",
					Language:     "go",
					Domain:       "backend",
					Content:      "# Test Pattern\n\nContent here",
					Tags:         []string{"go", "test"},
					SectionTitle: "Philosophy",
					ChunkIndex:   0,
					Similarity:   0.92,
				},
			},
			Query:            "error handling",
			TotalCandidates:  47,
			SearchDurationMs: 23,
		}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search?q=error+handling", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	results := resp["results"].([]any)
	assert.Len(t, results, 1)

	first := results[0].(map[string]any)
	assert.Equal(t, "go-error-handling", first["pattern_name"])
	assert.Equal(t, "go-pattern", first["entity_type"])
	assert.Equal(t, "go", first["language"])
	assert.Equal(t, "backend", first["domain"])

	metadata := resp["metadata"].(map[string]any)
	assert.Equal(t, "error handling", metadata["query"])
}

func TestSearch_MissingQuery(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearch_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	ssvc.On("SearchPatterns", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("%w: embedding service down", service.ErrServiceUnavailable))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/search?q=test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetChunks_NotFoundPattern(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	id := uuid.New()
	psvc.On("Get", mock.Anything, id).
		Return(nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+id.String()+"/chunks", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetChunks_Success(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	chunks := []*chunkrepo.Chunk{
		{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
		{ChunkIndex: 1, SectionTitle: "Details", EnrichmentStatus: "enriched"},
	}
	psvc.On("Get", mock.Anything, pattern.ID).Return(pattern, nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).Return(chunks, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String()+"/chunks", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.InDelta(t, float64(2), resp["count"], 0.001)
	chunkList := resp["chunks"].([]any)
	require.Len(t, chunkList, 2)
	first := chunkList[0].(map[string]any)
	assert.Equal(t, "Overview", first["section_title"])
}

func TestPatternGet_ChunksPopulated(t *testing.T) {
	t.Parallel()
	psvc := new(mockPatternService)
	ssvc := new(mockSearchService)
	router := newTestRouter(psvc, ssvc)

	pattern := makePattern("go-error-handling")
	chunks := []*chunkrepo.Chunk{
		{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
		{ChunkIndex: 1, SectionTitle: "Philosophy", EnrichmentStatus: "enriched"},
	}
	psvc.On("GetWithGraph", mock.Anything, pattern.ID).Return(pattern, (*patternsvc.GraphContext)(nil), nil)
	psvc.On("ListChunks", mock.Anything, pattern.ID).Return(chunks, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/patterns/"+pattern.ID.String(), nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "go-error-handling", resp["name"])

	chunkList, ok := resp["chunks"].([]any)
	require.True(t, ok, "expected chunks to be an array")
	require.Len(t, chunkList, 2)

	first := chunkList[0].(map[string]any)
	assert.Equal(t, "Overview", first["section_title"])
	assert.InDelta(t, float64(0), first["chunk_index"], 0.001)
	assert.Equal(t, "pending", first["enrichment_status"])

	second := chunkList[1].(map[string]any)
	assert.Equal(t, "Philosophy", second["section_title"])
	assert.Equal(t, "enriched", second["enrichment_status"])

	psvc.AssertExpectations(t)
}
