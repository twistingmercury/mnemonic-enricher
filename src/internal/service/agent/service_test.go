package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"hash/crc64"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	graphrepo "github.com/twistingmercury/mnemonic/internal/repository/graph"
	"github.com/twistingmercury/mnemonic/internal/service"
	agentsvc "github.com/twistingmercury/mnemonic/internal/service/agent"
)

// --- Mock: agentrepo.Repository ---

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

// --- Mock: graphrepo.Repository ---

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

func (m *mockGraphRepo) SyncPattern(_ context.Context, _ *graphrepo.Pattern) error {
	return nil
}

func (m *mockGraphRepo) DeletePattern(_ context.Context, _ uuid.UUID) error { return nil }

func (m *mockGraphRepo) SyncConcepts(_ context.Context, _ uuid.UUID, _ []graphrepo.Concept) error {
	return nil
}

func (m *mockGraphRepo) SetPatternAgentRelevance(_ context.Context, _ uuid.UUID, _ []graphrepo.AgentAssociation) error {
	return nil
}

func (m *mockGraphRepo) ComputeRelatedToEdges(_ context.Context, _ uuid.UUID, _ float64) error {
	return nil
}

func (m *mockGraphRepo) GetPatternConcepts(_ context.Context, _ uuid.UUID) ([]graphrepo.Concept, error) {
	return nil, nil
}

func (m *mockGraphRepo) FindRelatedPatterns(_ context.Context, _ uuid.UUID, _ int) ([]graphrepo.RelatedPattern, error) {
	return nil, nil
}

func (m *mockGraphRepo) FindPatternsByAgent(_ context.Context, _ string, _ int) ([]graphrepo.PatternRelevance, error) {
	return nil, nil
}

func (m *mockGraphRepo) CleanupOrphanedConcepts(_ context.Context) (int64, error) { return 0, nil }
func (m *mockGraphRepo) HealthCheck(_ context.Context) error                      { return nil }

// --- Helpers ---

func testCreateInput() agentsvc.CreateInput {
	return agentsvc.CreateInput{
		Name:         "test-agent",
		Description:  "A test agent",
		SystemPrompt: "You are helpful.",
		Model:        "sonnet",
		AllowedTools: []string{"Read", "Write"},
		Version:      "1.0.0",
	}
}

func testUpdateInput() agentsvc.UpdateInput {
	return agentsvc.UpdateInput{
		Description:  "Updated description",
		SystemPrompt: "Updated prompt.",
		Model:        "opus",
		AllowedTools: []string{"Read", "Write", "Edit"},
		Version:      "2.0.0",
	}
}

// expectedCRC64 computes the expected CRC64 for a definition JSON.
func expectedCRC64(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	table := crc64.MakeTable(crc64.ISO)
	checksum := crc64.Checksum(data, table)
	return strconv.FormatUint(checksum, 10)
}

// definitionFromInput constructs the agentDefinition equivalent for CRC computation.
type agentDefinition struct {
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Version      string   `json:"version"`
}

func newService(agentRepo *mockAgentRepo, graphRepo *mockGraphRepo) agentsvc.Service {
	logger := zerolog.Nop()
	return agentsvc.New(agentRepo, graphRepo, logger)
}

// ---------- Create ----------

func TestCreate(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		input := testCreateInput()
		def := agentDefinition{
			Description:  input.Description,
			SystemPrompt: input.SystemPrompt,
			Model:        input.Model,
			AllowedTools: input.AllowedTools,
			Version:      input.Version,
		}
		wantCRC := expectedCRC64(t, def)

		agentRepo.On("Create", mock.Anything, mock.MatchedBy(func(a *agentrepo.Agent) bool {
			return a.Name == "test-agent" && a.CRC64 == wantCRC && a.Definition != nil
		})).Return(nil)

		graphRepo.On("SyncAgent", mock.Anything, "test-agent").Return(nil)

		result, err := svc.Create(context.Background(), input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-agent", result.Name)
		assert.Equal(t, wantCRC, result.CRC64)

		// Verify definition JSON structure.
		var parsed agentDefinition
		require.NoError(t, json.Unmarshal(result.Definition, &parsed))
		assert.Equal(t, input.Description, parsed.Description)
		assert.Equal(t, input.SystemPrompt, parsed.SystemPrompt)
		assert.Equal(t, input.Model, parsed.Model)
		assert.Equal(t, input.AllowedTools, parsed.AllowedTools)
		assert.Equal(t, input.Version, parsed.Version)

		agentRepo.AssertExpectations(t)
		graphRepo.AssertExpectations(t)
	})

	t.Run("conflict on duplicate name", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		agentRepo.On("Create", mock.Anything, mock.Anything).Return(agentrepo.ErrExists)

		result, err := svc.Create(context.Background(), testCreateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrConflict), "expected service.ErrConflict, got: %v", err)
		graphRepo.AssertNotCalled(t, "SyncAgent")
	})

	t.Run("neo4j failure is logged not returned", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		agentRepo.On("Create", mock.Anything, mock.Anything).Return(nil)
		graphRepo.On("SyncAgent", mock.Anything, "test-agent").Return(errors.New("neo4j unavailable"))

		result, err := svc.Create(context.Background(), testCreateInput())

		require.NoError(t, err, "neo4j failure should not propagate")
		require.NotNil(t, result)
		assert.Equal(t, "test-agent", result.Name)

		agentRepo.AssertExpectations(t)
		graphRepo.AssertExpectations(t)
	})
}

// ---------- Get ----------

func TestGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		setupRepo func(m *mockAgentRepo)
		wantErr   error
		wantAgent bool
	}{
		{
			name:      "happy path",
			agentName: "my-agent",
			setupRepo: func(m *mockAgentRepo) {
				m.On("Get", mock.Anything, "my-agent").Return(&agentrepo.Agent{
					Name:  "my-agent",
					CRC64: "999",
				}, nil)
			},
			wantAgent: true,
		},
		{
			name:      "not found",
			agentName: "missing",
			setupRepo: func(m *mockAgentRepo) {
				m.On("Get", mock.Anything, "missing").Return(nil, agentrepo.ErrNotFound)
			},
			wantErr: service.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agentRepo := new(mockAgentRepo)
			graphRepo := new(mockGraphRepo)
			svc := newService(agentRepo, graphRepo)

			tt.setupRepo(agentRepo)

			result, err := svc.Get(context.Background(), tt.agentName)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got: %v", tt.wantErr, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.agentName, result.Name)
			}

			agentRepo.AssertExpectations(t)
		})
	}
}

// ---------- Update ----------

func TestUpdate(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		existing := &agentrepo.Agent{
			Name:       "my-agent",
			Definition: json.RawMessage(`{"old":"data"}`),
			CRC64:      "old-crc",
		}

		input := testUpdateInput()
		def := agentDefinition{
			Description:  input.Description,
			SystemPrompt: input.SystemPrompt,
			Model:        input.Model,
			AllowedTools: input.AllowedTools,
			Version:      input.Version,
		}
		wantCRC := expectedCRC64(t, def)

		agentRepo.On("Get", mock.Anything, "my-agent").Return(existing, nil)
		agentRepo.On("Update", mock.Anything, mock.MatchedBy(func(a *agentrepo.Agent) bool {
			return a.Name == "my-agent" && a.CRC64 == wantCRC
		})).Return(nil)
		graphRepo.On("SyncAgent", mock.Anything, "my-agent").Return(nil)

		result, err := svc.Update(context.Background(), "my-agent", input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, wantCRC, result.CRC64)
		assert.NotEqual(t, "old-crc", result.CRC64)

		agentRepo.AssertExpectations(t)
		graphRepo.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		agentRepo.On("Get", mock.Anything, "missing").Return(nil, agentrepo.ErrNotFound)

		result, err := svc.Update(context.Background(), "missing", testUpdateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)
		agentRepo.AssertNotCalled(t, "Update")
	})
}

// ---------- Delete ----------

func TestDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		setupRepo func(ar *mockAgentRepo, gr *mockGraphRepo)
		wantErr   error
	}{
		{
			name:      "happy path",
			agentName: "my-agent",
			setupRepo: func(ar *mockAgentRepo, gr *mockGraphRepo) {
				ar.On("Delete", mock.Anything, "my-agent").Return(nil)
				gr.On("DeleteAgent", mock.Anything, "my-agent").Return(nil)
			},
		},
		{
			name:      "not found",
			agentName: "missing",
			setupRepo: func(ar *mockAgentRepo, gr *mockGraphRepo) {
				ar.On("Delete", mock.Anything, "missing").Return(agentrepo.ErrNotFound)
			},
			wantErr: service.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agentRepo := new(mockAgentRepo)
			graphRepo := new(mockGraphRepo)
			svc := newService(agentRepo, graphRepo)

			tt.setupRepo(agentRepo, graphRepo)

			err := svc.Delete(context.Background(), tt.agentName)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr), "expected %v, got: %v", tt.wantErr, err)
			} else {
				require.NoError(t, err)
			}

			agentRepo.AssertExpectations(t)
			graphRepo.AssertExpectations(t)
		})
	}
}

// ---------- List ----------

func TestList(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		agents := []*agentrepo.Agent{
			{Name: "agent-a"},
			{Name: "agent-b"},
		}
		agentRepo.On("List", mock.Anything, repository.ListOptions{
			Offset: 0,
			Limit:  10,
		}).Return(agents, int64(2), nil)

		result, total, err := svc.List(context.Background(), agentsvc.ListOptions{
			Offset: 0,
			Limit:  10,
		})

		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, int64(2), total)

		agentRepo.AssertExpectations(t)
	})
}

// ---------- GetManifest ----------

func TestGetManifest(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		agentRepo := new(mockAgentRepo)
		graphRepo := new(mockGraphRepo)
		svc := newService(agentRepo, graphRepo)

		entries := []agentrepo.ManifestEntry{
			{Name: "agent-a", CRC64: "111"},
			{Name: "agent-b", CRC64: "222"},
		}
		agentRepo.On("GetManifest", mock.Anything).Return(entries, nil)

		result, err := svc.GetManifest(context.Background())

		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "agent-a", result[0].Name)
		assert.Equal(t, "111", result[0].CRC64)

		agentRepo.AssertExpectations(t)
	})
}
