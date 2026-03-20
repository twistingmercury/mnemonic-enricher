package pattern_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/repository/chunk"
	enrichmentrepo "github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/mnemonic/internal/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	"github.com/twistingmercury/mnemonic/internal/service"
	patternsvc "github.com/twistingmercury/mnemonic/internal/service/pattern"
)

// ---------- Mock: patternrepo.Repository ----------

type mockPatternRepo struct {
	mock.Mock
}

func (m *mockPatternRepo) Create(ctx context.Context, pattern *patternrepo.Pattern) error {
	args := m.Called(ctx, pattern)
	return args.Error(0)
}

func (m *mockPatternRepo) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternRepo) GetByName(ctx context.Context, name string) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternRepo) Update(ctx context.Context, pattern *patternrepo.Pattern) error {
	args := m.Called(ctx, pattern)
	return args.Error(0)
}

func (m *mockPatternRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPatternRepo) List(ctx context.Context, filter patternrepo.Filter, opts repository.ListOptions) ([]*patternrepo.Pattern, int64, error) {
	args := m.Called(ctx, filter, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*patternrepo.Pattern), args.Get(1).(int64), args.Error(2)
}

func (m *mockPatternRepo) UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error {
	args := m.Called(ctx, id, embedding)
	return args.Error(0)
}

func (m *mockPatternRepo) UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	args := m.Called(ctx, id, status, errMsg)
	return args.Error(0)
}

func (m *mockPatternRepo) FindSimilar(ctx context.Context, embedding []float32, opts patternrepo.SimilarityOptions) ([]*patternrepo.Match, error) {
	args := m.Called(ctx, embedding, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*patternrepo.Match), args.Error(1)
}

func (m *mockPatternRepo) SetAgentAssociations(ctx context.Context, patternID uuid.UUID, associations []patternrepo.AgentAssociation) error {
	args := m.Called(ctx, patternID, associations)
	return args.Error(0)
}

func (m *mockPatternRepo) GetAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]patternrepo.AgentAssociation, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]patternrepo.AgentAssociation), args.Error(1)
}

func (m *mockPatternRepo) GetPatternIDsByAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]uuid.UUID), args.Error(1)
}

func (m *mockPatternRepo) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

// ---------- Mock: enrichmentrepo.Repository ----------

type mockEnrichmentRepo struct {
	mock.Mock
}

func (m *mockEnrichmentRepo) Create(ctx context.Context, job *enrichmentrepo.Job) error {
	args := m.Called(ctx, job)
	return args.Error(0)
}

func (m *mockEnrichmentRepo) Get(ctx context.Context, id uuid.UUID) (*enrichmentrepo.Job, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*enrichmentrepo.Job), args.Error(1)
}

func (m *mockEnrichmentRepo) GetByPatternID(ctx context.Context, patternID uuid.UUID) (*enrichmentrepo.Job, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*enrichmentrepo.Job), args.Error(1)
}

func (m *mockEnrichmentRepo) ClaimPending(ctx context.Context) (*enrichmentrepo.Job, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*enrichmentrepo.Job), args.Error(1)
}

func (m *mockEnrichmentRepo) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockEnrichmentRepo) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockEnrichmentRepo) MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error {
	args := m.Called(ctx, id, err, retryDelay)
	return args.Error(0)
}

func (m *mockEnrichmentRepo) ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error) {
	args := m.Called(ctx, timeout)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockEnrichmentRepo) List(ctx context.Context, filter enrichmentrepo.Filter, opts repository.ListOptions) ([]*enrichmentrepo.Job, int64, error) {
	args := m.Called(ctx, filter, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*enrichmentrepo.Job), args.Get(1).(int64), args.Error(2)
}

func (m *mockEnrichmentRepo) DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error) {
	args := m.Called(ctx, retention)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockEnrichmentRepo) DeleteFailed(ctx context.Context, retention time.Duration) (int64, error) {
	args := m.Called(ctx, retention)
	return args.Get(0).(int64), args.Error(1)
}

// ---------- Mock: graphrepo.Repository ----------

type mockGraphRepo struct {
	mock.Mock
}

func (m *mockGraphRepo) SyncAgent(ctx context.Context, agentName string) error {
	args := m.Called(ctx, agentName)
	return args.Error(0)
}

func (m *mockGraphRepo) DeleteAgent(ctx context.Context, agentName string) error {
	args := m.Called(ctx, agentName)
	return args.Error(0)
}

func (m *mockGraphRepo) SyncPattern(ctx context.Context, pattern *graphrepo.Pattern) error {
	args := m.Called(ctx, pattern)
	return args.Error(0)
}

func (m *mockGraphRepo) DeletePattern(ctx context.Context, patternID uuid.UUID) error {
	args := m.Called(ctx, patternID)
	return args.Error(0)
}

func (m *mockGraphRepo) SyncConcepts(ctx context.Context, patternID uuid.UUID, concepts []graphrepo.Concept) error {
	args := m.Called(ctx, patternID, concepts)
	return args.Error(0)
}

func (m *mockGraphRepo) SetPatternAgentRelevance(ctx context.Context, patternID uuid.UUID, associations []graphrepo.AgentAssociation) error {
	args := m.Called(ctx, patternID, associations)
	return args.Error(0)
}

func (m *mockGraphRepo) ComputeRelatedToEdges(ctx context.Context, patternID uuid.UUID, minSimilarity float64) error {
	args := m.Called(ctx, patternID, minSimilarity)
	return args.Error(0)
}

func (m *mockGraphRepo) GetPatternConcepts(ctx context.Context, patternID uuid.UUID) ([]graphrepo.Concept, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]graphrepo.Concept), args.Error(1)
}

func (m *mockGraphRepo) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]graphrepo.RelatedPattern, error) {
	args := m.Called(ctx, patternID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]graphrepo.RelatedPattern), args.Error(1)
}

func (m *mockGraphRepo) FindPatternsByAgent(ctx context.Context, agentName string, limit int) ([]graphrepo.PatternRelevance, error) {
	args := m.Called(ctx, agentName, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]graphrepo.PatternRelevance), args.Error(1)
}

func (m *mockGraphRepo) CleanupOrphanedConcepts(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockGraphRepo) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// ---------- Mock: agentrepo.Repository ----------

type mockAgentRepo struct {
	mock.Mock
}

func (m *mockAgentRepo) Create(ctx context.Context, agent *agentrepo.Agent) error {
	args := m.Called(ctx, agent)
	return args.Error(0)
}

func (m *mockAgentRepo) Get(ctx context.Context, name string) (*agentrepo.Agent, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*agentrepo.Agent), args.Error(1)
}

func (m *mockAgentRepo) GetByID(ctx context.Context, id uuid.UUID) (*agentrepo.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*agentrepo.Agent), args.Error(1)
}

func (m *mockAgentRepo) Update(ctx context.Context, agent *agentrepo.Agent) error {
	args := m.Called(ctx, agent)
	return args.Error(0)
}

func (m *mockAgentRepo) Delete(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockAgentRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockAgentRepo) List(ctx context.Context, opts repository.ListOptions) ([]*agentrepo.Agent, int64, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*agentrepo.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *mockAgentRepo) Exists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func (m *mockAgentRepo) GetManifest(ctx context.Context) ([]agentrepo.ManifestEntry, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]agentrepo.ManifestEntry), args.Error(1)
}

// ---------- Mock: repository.TxBeginner ----------

type mockTxBeginner struct {
	mock.Mock
}

func (m *mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

// ---------- Mock: pgx.Tx ----------

// mockPgxTx implements pgx.Tx for unit-testing the transaction lifecycle
// (Begin/Commit/Rollback) inside the service layer. The DBTX methods (Exec,
// Query, QueryRow) are never invoked during service-layer tests because mock
// repository implementations intercept all data-access calls before they
// reach the underlying connection; those methods therefore panic to catch
// accidental use.
type mockPgxTx struct {
	mock.Mock
}

func (m *mockPgxTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("mockPgxTx.Begin called unexpectedly")
}

func (m *mockPgxTx) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockPgxTx) Rollback(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockPgxTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	panic("mockPgxTx.Exec called unexpectedly")
}

func (m *mockPgxTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	panic("mockPgxTx.Query called unexpectedly")
}

func (m *mockPgxTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	panic("mockPgxTx.QueryRow called unexpectedly")
}

func (m *mockPgxTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("mockPgxTx.CopyFrom called unexpectedly")
}

func (m *mockPgxTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("mockPgxTx.SendBatch called unexpectedly")
}

func (m *mockPgxTx) LargeObjects() pgx.LargeObjects {
	panic("mockPgxTx.LargeObjects called unexpectedly")
}

func (m *mockPgxTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("mockPgxTx.Prepare called unexpectedly")
}

func (m *mockPgxTx) Conn() *pgx.Conn {
	panic("mockPgxTx.Conn called unexpectedly")
}

// ---------- Mock: chunkrepo.Repository ----------

type mockChunkRepo struct {
	mock.Mock
}

func (m *mockChunkRepo) Create(ctx context.Context, c *chunkrepo.Chunk) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}

func (m *mockChunkRepo) CreateBatch(ctx context.Context, chunks []*chunkrepo.Chunk) error {
	args := m.Called(ctx, chunks)
	// Assign IDs to chunks so that enrichment job creation can use them.
	if args.Error(0) == nil {
		for _, c := range chunks {
			if c.ID == (uuid.UUID{}) {
				c.ID = uuid.New()
			}
		}
	}
	return args.Error(0)
}

func (m *mockChunkRepo) Get(ctx context.Context, id uuid.UUID) (*chunkrepo.Chunk, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*chunkrepo.Chunk), args.Error(1)
}

func (m *mockChunkRepo) ListByPatternID(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Chunk), args.Error(1)
}

func (m *mockChunkRepo) DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error {
	args := m.Called(ctx, patternID)
	return args.Error(0)
}

func (m *mockChunkRepo) UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error {
	args := m.Called(ctx, id, embedding)
	return args.Error(0)
}

func (m *mockChunkRepo) UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	args := m.Called(ctx, id, status, errMsg)
	return args.Error(0)
}

func (m *mockChunkRepo) FindSimilar(ctx context.Context, embedding []float32, opts chunkrepo.SimilarityOptions) ([]*chunkrepo.Match, error) {
	args := m.Called(ctx, embedding, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Match), args.Error(1)
}

func (m *mockChunkRepo) AllEnrichedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error) {
	args := m.Called(ctx, patternID)
	return args.Bool(0), args.Error(1)
}

func (m *mockChunkRepo) AnyFailedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error) {
	args := m.Called(ctx, patternID)
	return args.Bool(0), args.Error(1)
}

// ---------- Helpers ----------

var (
	testPatternID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testAgentID   = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testAgent2ID  = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	testRelatedID = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

func newTestService(
	pr *mockPatternRepo,
	er *mockEnrichmentRepo,
	gr *mockGraphRepo,
	ar *mockAgentRepo,
	tb *mockTxBeginner,
) patternsvc.Service {
	logger := zerolog.Nop()
	// chunkRepo is nil: chunk creation is skipped during the transitional period.
	return patternsvc.New(pr, er, gr, ar, tb, nil, logger)
}

func newTestServiceWithChunkRepo(
	pr *mockPatternRepo,
	er *mockEnrichmentRepo,
	gr *mockGraphRepo,
	ar *mockAgentRepo,
	tb *mockTxBeginner,
	cr *mockChunkRepo,
) patternsvc.Service {
	logger := zerolog.Nop()
	return patternsvc.New(pr, er, gr, ar, tb, cr, logger)
}

func newTestServiceWithChunkRepoAndLogger(
	pr *mockPatternRepo,
	er *mockEnrichmentRepo,
	gr *mockGraphRepo,
	ar *mockAgentRepo,
	tb *mockTxBeginner,
	cr *mockChunkRepo,
	logger zerolog.Logger,
) patternsvc.Service {
	return patternsvc.New(pr, er, gr, ar, tb, cr, logger)
}

func testCreateInput() patternsvc.CreateInput {
	desc := "A test pattern"
	return patternsvc.CreateInput{
		Name:        "go-error-handling",
		Description: &desc,
		Content:     "Always handle errors explicitly.",
		Tags:        []string{"golang", "best-practices"},
		AgentAssociations: []patternsvc.AssociationInput{
			{AgentName: "code-reviewer", Relevance: 0.9},
		},
	}
}

func testUpdateInput() patternsvc.UpdateInput {
	desc := "Updated description"
	return patternsvc.UpdateInput{
		Name:        "go-error-handling-v2",
		Description: &desc,
		Content:     "Updated content.",
		Tags:        []string{"golang", "errors"},
		AgentAssociations: []patternsvc.AssociationInput{
			{AgentName: "code-reviewer", Relevance: 0.8},
		},
	}
}

func testPattern() *patternrepo.Pattern {
	desc := "A test pattern"
	return &patternrepo.Pattern{
		ID:               testPatternID,
		Name:             "go-error-handling",
		Description:      &desc,
		Content:          "Always handle errors explicitly.",
		Tags:             []string{"golang", "best-practices"},
		EnrichmentStatus: "pending",
	}
}

func enrichedPattern() *patternrepo.Pattern {
	p := testPattern()
	p.EnrichmentStatus = "enriched"
	return p
}

// ---------- Create ----------

func TestCreate(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		// Agent resolution.
		ar.On("Get", mock.Anything, "code-reviewer").Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)

		// Pattern creation.
		pr.On("Create", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.Name == "go-error-handling" && p.Content == "Always handle errors explicitly."
		})).Run(func(args mock.Arguments) {
			p := args.Get(1).(*patternrepo.Pattern)
			p.ID = testPatternID
			p.EnrichmentStatus = "pending"
		}).Return(nil)

		// Agent associations.
		pr.On("SetAgentAssociations", mock.Anything, testPatternID, mock.MatchedBy(func(assocs []patternrepo.AgentAssociation) bool {
			return len(assocs) == 1 && assocs[0].AgentID == testAgentID && assocs[0].Relevance == 0.9
		})).Return(nil)

		// Enrichment job.
		er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
			return j.PatternID != nil && *j.PatternID == testPatternID && j.Status == "pending"
		})).Return(nil)

		// Neo4j sync.
		gr.On("SetPatternAgentRelevance", mock.Anything, testPatternID, mock.MatchedBy(func(assocs []graphrepo.AgentAssociation) bool {
			return len(assocs) == 1 && assocs[0].AgentName == "code-reviewer" && assocs[0].Relevance == 0.9
		})).Return(nil)

		result, err := svc.Create(context.Background(), testCreateInput())

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "go-error-handling", result.Name)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "pending", result.EnrichmentStatus)

		pr.AssertExpectations(t)
		er.AssertExpectations(t)
		gr.AssertExpectations(t)
		ar.AssertExpectations(t)
	})

	t.Run("agent not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("Get", mock.Anything, "code-reviewer").Return(nil, agentrepo.ErrNotFound)

		result, err := svc.Create(context.Background(), testCreateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)
		assert.Contains(t, err.Error(), "code-reviewer")

		pr.AssertNotCalled(t, "Create")
	})

	t.Run("pattern name conflict returns service.ErrConflict", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("Get", mock.Anything, "code-reviewer").Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)
		pr.On("Create", mock.Anything, mock.Anything).Return(patternrepo.ErrNameExists)

		result, err := svc.Create(context.Background(), testCreateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrConflict), "expected service.ErrConflict, got: %v", err)

		er.AssertNotCalled(t, "Create")
	})

	t.Run("neo4j failure logged but not returned", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("Get", mock.Anything, "code-reviewer").Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)
		pr.On("Create", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			p := args.Get(1).(*patternrepo.Pattern)
			p.ID = testPatternID
		}).Return(nil)
		pr.On("SetAgentAssociations", mock.Anything, testPatternID, mock.Anything).Return(nil)
		er.On("Create", mock.Anything, mock.Anything).Return(nil)
		gr.On("SetPatternAgentRelevance", mock.Anything, testPatternID, mock.Anything).
			Return(errors.New("neo4j unavailable"))

		result, err := svc.Create(context.Background(), testCreateInput())

		require.NoError(t, err, "neo4j failure should not propagate")
		require.NotNil(t, result)
		assert.Equal(t, "go-error-handling", result.Name)

		pr.AssertExpectations(t)
		er.AssertExpectations(t)
		gr.AssertExpectations(t)
	})
}

// ---------- Get ----------

func TestGet(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)

		result, err := svc.Get(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "go-error-handling", result.Name)

		pr.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := svc.Get(context.Background(), testPatternID)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)
	})
}

// ---------- GetWithGraph ----------

func TestGetWithGraph(t *testing.T) {
	t.Parallel()

	t.Run("enriched pattern with graph context", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)

		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 10).Return([]graphrepo.RelatedPattern{
			{
				ID:             testRelatedID,
				Name:           "go-concurrency",
				SharedConcepts: 2,
				Similarity:     0.75,
				ConceptNames:   []string{"goroutines", "channels"},
			},
		}, nil)

		gr.On("GetPatternConcepts", mock.Anything, testPatternID).Return([]graphrepo.Concept{
			{Name: "error-handling", Type: "practice"},
			{Name: "golang", Type: "technology"},
		}, nil)

		pattern, graphCtx, err := svc.GetWithGraph(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, pattern)
		require.NotNil(t, graphCtx)

		assert.Len(t, graphCtx.RelatedPatterns, 1)
		assert.Equal(t, testRelatedID, graphCtx.RelatedPatterns[0].ID)
		assert.Equal(t, "go-concurrency", graphCtx.RelatedPatterns[0].Name)
		assert.Equal(t, "RELATED_TO", graphCtx.RelatedPatterns[0].Relationship)
		assert.InDelta(t, 0.75, graphCtx.RelatedPatterns[0].Similarity, 0.001)
		assert.Equal(t, []string{"goroutines", "channels"}, graphCtx.RelatedPatterns[0].SharedConcepts)

		assert.Len(t, graphCtx.Concepts, 2)
		assert.Equal(t, "error-handling", graphCtx.Concepts[0].Name)
		assert.Equal(t, "practice", graphCtx.Concepts[0].Type)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
	})

	t.Run("pattern not enriched returns nil graph context", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil) // status = "pending"

		pattern, graphCtx, err := svc.GetWithGraph(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, pattern)
		assert.Nil(t, graphCtx)

		gr.AssertNotCalled(t, "FindRelatedPatterns")
		gr.AssertNotCalled(t, "GetPatternConcepts")
	})

	t.Run("neo4j failure returns nil graph context", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)
		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 10).
			Return(nil, errors.New("neo4j connection refused"))

		pattern, graphCtx, err := svc.GetWithGraph(context.Background(), testPatternID)

		require.NoError(t, err, "neo4j failure should degrade gracefully")
		require.NotNil(t, pattern)
		assert.Nil(t, graphCtx)
	})
}

// ---------- Update ----------

func TestUpdate(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		input := testUpdateInput()

		// Get existing.
		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)

		// Agent resolution.
		ar.On("Get", mock.Anything, "code-reviewer").Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)

		// Update.
		pr.On("Update", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.ID == testPatternID && p.Name == "go-error-handling-v2" && p.Content == "Updated content."
		})).Return(nil)

		// Associations.
		pr.On("SetAgentAssociations", mock.Anything, testPatternID, mock.MatchedBy(func(assocs []patternrepo.AgentAssociation) bool {
			return len(assocs) == 1 && assocs[0].AgentID == testAgentID && assocs[0].Relevance == 0.8
		})).Return(nil)

		// Enrichment job.
		er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
			return j.PatternID != nil && *j.PatternID == testPatternID && j.Status == "pending"
		})).Return(nil)

		// Neo4j sync.
		gr.On("SetPatternAgentRelevance", mock.Anything, testPatternID, mock.Anything).Return(nil)

		result, err := svc.Update(context.Background(), testPatternID, input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "go-error-handling-v2", result.Name)

		pr.AssertExpectations(t)
		er.AssertExpectations(t)
		gr.AssertExpectations(t)
		ar.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := svc.Update(context.Background(), testPatternID, testUpdateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		pr.AssertNotCalled(t, "Update")
	})

	t.Run("chunk-aware path deletes stale chunks, creates new chunks, and enqueues per-chunk jobs", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		tx := new(mockPgxTx)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		input := patternsvc.UpdateInput{
			Name:    "go-error-handling-v2",
			Content: "[//]: pattern\n## Section One\nNew content.\n\n[//]: pattern\n## Section Two\nMore new content.",
			Tags:    []string{"golang", "errors"},
		}

		// Transaction lifecycle.
		tb.On("Begin", mock.Anything).Return(tx, nil)
		tx.On("Commit", mock.Anything).Return(nil)
		// Rollback is always called by defer; pgx guarantees it is a no-op
		// after a successful Commit.
		tx.On("Rollback", mock.Anything).Return(nil)

		// Get existing.
		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)

		// Update (no agent associations in input, so no agent resolution needed).
		pr.On("Update", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.ID == testPatternID && p.Name == "go-error-handling-v2"
		})).Return(nil)

		// Chunk-aware path: delete stale chunks.
		cr.On("DeleteByPatternID", mock.Anything, testPatternID).Return(nil)

		// Create new chunks (content has 2 PATTERN sections → 2 chunks).
		cr.On("CreateBatch", mock.Anything, mock.MatchedBy(func(chunks []*chunkrepo.Chunk) bool {
			return len(chunks) == 2
		})).Return(nil)

		// Per-chunk enrichment jobs: expect 2 calls, each with a ChunkID set.
		er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
			return j.ChunkID != nil && j.PatternID == nil && j.Status == enrichmentrepo.StatusPending
		})).Return(nil).Times(2)

		result, err := svc.Update(context.Background(), testPatternID, input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "go-error-handling-v2", result.Name)

		tb.AssertExpectations(t)
		tx.AssertExpectations(t)
		pr.AssertExpectations(t)
		cr.AssertExpectations(t)
		er.AssertExpectations(t)
		gr.AssertNotCalled(t, "SetPatternAgentRelevance")
	})

	t.Run("chunk-aware path: delete stale chunks error rolls back transaction and propagates error", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		tx := new(mockPgxTx)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		input := patternsvc.UpdateInput{
			Name:    "go-error-handling-v2",
			Content: "## Section\nContent.",
		}

		// Transaction lifecycle: Begin succeeds, Rollback is called by defer.
		tb.On("Begin", mock.Anything).Return(tx, nil)
		tx.On("Rollback", mock.Anything).Return(nil)

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		pr.On("Update", mock.Anything, mock.Anything).Return(nil)
		cr.On("DeleteByPatternID", mock.Anything, testPatternID).
			Return(errors.New("delete failed"))

		result, err := svc.Update(context.Background(), testPatternID, input)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete stale chunks")

		tb.AssertExpectations(t)
		tx.AssertExpectations(t)
		tx.AssertNotCalled(t, "Commit")
		cr.AssertNotCalled(t, "CreateBatch")
		er.AssertNotCalled(t, "Create")
	})
}

// ---------- Chunk enrichment job failure summary warnings ----------

func TestCreate_ChunkJobFailuresSummarisedInLog(t *testing.T) {
	t.Parallel()

	pr := new(mockPatternRepo)
	er := new(mockEnrichmentRepo)
	gr := new(mockGraphRepo)
	ar := new(mockAgentRepo)
	tb := new(mockTxBeginner)
	cr := new(mockChunkRepo)

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	svc := newTestServiceWithChunkRepoAndLogger(pr, er, gr, ar, tb, cr, logger)

	input := patternsvc.CreateInput{
		Name:    "go-error-handling",
		Content: "[//]: pattern\n## Section One\nContent one.\n\n[//]: pattern\n## Section Two\nContent two.",
	}

	pr.On("Create", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		p := args.Get(1).(*patternrepo.Pattern)
		p.ID = testPatternID
		p.EnrichmentStatus = "pending"
	}).Return(nil)

	// CreateBatch assigns IDs via the mock.
	cr.On("CreateBatch", mock.Anything, mock.Anything).Return(nil)

	// All enrichment job creations fail.
	jobErr := errors.New("db down")
	er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
		return j.ChunkID != nil
	})).Return(jobErr)

	result, err := svc.Create(context.Background(), input)

	require.NoError(t, err, "job creation failures must not propagate")
	require.NotNil(t, result)

	logs := buf.String()
	assert.Contains(t, logs, "failed to create chunk enrichment jobs")
	assert.Contains(t, logs, `"failed":2`)
	assert.Contains(t, logs, `"total":2`)
}

func TestUpdate_ChunkJobFailuresSummarisedInLog(t *testing.T) {
	t.Parallel()

	pr := new(mockPatternRepo)
	er := new(mockEnrichmentRepo)
	gr := new(mockGraphRepo)
	ar := new(mockAgentRepo)
	tb := new(mockTxBeginner)
	cr := new(mockChunkRepo)
	tx := new(mockPgxTx)

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	svc := newTestServiceWithChunkRepoAndLogger(pr, er, gr, ar, tb, cr, logger)

	input := patternsvc.UpdateInput{
		Name:    "go-error-handling-v2",
		Content: "[//]: pattern\n## Section One\nContent one.\n\n[//]: pattern\n## Section Two\nContent two.",
	}

	tb.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("Commit", mock.Anything).Return(nil)
	tx.On("Rollback", mock.Anything).Return(nil)

	pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	pr.On("Update", mock.Anything, mock.Anything).Return(nil)
	cr.On("DeleteByPatternID", mock.Anything, testPatternID).Return(nil)
	cr.On("CreateBatch", mock.Anything, mock.Anything).Return(nil)

	// All enrichment job creations fail.
	jobErr := errors.New("db down")
	er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
		return j.ChunkID != nil
	})).Return(jobErr)

	result, err := svc.Update(context.Background(), testPatternID, input)

	require.NoError(t, err, "job creation failures must not propagate")
	require.NotNil(t, result)

	logs := buf.String()
	assert.Contains(t, logs, "failed to create chunk enrichment jobs")
	assert.Contains(t, logs, `"failed":2`)
	assert.Contains(t, logs, `"total":2`)
}

// ---------- Delete ----------

func TestDelete(t *testing.T) {
	t.Parallel()

	t.Run("happy path with neo4j cleanup", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Delete", mock.Anything, testPatternID).Return(nil)
		gr.On("DeletePattern", mock.Anything, testPatternID).Return(nil)
		gr.On("CleanupOrphanedConcepts", mock.Anything).Return(int64(2), nil)

		err := svc.Delete(context.Background(), testPatternID)

		require.NoError(t, err)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Delete", mock.Anything, testPatternID).Return(patternrepo.ErrNotFound)

		err := svc.Delete(context.Background(), testPatternID)

		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		gr.AssertNotCalled(t, "DeletePattern")
		gr.AssertNotCalled(t, "CleanupOrphanedConcepts")
	})
}

// ---------- List ----------

func TestList(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		patterns := []*patternrepo.Pattern{
			{ID: testPatternID, Name: "pattern-a"},
			{ID: testRelatedID, Name: "pattern-b"},
		}
		filter := patternrepo.Filter{Tags: []string{"golang"}}

		pr.On("List", mock.Anything, filter, repository.ListOptions{
			Offset: 0,
			Limit:  10,
		}).Return(patterns, int64(2), nil)

		result, total, err := svc.List(context.Background(), filter, patternsvc.ListOptions{
			Offset: 0,
			Limit:  10,
		})

		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, int64(2), total)

		pr.AssertExpectations(t)
	})
}

// ---------- SetAgentAssociations ----------

func TestSetAgentAssociations(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("Get", mock.Anything, "code-reviewer").Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)
		ar.On("Get", mock.Anything, "doc-writer").Return(&agentrepo.Agent{
			ID:   testAgent2ID,
			Name: "doc-writer",
		}, nil)

		pr.On("SetAgentAssociations", mock.Anything, testPatternID, mock.MatchedBy(func(assocs []patternrepo.AgentAssociation) bool {
			return len(assocs) == 2
		})).Return(nil)

		gr.On("SetPatternAgentRelevance", mock.Anything, testPatternID, mock.MatchedBy(func(assocs []graphrepo.AgentAssociation) bool {
			return len(assocs) == 2
		})).Return(nil)

		err := svc.SetAgentAssociations(context.Background(), testPatternID, []patternsvc.AssociationInput{
			{AgentName: "code-reviewer", Relevance: 0.9},
			{AgentName: "doc-writer", Relevance: 0.7},
		})

		require.NoError(t, err)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
		ar.AssertExpectations(t)
	})

	t.Run("agent not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("Get", mock.Anything, "missing-agent").Return(nil, agentrepo.ErrNotFound)

		err := svc.SetAgentAssociations(context.Background(), testPatternID, []patternsvc.AssociationInput{
			{AgentName: "missing-agent", Relevance: 0.5},
		})

		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)
		assert.Contains(t, err.Error(), "missing-agent")

		pr.AssertNotCalled(t, "SetAgentAssociations")
	})
}

// ---------- GetAgentAssociations ----------

func TestGetAgentAssociations(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		existingPattern := &patternrepo.Pattern{ID: testPatternID, Name: "go-error-handling"}
		expected := []patternrepo.AgentAssociation{
			{AgentID: testAgentID, Relevance: 0.9},
			{AgentID: testAgent2ID, Relevance: 0.7},
		}
		pr.On("Get", mock.Anything, testPatternID).Return(existingPattern, nil)
		pr.On("GetAgentAssociations", mock.Anything, testPatternID).Return(expected, nil)

		result, err := svc.GetAgentAssociations(context.Background(), testPatternID)

		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, testAgentID, result[0].AgentID)
		assert.InDelta(t, 0.9, result[0].Relevance, 0.001)

		pr.AssertExpectations(t)
	})

	t.Run("pattern not found returns ErrNotFound", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := svc.GetAgentAssociations(context.Background(), testPatternID)

		require.Error(t, err)
		assert.ErrorIs(t, err, service.ErrNotFound)
		assert.Nil(t, result)

		pr.AssertExpectations(t)
	})
}

// ---------- ResolveAgentNames ----------

func TestResolveAgentNames(t *testing.T) {
	t.Parallel()

	t.Run("happy path resolves all IDs", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("GetByID", mock.Anything, testAgentID).Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)
		ar.On("GetByID", mock.Anything, testAgent2ID).Return(&agentrepo.Agent{
			ID:   testAgent2ID,
			Name: "doc-writer",
		}, nil)

		names, err := svc.ResolveAgentNames(context.Background(), []uuid.UUID{testAgentID, testAgent2ID})

		require.NoError(t, err)
		assert.Len(t, names, 2)
		assert.Equal(t, "code-reviewer", names[testAgentID])
		assert.Equal(t, "doc-writer", names[testAgent2ID])

		ar.AssertExpectations(t)
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		names, err := svc.ResolveAgentNames(context.Background(), []uuid.UUID{})

		require.NoError(t, err)
		assert.Empty(t, names)

		ar.AssertNotCalled(t, "GetByID")
	})

	t.Run("unknown agent ID is omitted silently", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		unknownID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
		ar.On("GetByID", mock.Anything, testAgentID).Return(&agentrepo.Agent{
			ID:   testAgentID,
			Name: "code-reviewer",
		}, nil)
		ar.On("GetByID", mock.Anything, unknownID).Return(nil, agentrepo.ErrNotFound)

		names, err := svc.ResolveAgentNames(context.Background(), []uuid.UUID{testAgentID, unknownID})

		require.NoError(t, err)
		assert.Len(t, names, 1)
		assert.Equal(t, "code-reviewer", names[testAgentID])
		_, exists := names[unknownID]
		assert.False(t, exists)

		ar.AssertExpectations(t)
	})

	t.Run("repository error propagates", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		ar.On("GetByID", mock.Anything, testAgentID).Return(nil, errors.New("db connection lost"))

		names, err := svc.ResolveAgentNames(context.Background(), []uuid.UUID{testAgentID})

		require.Error(t, err)
		assert.Nil(t, names)
		assert.Contains(t, err.Error(), "db connection lost")
	})
}

// ---------- FindRelated ----------

func TestFindRelated(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Exists", mock.Anything, testPatternID).Return(true, nil)

		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 5).Return([]graphrepo.RelatedPattern{
			{
				ID:             testRelatedID,
				Name:           "go-concurrency",
				SharedConcepts: 3,
				Similarity:     0.8,
				ConceptNames:   []string{"goroutines", "channels", "select"},
			},
		}, nil)

		results, err := svc.FindRelated(context.Background(), testPatternID, 5)

		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, testRelatedID, results[0].ID)
		assert.Equal(t, "go-concurrency", results[0].Name)
		assert.Equal(t, "RELATED_TO", results[0].Relationship)
		assert.InDelta(t, 0.8, results[0].Similarity, 0.001)
		assert.Equal(t, []string{"goroutines", "channels", "select"}, results[0].SharedConcepts)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
	})

	t.Run("pattern not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb)

		pr.On("Exists", mock.Anything, testPatternID).Return(false, nil)

		results, err := svc.FindRelated(context.Background(), testPatternID, 5)

		assert.Nil(t, results)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		gr.AssertNotCalled(t, "FindRelatedPatterns")
	})
}

// ---------- TestCreate_ChunksContent ----------

func TestCreate_ChunksContent(t *testing.T) {
	t.Parallel()

	t.Run("chunks are created and per-chunk enrichment jobs are queued", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		desc := "A chunked pattern"
		input := patternsvc.CreateInput{
			Name:        "chunked-pattern",
			Description: &desc,
			// Two decorated sections → 2 chunks.
			Content:    "[//]: pattern\n## Section One\nContent of section one.\n\n[//]: pattern\n## Section Two\nContent of section two.",
			Tags:       []string{"test"},
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}

		// Pattern creation.
		pr.On("Create", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.Name == "chunked-pattern" && p.EntityType == "go-pattern" && p.Language == "go"
		})).Run(func(args mock.Arguments) {
			p := args.Get(1).(*patternrepo.Pattern)
			p.ID = testPatternID
			p.EnrichmentStatus = "pending"
		}).Return(nil)

		// No agent associations (none provided).

		// Chunk batch creation: expect 2 chunks.
		cr.On("CreateBatch", mock.Anything, mock.MatchedBy(func(chunks []*chunkrepo.Chunk) bool {
			return len(chunks) == 2
		})).Return(nil)

		// Per-chunk enrichment jobs: expect 2 calls.
		er.On("Create", mock.Anything, mock.MatchedBy(func(j *enrichmentrepo.Job) bool {
			return j.ChunkID != nil && j.PatternID == nil
		})).Return(nil).Times(2)

		// No Neo4j sync (no agent associations).

		result, err := svc.Create(context.Background(), input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "chunked-pattern", result.Name)
		assert.Equal(t, testPatternID, result.ID)

		pr.AssertExpectations(t)
		cr.AssertExpectations(t)
		er.AssertExpectations(t)
		gr.AssertNotCalled(t, "SetPatternAgentRelevance")
	})
}

// ---------- ListChunks ----------

func TestListChunks(t *testing.T) {
	t.Parallel()

	t.Run("happy path returns chunks for existing pattern", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		expectedChunks := []*chunkrepo.Chunk{
			{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
			{ChunkIndex: 1, SectionTitle: "Philosophy", EnrichmentStatus: "enriched"},
		}
		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		cr.On("ListByPatternID", mock.Anything, testPatternID).Return(expectedChunks, nil)

		result, err := svc.ListChunks(context.Background(), testPatternID)

		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, 0, result[0].ChunkIndex)
		assert.Equal(t, "Overview", result[0].SectionTitle)
		assert.Equal(t, "pending", result[0].EnrichmentStatus)
		assert.Equal(t, 1, result[1].ChunkIndex)
		assert.Equal(t, "Philosophy", result[1].SectionTitle)

		pr.AssertExpectations(t)
		cr.AssertExpectations(t)
	})

	t.Run("pattern not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := svc.ListChunks(context.Background(), testPatternID)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		cr.AssertNotCalled(t, "ListByPatternID")
	})

	t.Run("nil chunk repo returns empty slice", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		svc := newTestService(pr, er, gr, ar, tb) // chunkRepo is nil

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)

		result, err := svc.ListChunks(context.Background(), testPatternID)

		require.NoError(t, err)
		assert.Empty(t, result)

		pr.AssertExpectations(t)
	})

	t.Run("repo error propagates", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		er := new(mockEnrichmentRepo)
		gr := new(mockGraphRepo)
		ar := new(mockAgentRepo)
		tb := new(mockTxBeginner)
		cr := new(mockChunkRepo)
		svc := newTestServiceWithChunkRepo(pr, er, gr, ar, tb, cr)

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		cr.On("ListByPatternID", mock.Anything, testPatternID).Return(nil, errors.New("db error"))

		result, err := svc.ListChunks(context.Background(), testPatternID)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db error")

		pr.AssertExpectations(t)
		cr.AssertExpectations(t)
	})
}
