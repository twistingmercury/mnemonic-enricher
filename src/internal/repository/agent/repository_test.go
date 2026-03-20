package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository"
	"github.com/twistingmercury/mnemonic/internal/repository/agent"
)

// testDefinition returns a sample JSONB definition for testing.
func testDefinition() json.RawMessage {
	return json.RawMessage(`{"description":"test agent","system_prompt":"You are helpful.","model":"sonnet","allowed_tools":["Read","Write"],"version":"1.0.0"}`)
}

// testAgent returns a sample agent for testing.
func testAgent() *agent.Agent {
	return &agent.Agent{
		Name:       "test-agent",
		Definition: testDefinition(),
		CRC64:      "1234567890",
	}
}

// agentColumns returns the column names for a full agent SELECT.
func agentColumns() []string {
	return []string{"id", "name", "definition", "crc64", "created_at", "updated_at"}
}

// ---------- Create ----------

func TestRepository_Create(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()

	tests := []struct {
		name      string
		agent     *agent.Agent
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful creation",
			agent: testAgent(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
					AddRow(testID, now, now)
				mock.ExpectQuery("INSERT INTO agents").
					WithArgs("test-agent", testDefinition(), "1234567890").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:  "duplicate name returns ErrExists",
			agent: testAgent(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO agents").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(&pgconn.PgError{Code: "23505"})
			},
			wantErr: agent.ErrExists,
		},
		{
			name:  "database error propagated",
			agent: testAgent(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO agents").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: errors.New("connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			err = repo.Create(context.Background(), tt.agent)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, agent.ErrExists) {
					assert.ErrorIs(t, err, agent.ErrExists)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, testID, tt.agent.ID)
				assert.False(t, tt.agent.CreatedAt.IsZero())
				assert.False(t, tt.agent.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Get (by name) ----------

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()
	def := testDefinition()

	tests := []struct {
		name      string
		agentName string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantAgent *agent.Agent
		wantErr   error
	}{
		{
			name:      "successful retrieval",
			agentName: "test-agent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(agentColumns()).
					AddRow(testID, "test-agent", def, "1234567890", now, now)
				mock.ExpectQuery("SELECT .* FROM agents").
					WithArgs("test-agent").
					WillReturnRows(rows)
			},
			wantAgent: &agent.Agent{
				ID:         testID,
				Name:       "test-agent",
				Definition: def,
				CRC64:      "1234567890",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			agentName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM agents").
					WithArgs("nonexistent").
					WillReturnError(pgx.ErrNoRows)
			},
			wantAgent: nil,
			wantErr:   agent.ErrNotFound,
		},
		{
			name:      "database error propagated",
			agentName: "any",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM agents").
					WithArgs("any").
					WillReturnError(errors.New("connection lost"))
			},
			wantAgent: nil,
			wantErr:   errors.New("connection lost"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			a, err := repo.Get(context.Background(), tt.agentName)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, agent.ErrNotFound) {
					assert.ErrorIs(t, err, agent.ErrNotFound)
				}
				assert.Nil(t, a)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantAgent.ID, a.ID)
				assert.Equal(t, tt.wantAgent.Name, a.Name)
				assert.JSONEq(t, string(tt.wantAgent.Definition), string(a.Definition))
				assert.Equal(t, tt.wantAgent.CRC64, a.CRC64)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- GetByID ----------

func TestRepository_GetByID(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()
	def := testDefinition()

	tests := []struct {
		name    string
		id      uuid.UUID
		setup   func(mock pgxmock.PgxPoolIface)
		wantErr error
	}{
		{
			name: "successful retrieval",
			id:   testID,
			setup: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(agentColumns()).
					AddRow(testID, "test-agent", def, "1234567890", now, now)
				mock.ExpectQuery("SELECT .* FROM agents").
					WithArgs(testID).
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM agents").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: agent.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setup(mock)

			repo := agent.NewRepository(mock)
			a, err := repo.GetByID(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, a)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testID, a.ID)
				assert.Equal(t, "test-agent", a.Name)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Update ----------

func TestRepository_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agent     *agent.Agent
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful update",
			agent: testAgent(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE agents").
					WithArgs(
						"test-agent",
						pgxmock.AnyArg(), // definition
						pgxmock.AnyArg(), // crc64
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			agent: &agent.Agent{
				Name:       "nonexistent",
				Definition: testDefinition(),
				CRC64:      "9999999999",
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE agents").
					WithArgs(
						"nonexistent",
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: agent.ErrNotFound,
		},
		{
			name:  "database error propagated",
			agent: testAgent(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE agents").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(errors.New("disk full"))
			},
			wantErr: errors.New("disk full"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			err = repo.Update(context.Background(), tt.agent)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, agent.ErrNotFound) {
					assert.ErrorIs(t, err, agent.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.agent.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Delete (by name) ----------

func TestRepository_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agentName string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "successful deletion",
			agentName: "test-agent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM agents").
					WithArgs("test-agent").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			agentName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM agents").
					WithArgs("nonexistent").
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: agent.ErrNotFound,
		},
		{
			name:      "database error propagated",
			agentName: "any",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM agents").
					WithArgs("any").
					WillReturnError(errors.New("connection reset"))
			},
			wantErr: errors.New("connection reset"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			err = repo.Delete(context.Background(), tt.agentName)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, agent.ErrNotFound) {
					assert.ErrorIs(t, err, agent.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- DeleteByID ----------

func TestRepository_DeleteByID(t *testing.T) {
	t.Parallel()

	testID := uuid.New()

	tests := []struct {
		name    string
		id      uuid.UUID
		setup   func(mock pgxmock.PgxPoolIface)
		wantErr error
	}{
		{
			name: "successful deletion",
			id:   testID,
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM agents").
					WithArgs(testID).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM agents").
					WithArgs(pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: agent.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setup(mock)

			repo := agent.NewRepository(mock)
			err = repo.DeleteByID(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- List ----------

func TestRepository_List(t *testing.T) {
	t.Parallel()

	now := time.Now()
	def := testDefinition()
	id1, id2 := uuid.New(), uuid.New()

	listColumns := []string{"id", "name", "definition", "crc64", "created_at", "updated_at", "total_count"}

	tests := []struct {
		name       string
		opts       repository.ListOptions
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantCount  int
		wantTotal  int64
		wantErr    error
		wantAgents []string
	}{
		{
			name: "list all agents without pagination",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id1, "agent-a", def, "111", now, now, int64(2)).
					AddRow(id2, "agent-b", def, "222", now, now, int64(2))
				mock.ExpectQuery("SELECT .* FROM agents ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount:  2,
			wantTotal:  2,
			wantAgents: []string{"agent-a", "agent-b"},
		},
		{
			name: "list with limit and offset",
			opts: repository.ListOptions{Limit: 1, Offset: 1},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id2, "agent-b", def, "222", now, now, int64(2))
				mock.ExpectQuery("SELECT .* FROM agents ORDER BY name ASC LIMIT").
					WithArgs(1, 1).
					WillReturnRows(rows)
			},
			wantCount:  1,
			wantTotal:  2,
			wantAgents: []string{"agent-b"},
		},
		{
			name: "empty list returns empty slice",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns)
				mock.ExpectQuery("SELECT .* FROM agents ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount:  0,
			wantTotal:  0,
			wantAgents: []string{},
		},
		{
			name: "list with offset only",
			opts: repository.ListOptions{Offset: 5, Limit: 0},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id1, "agent-f", def, "111", now, now, int64(10))
				mock.ExpectQuery("SELECT .* FROM agents ORDER BY name ASC OFFSET").
					WithArgs(5).
					WillReturnRows(rows)
			},
			wantCount:  1,
			wantTotal:  10,
			wantAgents: []string{"agent-f"},
		},
		{
			name: "database error propagated",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM agents ORDER BY name").
					WillReturnError(errors.New("timeout"))
			},
			wantErr: errors.New("timeout"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			agents, total, err := repo.List(context.Background(), tt.opts)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTotal, total)
				assert.Len(t, agents, tt.wantCount)

				for i, expectedName := range tt.wantAgents {
					assert.Equal(t, expectedName, agents[i].Name)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Exists ----------

func TestRepository_Exists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		agentName  string
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantExists bool
		wantErr    bool
	}{
		{
			name:      "agent exists",
			agentName: "existing-agent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("existing-agent").
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
			},
			wantExists: true,
		},
		{
			name:      "agent does not exist",
			agentName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("nonexistent").
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
			},
			wantExists: false,
		},
		{
			name:      "database error",
			agentName: "any-agent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("any-agent").
					WillReturnError(errors.New("connection failed"))
			},
			wantExists: false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			exists, err := repo.Exists(context.Background(), tt.agentName)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantExists, exists)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- GetManifest ----------

func TestRepository_GetManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      []agent.ManifestEntry
		wantErr   bool
	}{
		{
			name: "returns all entries ordered by name",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"name", "crc64"}).
					AddRow("agent-a", "111").
					AddRow("agent-b", "222").
					AddRow("agent-c", "333")
				mock.ExpectQuery("SELECT name, crc64 FROM agents ORDER BY name").
					WillReturnRows(rows)
			},
			want: []agent.ManifestEntry{
				{Name: "agent-a", CRC64: "111"},
				{Name: "agent-b", CRC64: "222"},
				{Name: "agent-c", CRC64: "333"},
			},
		},
		{
			name: "empty table returns empty slice",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"name", "crc64"})
				mock.ExpectQuery("SELECT name, crc64 FROM agents ORDER BY name").
					WillReturnRows(rows)
			},
			want: []agent.ManifestEntry{},
		},
		{
			name: "database error propagated",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT name, crc64 FROM agents ORDER BY name").
					WillReturnError(errors.New("query failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := agent.NewRepository(mock)
			entries, err := repo.GetManifest(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, entries)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Context cancellation ----------

func TestRepository_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("Get respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT .* FROM agents").
			WithArgs("test-agent").
			WillReturnError(context.Canceled)

		repo := agent.NewRepository(mock)
		a, err := repo.Get(ctx, "test-agent")

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, a)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByID respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		id := uuid.New()
		mock.ExpectQuery("SELECT .* FROM agents").
			WithArgs(id).
			WillReturnError(context.Canceled)

		repo := agent.NewRepository(mock)
		a, err := repo.GetByID(ctx, id)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, a)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("INSERT INTO agents").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(context.Canceled)

		repo := agent.NewRepository(mock)
		err = repo.Create(ctx, testAgent())

		assert.ErrorIs(t, err, context.Canceled)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("List respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT .* FROM agents ORDER BY name").
			WillReturnError(context.Canceled)

		repo := agent.NewRepository(mock)
		agents, total, err := repo.List(ctx, repository.ListOptions{})

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, agents)
		assert.Equal(t, int64(0), total)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetManifest respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT name, crc64 FROM agents ORDER BY name").
			WillReturnError(context.Canceled)

		repo := agent.NewRepository(mock)
		entries, err := repo.GetManifest(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, entries)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ---------- SQL error mapping ----------

func TestRepository_SQLErrorMapping(t *testing.T) {
	t.Parallel()

	t.Run("Create unique violation maps to ErrExists", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("INSERT INTO agents").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})

		repo := agent.NewRepository(mock)
		err = repo.Create(context.Background(), testAgent())

		assert.ErrorIs(t, err, agent.ErrExists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create non-unique PgError not mapped", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("INSERT INTO agents").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23514", Message: "check violation"})

		repo := agent.NewRepository(mock)
		err = repo.Create(context.Background(), testAgent())

		assert.Error(t, err)
		assert.NotErrorIs(t, err, agent.ErrExists)
		assert.NotErrorIs(t, err, agent.ErrNotFound)

		var pgErr *pgconn.PgError
		assert.True(t, errors.As(err, &pgErr))
		assert.Equal(t, "23514", pgErr.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Get no rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("SELECT .* FROM agents").
			WithArgs("missing").
			WillReturnError(pgx.ErrNoRows)

		repo := agent.NewRepository(mock)
		a, err := repo.Get(context.Background(), "missing")

		assert.ErrorIs(t, err, agent.ErrNotFound)
		assert.Nil(t, a)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByID no rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectQuery("SELECT .* FROM agents").
			WithArgs(id).
			WillReturnError(pgx.ErrNoRows)

		repo := agent.NewRepository(mock)
		a, err := repo.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, agent.ErrNotFound)
		assert.Nil(t, a)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Update zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("UPDATE agents").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		repo := agent.NewRepository(mock)
		err = repo.Update(context.Background(), testAgent())

		assert.ErrorIs(t, err, agent.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Delete zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("DELETE FROM agents").
			WithArgs("missing").
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := agent.NewRepository(mock)
		err = repo.Delete(context.Background(), "missing")

		assert.ErrorIs(t, err, agent.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteByID zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec("DELETE FROM agents").
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := agent.NewRepository(mock)
		err = repo.DeleteByID(context.Background(), id)

		assert.ErrorIs(t, err, agent.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ---------- ListOptions defaults ----------

func TestListOptions_Default(t *testing.T) {
	t.Parallel()

	opts := repository.DefaultListOptions()
	assert.Equal(t, 100, opts.Limit)
	assert.Equal(t, 0, opts.Offset)
}
