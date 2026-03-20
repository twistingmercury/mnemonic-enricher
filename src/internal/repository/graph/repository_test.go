package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository/graph"
)

// --- Mock types ---

// mockResultCollector implements graph.ResultCollector for unit testing.
type mockResultCollector struct {
	records    []*db.Record
	collectErr error
	singleErr  error
}

func (m *mockResultCollector) Collect(_ context.Context) ([]*db.Record, error) {
	if m.collectErr != nil {
		return nil, m.collectErr
	}
	return m.records, nil
}

func (m *mockResultCollector) Single(_ context.Context) (*db.Record, error) {
	if m.singleErr != nil {
		return nil, m.singleErr
	}
	if len(m.records) == 1 {
		return m.records[0], nil
	}
	if len(m.records) == 0 {
		return nil, errors.New("result contains no records")
	}
	return nil, errors.New("result contains more than one record")
}

// mockCypherRunner implements graph.CypherRunner for unit testing.
// It records the Cypher queries and parameters that were executed.
type mockCypherRunner struct {
	runFn func(ctx context.Context, cypher string, params map[string]any) (graph.ResultCollector, error)
	calls []cypherCall
}

type cypherCall struct {
	cypher string
	params map[string]any
}

func (m *mockCypherRunner) Run(ctx context.Context, cypher string, params map[string]any) (graph.ResultCollector, error) {
	m.calls = append(m.calls, cypherCall{cypher: cypher, params: params})
	if m.runFn != nil {
		return m.runFn(ctx, cypher, params)
	}
	return &mockResultCollector{}, nil
}

// mockSessionExecutor implements graph.SessionExecutor for unit testing.
type mockSessionExecutor struct {
	executeReadFn  func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error)
	executeWriteFn func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error)
	closeFn        func(ctx context.Context) error
}

func (m *mockSessionExecutor) ExecuteRead(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
	if m.executeReadFn != nil {
		return m.executeReadFn(ctx, work)
	}
	return nil, nil
}

func (m *mockSessionExecutor) ExecuteWrite(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
	if m.executeWriteFn != nil {
		return m.executeWriteFn(ctx, work)
	}
	return nil, nil
}

func (m *mockSessionExecutor) Close(_ context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(context.Background())
	}
	return nil
}

// --- Helper functions ---

// newTestRepo creates a Repository with a mock session factory for testing.
func newTestRepo(session *mockSessionExecutor) graph.Repository {
	return graph.ExportNewRepositoryWithFactory(func(_ context.Context) graph.SessionExecutor {
		return session
	}, nil)
}

// makeRecord creates a db.Record with the given keys and values.
func makeRecord(keys []string, values []any) *db.Record {
	return &db.Record{
		Keys:   keys,
		Values: values,
	}
}

// --- SyncAgent tests ---

func TestRepository_SyncAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "successful sync",
			agentName: "go-software-agent",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name:      "database error wraps with context",
			agentName: "failing-agent",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("connection refused")
					},
				}
			},
			wantErr: true,
			errMsg:  "syncing agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.SyncAgent(context.Background(), tt.agentName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_SyncAgent_CypherParams(t *testing.T) {
	t.Parallel()

	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SyncAgent(context.Background(), "test-agent")

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "MERGE (a:Agent {name: $name})")
	assert.Equal(t, "test-agent", capturedRunner.calls[0].params["name"])
}

// --- DeleteAgent tests ---

func TestRepository_DeleteAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "successful deletion",
			agentName: "go-software-agent",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name:      "database error wraps with context",
			agentName: "failing-agent",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("connection refused")
					},
				}
			},
			wantErr: true,
			errMsg:  "deleting agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.DeleteAgent(context.Background(), tt.agentName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_DeleteAgent_CypherParams(t *testing.T) {
	t.Parallel()

	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.DeleteAgent(context.Background(), "doomed-agent")

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "DETACH DELETE")
	assert.Equal(t, "doomed-agent", capturedRunner.calls[0].params["name"])
}

// --- SyncPattern tests ---

func TestRepository_SyncPattern(t *testing.T) {
	t.Parallel()

	desc := "A pattern for testing"
	patternID := uuid.New()

	tests := []struct {
		name      string
		pattern   *graph.Pattern
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful sync with description",
			pattern: &graph.Pattern{
				ID:          patternID,
				Name:        "test-pattern",
				Description: &desc,
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "successful sync with nil description",
			pattern: &graph.Pattern{
				ID:          patternID,
				Name:        "test-pattern-no-desc",
				Description: nil,
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			pattern: &graph.Pattern{
				ID:          patternID,
				Name:        "failing-pattern",
				Description: &desc,
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("timeout")
					},
				}
			},
			wantErr: true,
			errMsg:  "syncing pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.SyncPattern(context.Background(), tt.pattern)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_SyncPattern_CypherParams(t *testing.T) {
	t.Parallel()

	desc := "test description"
	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SyncPattern(context.Background(), &graph.Pattern{
		ID:          patternID,
		Name:        "my-pattern",
		Description: &desc,
	})

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "MERGE (p:Pattern {id: $patternId})")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
	assert.Equal(t, "my-pattern", capturedRunner.calls[0].params["patternName"])
	assert.Equal(t, "test description", capturedRunner.calls[0].params["patternDescription"])
}

func TestRepository_SyncPattern_NilDescriptionPassesNil(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SyncPattern(context.Background(), &graph.Pattern{
		ID:          patternID,
		Name:        "no-desc-pattern",
		Description: nil,
	})

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Nil(t, capturedRunner.calls[0].params["patternDescription"])
}

// --- DeletePattern tests ---

func TestRepository_DeletePattern(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name      string
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful deletion",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("connection closed")
					},
				}
			},
			wantErr: true,
			errMsg:  "deleting pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.DeletePattern(context.Background(), patternID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_DeletePattern_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.DeletePattern(context.Background(), patternID)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "DETACH DELETE")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
}

// --- SyncConcepts tests ---

func TestRepository_SyncConcepts(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name      string
		concepts  []graph.Concept
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful sync with concepts",
			concepts: []graph.Concept{
				{Name: "golang", Type: "technology"},
				{Name: "microservices", Type: "practice"},
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name:     "successful sync with empty concepts only removes old relationships",
			concepts: []graph.Concept{},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			concepts: []graph.Concept{
				{Name: "golang", Type: "technology"},
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("write failed")
					},
				}
			},
			wantErr: true,
			errMsg:  "syncing concepts for pattern",
		},
		{
			name: "step 1 failure returns wrapped error",
			concepts: []graph.Concept{
				{Name: "golang", Type: "technology"},
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return nil, errors.New("delete failed")
							},
						}
						return work(runner)
					},
				}
			},
			wantErr: true,
			errMsg:  "removing old MENTIONED_IN relationships",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.SyncConcepts(context.Background(), patternID, tt.concepts)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_SyncConcepts_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	concepts := []graph.Concept{
		{Name: "golang", Type: "technology"},
		{Name: "testing", Type: "practice"},
	}

	repo := newTestRepo(session)
	err := repo.SyncConcepts(context.Background(), patternID, concepts)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 2, "expected two Cypher calls: delete old + create new")

	// First call: diff-based delete — only remove relationships for concepts NOT in the new list
	assert.Contains(t, capturedRunner.calls[0].cypher, "MENTIONED_IN")
	assert.Contains(t, capturedRunner.calls[0].cypher, "WHERE NOT c.name IN $conceptNames")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE r")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
	assert.Equal(t, []string{"golang", "testing"}, capturedRunner.calls[0].params["conceptNames"])

	// Second call: MERGE concepts and relationships (idempotent)
	assert.Contains(t, capturedRunner.calls[1].cypher, "UNWIND")
	assert.Contains(t, capturedRunner.calls[1].cypher, "MERGE (c:Concept {name: concept.name})")
	assert.Contains(t, capturedRunner.calls[1].cypher, "MERGE (c)-[:MENTIONED_IN]->(p)")
	assert.Equal(t, patternID.String(), capturedRunner.calls[1].params["patternId"])

	conceptMaps := capturedRunner.calls[1].params["concepts"].([]map[string]any)
	require.Len(t, conceptMaps, 2)
	assert.Equal(t, "golang", conceptMaps[0]["name"])
	assert.Equal(t, "technology", conceptMaps[0]["type"])
	assert.Equal(t, "testing", conceptMaps[1]["name"])
	assert.Equal(t, "practice", conceptMaps[1]["type"])
}

func TestRepository_SyncConcepts_EmptyConceptsOnlyDeletes(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SyncConcepts(context.Background(), patternID, []graph.Concept{})

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1, "expected only the delete call for empty concepts")
	assert.Contains(t, capturedRunner.calls[0].cypher, "WHERE NOT c.name IN $conceptNames")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE r")
	assert.Equal(t, []string{}, capturedRunner.calls[0].params["conceptNames"])
}

func TestRepository_SyncConcepts_Step2Failure(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	callCount := 0

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					callCount++
					if callCount == 2 {
						return nil, errors.New("create failed")
					}
					return &mockResultCollector{}, nil
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SyncConcepts(context.Background(), patternID, []graph.Concept{
		{Name: "golang", Type: "technology"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating concepts and relationships")
}

// --- SetPatternAgentRelevance tests ---

func TestRepository_SetPatternAgentRelevance(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name         string
		associations []graph.AgentAssociation
		setupMock    func() *mockSessionExecutor
		wantErr      bool
		errMsg       string
	}{
		{
			name: "successful set with associations",
			associations: []graph.AgentAssociation{
				{AgentName: "go-agent", Relevance: 0.9},
				{AgentName: "python-agent", Relevance: 0.3},
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name:         "successful set with empty associations only removes old relationships",
			associations: []graph.AgentAssociation{},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			associations: []graph.AgentAssociation{
				{AgentName: "go-agent", Relevance: 0.9},
			},
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("write failed")
					},
				}
			},
			wantErr: true,
			errMsg:  "setting agent relevance for pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.SetPatternAgentRelevance(context.Background(), patternID, tt.associations)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_SetPatternAgentRelevance_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	associations := []graph.AgentAssociation{
		{AgentName: "go-agent", Relevance: 0.95},
		{AgentName: "devops-agent", Relevance: 0.4},
	}

	repo := newTestRepo(session)
	err := repo.SetPatternAgentRelevance(context.Background(), patternID, associations)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 2, "expected two Cypher calls: delete old + create new")

	// First call: diff-based delete — only remove relationships for agents NOT in the new list
	assert.Contains(t, capturedRunner.calls[0].cypher, "RELEVANT_FOR")
	assert.Contains(t, capturedRunner.calls[0].cypher, "WHERE NOT a.name IN $agentNames")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE r")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
	assert.Equal(t, []string{"go-agent", "devops-agent"}, capturedRunner.calls[0].params["agentNames"])

	// Second call: MERGE relationships and update relevance
	assert.Contains(t, capturedRunner.calls[1].cypher, "UNWIND")
	assert.Contains(t, capturedRunner.calls[1].cypher, "MERGE (p)-[r:RELEVANT_FOR]->(a)")
	assert.Equal(t, patternID.String(), capturedRunner.calls[1].params["patternId"])

	assocMaps := capturedRunner.calls[1].params["associations"].([]map[string]any)
	require.Len(t, assocMaps, 2)
	assert.Equal(t, "go-agent", assocMaps[0]["agentName"])
	assert.Equal(t, 0.95, assocMaps[0]["relevance"])
	assert.Equal(t, "devops-agent", assocMaps[1]["agentName"])
	assert.Equal(t, 0.4, assocMaps[1]["relevance"])
}

func TestRepository_SetPatternAgentRelevance_EmptyAssociationsOnlyDeletes(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SetPatternAgentRelevance(context.Background(), patternID, []graph.AgentAssociation{})

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1, "expected only the delete call for empty associations")
	assert.Contains(t, capturedRunner.calls[0].cypher, "WHERE NOT a.name IN $agentNames")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE r")
	assert.Equal(t, []string{}, capturedRunner.calls[0].params["agentNames"])
}

func TestRepository_SetPatternAgentRelevance_Step1Failure(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return nil, errors.New("delete failed")
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SetPatternAgentRelevance(context.Background(), patternID, []graph.AgentAssociation{
		{AgentName: "go-agent", Relevance: 0.9},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing old RELEVANT_FOR relationships")
}

func TestRepository_SetPatternAgentRelevance_Step2Failure(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	callCount := 0

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					callCount++
					if callCount == 2 {
						return nil, errors.New("create failed")
					}
					return &mockResultCollector{}, nil
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	err := repo.SetPatternAgentRelevance(context.Background(), patternID, []graph.AgentAssociation{
		{AgentName: "go-agent", Relevance: 0.9},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating RELEVANT_FOR relationships")
}

// --- ComputeRelatedToEdges tests ---

func TestRepository_ComputeRelatedToEdges(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name      string
		setupMock func() *mockSessionExecutor
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful computation",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{}
						return work(runner)
					},
				}
			},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("connection refused")
					},
				}
			},
			wantErr: true,
			errMsg:  "computing related-to edges for pattern",
		},
		{
			name: "step 1 failure returns wrapped error",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return nil, errors.New("delete failed")
							},
						}
						return work(runner)
					},
				}
			},
			wantErr: true,
			errMsg:  "deleting existing RELATED_TO edges",
		},
		{
			name: "step 2 failure returns wrapped error",
			setupMock: func() *mockSessionExecutor {
				callCount := 0
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								callCount++
								if callCount == 2 {
									return nil, errors.New("compute failed")
								}
								return &mockResultCollector{}, nil
							},
						}
						return work(runner)
					},
				}
			},
			wantErr: true,
			errMsg:  "computing RELATED_TO edges",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			err := repo.ComputeRelatedToEdges(context.Background(), patternID, 0.3)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRepository_ComputeRelatedToEdges_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	err := repo.ComputeRelatedToEdges(context.Background(), patternID, 0.3)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 2, "expected two Cypher calls: delete old + compute new")

	// First call: delete existing RELATED_TO edges
	assert.Contains(t, capturedRunner.calls[0].cypher, "RELATED_TO")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE r")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])

	// Second call: compute and create RELATED_TO edges
	assert.Contains(t, capturedRunner.calls[1].cypher, "MENTIONED_IN")
	assert.Contains(t, capturedRunner.calls[1].cypher, "RELATED_TO")
	assert.Contains(t, capturedRunner.calls[1].cypher, "similarity")
	assert.Equal(t, patternID.String(), capturedRunner.calls[1].params["patternId"])
	assert.Equal(t, 0.3, capturedRunner.calls[1].params["minSimilarity"])
}

func TestRepository_Validation_ComputeRelatedToEdges(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	err := repo.ComputeRelatedToEdges(context.Background(), uuid.Nil, 0.3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

// --- GetPatternConcepts tests ---

func TestRepository_GetPatternConcepts(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name      string
		setupMock func() *mockSessionExecutor
		want      []graph.Concept
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful find with results",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"name", "type"},
											[]any{"golang", "technology"},
										),
										makeRecord(
											[]string{"name", "type"},
											[]any{"microservices", "practice"},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want: []graph.Concept{
				{Name: "golang", Type: "technology"},
				{Name: "microservices", Type: "practice"},
			},
			wantErr: false,
		},
		{
			name: "no results returns empty slice",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{records: []*db.Record{}}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    []graph.Concept{},
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("read failed")
					},
				}
			},
			want:    nil,
			wantErr: true,
			errMsg:  "getting concepts for pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			result, err := repo.GetPatternConcepts(context.Background(), patternID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestRepository_GetPatternConcepts_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{records: []*db.Record{}}, nil
				},
			}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.GetPatternConcepts(context.Background(), patternID)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "MENTIONED_IN")
	assert.Contains(t, capturedRunner.calls[0].cypher, "c.name AS name")
	assert.Contains(t, capturedRunner.calls[0].cypher, "c.type AS type")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
}

func TestRepository_GetPatternConcepts_CollectError(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{collectErr: errors.New("collect failed")}, nil
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.GetPatternConcepts(context.Background(), patternID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "collect failed")
}

func TestRepository_Validation_GetPatternConcepts(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	_, err := repo.GetPatternConcepts(context.Background(), uuid.Nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

// --- FindRelatedPatterns tests ---

func TestRepository_FindRelatedPatterns(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	relatedID1 := uuid.New()
	relatedID2 := uuid.New()

	tests := []struct {
		name      string
		limit     int
		setupMock func() *mockSessionExecutor
		want      []graph.RelatedPattern
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "successful find with results",
			limit: 10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"id", "name", "sharedConcepts", "similarity", "conceptNames"},
											[]any{relatedID1.String(), "pattern-b", int64(5), float64(0.83), []any{"go", "concurrency", "channels", "testing", "errors"}},
										),
										makeRecord(
											[]string{"id", "name", "sharedConcepts", "similarity", "conceptNames"},
											[]any{relatedID2.String(), "pattern-c", int64(2), float64(0.5), []any{"go", "testing"}},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want: []graph.RelatedPattern{
				{ID: relatedID1, Name: "pattern-b", SharedConcepts: 5, Similarity: 0.83, ConceptNames: []string{"go", "concurrency", "channels", "testing", "errors"}},
				{ID: relatedID2, Name: "pattern-c", SharedConcepts: 2, Similarity: 0.5, ConceptNames: []string{"go", "testing"}},
			},
			wantErr: false,
		},
		{
			name:  "no results returns empty slice",
			limit: 10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{records: []*db.Record{}}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    []graph.RelatedPattern{},
			wantErr: false,
		},
		{
			name:  "database error wraps with context",
			limit: 10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("read failed")
					},
				}
			},
			want:    nil,
			wantErr: true,
			errMsg:  "finding related patterns",
		},
		{
			name:  "invalid UUID in result returns error",
			limit: 10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"id", "name", "sharedConcepts", "similarity", "conceptNames"},
											[]any{"not-a-uuid", "bad-pattern", int64(1), float64(0.5), []any{"go"}},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    nil,
			wantErr: true,
			errMsg:  "parsing pattern ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			result, err := repo.FindRelatedPatterns(context.Background(), patternID, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestRepository_FindRelatedPatterns_CypherParams(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{records: []*db.Record{}}, nil
				},
			}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.FindRelatedPatterns(context.Background(), patternID, 5)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "RELATED_TO")
	assert.Contains(t, capturedRunner.calls[0].cypher, "similarity")
	assert.Contains(t, capturedRunner.calls[0].cypher, "conceptNames")
	assert.Equal(t, patternID.String(), capturedRunner.calls[0].params["patternId"])
	assert.Equal(t, 5, capturedRunner.calls[0].params["limit"])
}

func TestRepository_FindRelatedPatterns_CollectError(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{collectErr: errors.New("collect failed")}, nil
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.FindRelatedPatterns(context.Background(), patternID, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "collect failed")
}

// --- FindPatternsByAgent tests ---

func TestRepository_FindPatternsByAgent(t *testing.T) {
	t.Parallel()

	patternID1 := uuid.New()
	patternID2 := uuid.New()

	tests := []struct {
		name      string
		agentName string
		limit     int
		setupMock func() *mockSessionExecutor
		want      []graph.PatternRelevance
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "successful find with results",
			agentName: "go-agent",
			limit:     10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"id", "name", "relevance"},
											[]any{patternID1.String(), "go-patterns", float64(0.95)},
										),
										makeRecord(
											[]string{"id", "name", "relevance"},
											[]any{patternID2.String(), "testing-patterns", float64(0.7)},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want: []graph.PatternRelevance{
				{ID: patternID1, Name: "go-patterns", Relevance: 0.95},
				{ID: patternID2, Name: "testing-patterns", Relevance: 0.7},
			},
			wantErr: false,
		},
		{
			name:      "no results returns empty slice",
			agentName: "unknown-agent",
			limit:     10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{records: []*db.Record{}}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    []graph.PatternRelevance{},
			wantErr: false,
		},
		{
			name:      "database error wraps with context",
			agentName: "go-agent",
			limit:     10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("connection refused")
					},
				}
			},
			want:    nil,
			wantErr: true,
			errMsg:  "finding patterns for agent",
		},
		{
			name:      "invalid UUID in result returns error",
			agentName: "go-agent",
			limit:     10,
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"id", "name", "relevance"},
											[]any{"invalid-uuid", "bad-pattern", float64(0.5)},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    nil,
			wantErr: true,
			errMsg:  "parsing pattern ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			result, err := repo.FindPatternsByAgent(context.Background(), tt.agentName, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestRepository_FindPatternsByAgent_CypherParams(t *testing.T) {
	t.Parallel()

	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{records: []*db.Record{}}, nil
				},
			}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.FindPatternsByAgent(context.Background(), "my-agent", 20)

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Equal(t, "my-agent", capturedRunner.calls[0].params["agentName"])
	assert.Equal(t, 20, capturedRunner.calls[0].params["limit"])
}

func TestRepository_FindPatternsByAgent_CollectError(t *testing.T) {
	t.Parallel()

	session := &mockSessionExecutor{
		executeReadFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			runner := &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{collectErr: errors.New("collect failed")}, nil
				},
			}
			return work(runner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.FindPatternsByAgent(context.Background(), "go-agent", 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "collect failed")
}

// --- CleanupOrphanedConcepts tests ---

func TestRepository_CleanupOrphanedConcepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupMock func() *mockSessionExecutor
		want      int64
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful cleanup deletes orphaned concepts",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"deletedCount"},
											[]any{int64(7)},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    7,
			wantErr: false,
		},
		{
			name: "no orphans returns zero",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records: []*db.Record{
										makeRecord(
											[]string{"deletedCount"},
											[]any{int64(0)},
										),
									},
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "database error wraps with context",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
						return nil, errors.New("database unavailable")
					},
				}
			},
			want:    0,
			wantErr: true,
			errMsg:  "cleaning up orphaned concepts",
		},
		{
			name: "single record error propagates",
			setupMock: func() *mockSessionExecutor {
				return &mockSessionExecutor{
					executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
						runner := &mockCypherRunner{
							runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
								return &mockResultCollector{
									records:   []*db.Record{},
									singleErr: errors.New("no records"),
								}, nil
							},
						}
						return work(runner)
					},
				}
			},
			want:    0,
			wantErr: true,
			errMsg:  "no records",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := tt.setupMock()
			repo := newTestRepo(session)

			count, err := repo.CleanupOrphanedConcepts(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, count)
			}
		})
	}
}

func TestRepository_CleanupOrphanedConcepts_CypherQuery(t *testing.T) {
	t.Parallel()

	var capturedRunner *mockCypherRunner

	session := &mockSessionExecutor{
		executeWriteFn: func(ctx context.Context, work func(runner graph.CypherRunner) (any, error)) (any, error) {
			capturedRunner = &mockCypherRunner{
				runFn: func(_ context.Context, _ string, _ map[string]any) (graph.ResultCollector, error) {
					return &mockResultCollector{
						records: []*db.Record{
							makeRecord([]string{"deletedCount"}, []any{int64(0)}),
						},
					}, nil
				},
			}
			return work(capturedRunner)
		},
	}

	repo := newTestRepo(session)
	_, err := repo.CleanupOrphanedConcepts(context.Background())

	require.NoError(t, err)
	require.Len(t, capturedRunner.calls, 1)
	assert.Contains(t, capturedRunner.calls[0].cypher, "MATCH (c:Concept)")
	assert.Contains(t, capturedRunner.calls[0].cypher, "WHERE NOT (c)-[:MENTIONED_IN]->()")
	assert.Contains(t, capturedRunner.calls[0].cypher, "DELETE c")
}

// --- Context cancellation tests ---

func TestRepository_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session := &mockSessionExecutor{
		executeWriteFn: func(_ context.Context, _ func(runner graph.CypherRunner) (any, error)) (any, error) {
			return nil, context.Canceled
		},
	}

	repo := newTestRepo(session)

	err := repo.SyncAgent(ctx, "test-agent")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Domain type tests ---

func TestPattern_Description(t *testing.T) {
	t.Parallel()

	t.Run("nil description", func(t *testing.T) {
		t.Parallel()
		p := graph.Pattern{
			ID:          uuid.New(),
			Name:        "test",
			Description: nil,
		}
		assert.Nil(t, p.Description)
	})

	t.Run("non-nil description", func(t *testing.T) {
		t.Parallel()
		desc := "a description"
		p := graph.Pattern{
			ID:          uuid.New(),
			Name:        "test",
			Description: &desc,
		}
		require.NotNil(t, p.Description)
		assert.Equal(t, "a description", *p.Description)
	})
}

// --- HealthCheck tests ---

func TestRepository_HealthCheck(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := graph.ExportNewRepositoryWithHealthCheck(
			func(_ context.Context) graph.SessionExecutor {
				return &mockSessionExecutor{}
			},
			func(_ context.Context) error {
				return nil
			},
		)

		err := repo.HealthCheck(context.Background())
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		repo := graph.ExportNewRepositoryWithHealthCheck(
			func(_ context.Context) graph.SessionExecutor {
				return &mockSessionExecutor{}
			},
			func(_ context.Context) error {
				return errors.New("connection refused")
			},
		)

		err := repo.HealthCheck(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("default no-op when nil", func(t *testing.T) {
		t.Parallel()

		repo := graph.ExportNewRepositoryWithHealthCheck(
			func(_ context.Context) graph.SessionExecutor {
				return &mockSessionExecutor{}
			},
			nil,
		)

		err := repo.HealthCheck(context.Background())
		assert.NoError(t, err)
	})
}

// --- Input validation tests ---

func TestRepository_Validation_SyncAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		errMsg    string
	}{
		{name: "empty string", agentName: "", errMsg: "agentName must not be empty"},
		{name: "whitespace only", agentName: "   ", errMsg: "agentName must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := newTestRepo(&mockSessionExecutor{})
			err := repo.SyncAgent(context.Background(), tt.agentName)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestRepository_Validation_DeleteAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		errMsg    string
	}{
		{name: "empty string", agentName: "", errMsg: "agentName must not be empty"},
		{name: "whitespace only", agentName: "  ", errMsg: "agentName must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := newTestRepo(&mockSessionExecutor{})
			err := repo.DeleteAgent(context.Background(), tt.agentName)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestRepository_Validation_SyncPattern(t *testing.T) {
	t.Parallel()

	t.Run("nil pattern", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(&mockSessionExecutor{})
		err := repo.SyncPattern(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern must not be nil")
	})

	t.Run("empty pattern name", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(&mockSessionExecutor{})
		err := repo.SyncPattern(context.Background(), &graph.Pattern{
			ID:   uuid.New(),
			Name: "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern name must not be empty")
	})

	t.Run("whitespace pattern name", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(&mockSessionExecutor{})
		err := repo.SyncPattern(context.Background(), &graph.Pattern{
			ID:   uuid.New(),
			Name: "   ",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern name must not be empty")
	})
}

func TestRepository_Validation_DeletePattern(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	err := repo.DeletePattern(context.Background(), uuid.Nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

func TestRepository_Validation_SyncConcepts(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	err := repo.SyncConcepts(context.Background(), uuid.Nil, []graph.Concept{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

func TestRepository_Validation_SetPatternAgentRelevance(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	err := repo.SetPatternAgentRelevance(context.Background(), uuid.Nil, []graph.AgentAssociation{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

func TestRepository_Validation_FindRelatedPatterns(t *testing.T) {
	t.Parallel()

	repo := newTestRepo(&mockSessionExecutor{})
	_, err := repo.FindRelatedPatterns(context.Background(), uuid.Nil, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patternID must not be nil UUID")
}

func TestRepository_Validation_FindPatternsByAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		errMsg    string
	}{
		{name: "empty string", agentName: "", errMsg: "agentName must not be empty"},
		{name: "whitespace only", agentName: "  \t ", errMsg: "agentName must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := newTestRepo(&mockSessionExecutor{})
			_, err := repo.FindPatternsByAgent(context.Background(), tt.agentName, 10)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}
