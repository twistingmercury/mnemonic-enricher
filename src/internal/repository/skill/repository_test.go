package skill_test

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
	"github.com/twistingmercury/mnemonic/internal/repository/skill"
)

// testDefinition returns a sample JSONB definition for testing.
func testDefinition() json.RawMessage {
	return json.RawMessage(`{"description":"test skill","content":"---\nname: test-skill\n---\n\nYou are testing.","tags":["test"],"version":"1.0.0"}`)
}

// testSkill returns a sample skill for testing.
func testSkill() *skill.Skill {
	return &skill.Skill{
		Name:       "test-skill",
		Definition: testDefinition(),
		CRC64:      "1234567890",
	}
}

// skillColumns returns the column names for a full skill SELECT.
func skillColumns() []string {
	return []string{"id", "name", "definition", "crc64", "created_at", "updated_at"}
}

// ---------- Create ----------

func TestRepository_Create(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()

	tests := []struct {
		name      string
		skill     *skill.Skill
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful creation",
			skill: testSkill(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
					AddRow(testID, now, now)
				mock.ExpectQuery("INSERT INTO skills").
					WithArgs("test-skill", testDefinition(), "1234567890").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:  "duplicate name returns ErrExists",
			skill: testSkill(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO skills").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(&pgconn.PgError{Code: "23505"})
			},
			wantErr: skill.ErrExists,
		},
		{
			name:  "database error propagated",
			skill: testSkill(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO skills").
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

			repo := skill.NewRepository(mock)
			err = repo.Create(context.Background(), tt.skill)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrExists) {
					assert.ErrorIs(t, err, skill.ErrExists)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, testID, tt.skill.ID)
				assert.False(t, tt.skill.CreatedAt.IsZero())
				assert.False(t, tt.skill.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Get (by ID) ----------

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()
	def := testDefinition()

	tests := []struct {
		name      string
		id        uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantSkill *skill.Skill
		wantErr   error
	}{
		{
			name: "successful retrieval",
			id:   testID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(skillColumns()).
					AddRow(testID, "test-skill", def, "1234567890", now, now)
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs(testID).
					WillReturnRows(rows)
			},
			wantSkill: &skill.Skill{
				ID:         testID,
				Name:       "test-skill",
				Definition: def,
				CRC64:      "1234567890",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantSkill: nil,
			wantErr:   skill.ErrNotFound,
		},
		{
			name: "database error propagated",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(errors.New("connection lost"))
			},
			wantSkill: nil,
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

			repo := skill.NewRepository(mock)
			s, err := repo.Get(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrNotFound) {
					assert.ErrorIs(t, err, skill.ErrNotFound)
				}
				assert.Nil(t, s)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSkill.ID, s.ID)
				assert.Equal(t, tt.wantSkill.Name, s.Name)
				assert.JSONEq(t, string(tt.wantSkill.Definition), string(s.Definition))
				assert.Equal(t, tt.wantSkill.CRC64, s.CRC64)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- GetByName ----------

func TestRepository_GetByName(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()
	def := testDefinition()

	tests := []struct {
		name      string
		skillName string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantSkill *skill.Skill
		wantErr   error
	}{
		{
			name:      "successful retrieval",
			skillName: "test-skill",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(skillColumns()).
					AddRow(testID, "test-skill", def, "1234567890", now, now)
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs("test-skill").
					WillReturnRows(rows)
			},
			wantSkill: &skill.Skill{
				ID:         testID,
				Name:       "test-skill",
				Definition: def,
				CRC64:      "1234567890",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			skillName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs("nonexistent").
					WillReturnError(pgx.ErrNoRows)
			},
			wantSkill: nil,
			wantErr:   skill.ErrNotFound,
		},
		{
			name:      "database error propagated",
			skillName: "any",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skills").
					WithArgs("any").
					WillReturnError(errors.New("connection lost"))
			},
			wantSkill: nil,
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

			repo := skill.NewRepository(mock)
			s, err := repo.GetByName(context.Background(), tt.skillName)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrNotFound) {
					assert.ErrorIs(t, err, skill.ErrNotFound)
				}
				assert.Nil(t, s)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSkill.ID, s.ID)
				assert.Equal(t, tt.wantSkill.Name, s.Name)
				assert.JSONEq(t, string(tt.wantSkill.Definition), string(s.Definition))
				assert.Equal(t, tt.wantSkill.CRC64, s.CRC64)
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
		skill     *skill.Skill
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful update",
			skill: testSkill(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skills").
					WithArgs(
						"test-skill",
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
			skill: &skill.Skill{
				Name:       "nonexistent",
				Definition: testDefinition(),
				CRC64:      "9999999999",
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skills").
					WithArgs(
						"nonexistent",
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: skill.ErrNotFound,
		},
		{
			name:  "database error propagated",
			skill: testSkill(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skills").
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

			repo := skill.NewRepository(mock)
			err = repo.Update(context.Background(), tt.skill)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrNotFound) {
					assert.ErrorIs(t, err, skill.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.skill.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Delete (by ID) ----------

func TestRepository_Delete(t *testing.T) {
	t.Parallel()

	testID := uuid.New()

	tests := []struct {
		name      string
		id        uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name: "successful deletion",
			id:   testID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
					WithArgs(testID).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
					WithArgs(pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: skill.ErrNotFound,
		},
		{
			name: "database error propagated",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
					WithArgs(pgxmock.AnyArg()).
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

			repo := skill.NewRepository(mock)
			err = repo.Delete(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrNotFound) {
					assert.ErrorIs(t, err, skill.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- DeleteByName ----------

func TestRepository_DeleteByName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		skillName string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "successful deletion",
			skillName: "test-skill",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
					WithArgs("test-skill").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			skillName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
					WithArgs("nonexistent").
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: skill.ErrNotFound,
		},
		{
			name:      "database error propagated",
			skillName: "any",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skills").
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

			repo := skill.NewRepository(mock)
			err = repo.DeleteByName(context.Background(), tt.skillName)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skill.ErrNotFound) {
					assert.ErrorIs(t, err, skill.ErrNotFound)
				}
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
		wantSkills []string
	}{
		{
			name: "list all skills without pagination",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id1, "skill-a", def, "111", now, now, int64(2)).
					AddRow(id2, "skill-b", def, "222", now, now, int64(2))
				mock.ExpectQuery("SELECT .* FROM skills ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount:  2,
			wantTotal:  2,
			wantSkills: []string{"skill-a", "skill-b"},
		},
		{
			name: "list with limit and offset",
			opts: repository.ListOptions{Limit: 1, Offset: 1},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id2, "skill-b", def, "222", now, now, int64(2))
				mock.ExpectQuery("SELECT .* FROM skills ORDER BY name ASC LIMIT").
					WithArgs(1, 1).
					WillReturnRows(rows)
			},
			wantCount:  1,
			wantTotal:  2,
			wantSkills: []string{"skill-b"},
		},
		{
			name: "empty list returns empty slice",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns)
				mock.ExpectQuery("SELECT .* FROM skills ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount:  0,
			wantTotal:  0,
			wantSkills: []string{},
		},
		{
			name: "list with offset only",
			opts: repository.ListOptions{Offset: 5, Limit: 0},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(listColumns).
					AddRow(id1, "skill-f", def, "111", now, now, int64(10))
				mock.ExpectQuery("SELECT .* FROM skills ORDER BY name ASC OFFSET").
					WithArgs(5).
					WillReturnRows(rows)
			},
			wantCount:  1,
			wantTotal:  10,
			wantSkills: []string{"skill-f"},
		},
		{
			name: "database error propagated",
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skills ORDER BY name").
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

			repo := skill.NewRepository(mock)
			skills, total, err := repo.List(context.Background(), tt.opts)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTotal, total)
				assert.Len(t, skills, tt.wantCount)

				for i, expectedName := range tt.wantSkills {
					assert.Equal(t, expectedName, skills[i].Name)
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
		skillName  string
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantExists bool
		wantErr    bool
	}{
		{
			name:      "skill exists",
			skillName: "existing-skill",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("existing-skill").
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
			},
			wantExists: true,
		},
		{
			name:      "skill does not exist",
			skillName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("nonexistent").
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
			},
			wantExists: false,
		},
		{
			name:      "database error",
			skillName: "any-skill",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs("any-skill").
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

			repo := skill.NewRepository(mock)
			exists, err := repo.Exists(context.Background(), tt.skillName)

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
		want      []skill.ManifestEntry
		wantErr   bool
	}{
		{
			name: "returns all entries ordered by name",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"name", "crc64"}).
					AddRow("skill-a", "111").
					AddRow("skill-b", "222").
					AddRow("skill-c", "333")
				mock.ExpectQuery("SELECT name, crc64 FROM skills ORDER BY name").
					WillReturnRows(rows)
			},
			want: []skill.ManifestEntry{
				{Name: "skill-a", CRC64: "111"},
				{Name: "skill-b", CRC64: "222"},
				{Name: "skill-c", CRC64: "333"},
			},
		},
		{
			name: "empty table returns empty slice",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"name", "crc64"})
				mock.ExpectQuery("SELECT name, crc64 FROM skills ORDER BY name").
					WillReturnRows(rows)
			},
			want: []skill.ManifestEntry{},
		},
		{
			name: "database error propagated",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT name, crc64 FROM skills ORDER BY name").
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

			repo := skill.NewRepository(mock)
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

		id := uuid.New()
		mock.ExpectQuery("SELECT .* FROM skills").
			WithArgs(id).
			WillReturnError(context.Canceled)

		repo := skill.NewRepository(mock)
		s, err := repo.Get(ctx, id)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, s)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByName respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT .* FROM skills").
			WithArgs("test-skill").
			WillReturnError(context.Canceled)

		repo := skill.NewRepository(mock)
		s, err := repo.GetByName(ctx, "test-skill")

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, s)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("INSERT INTO skills").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(context.Canceled)

		repo := skill.NewRepository(mock)
		err = repo.Create(ctx, testSkill())

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

		mock.ExpectQuery("SELECT .* FROM skills ORDER BY name").
			WillReturnError(context.Canceled)

		repo := skill.NewRepository(mock)
		skills, total, err := repo.List(ctx, repository.ListOptions{})

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, skills)
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

		mock.ExpectQuery("SELECT name, crc64 FROM skills ORDER BY name").
			WillReturnError(context.Canceled)

		repo := skill.NewRepository(mock)
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

		mock.ExpectQuery("INSERT INTO skills").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})

		repo := skill.NewRepository(mock)
		err = repo.Create(context.Background(), testSkill())

		assert.ErrorIs(t, err, skill.ErrExists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create non-unique PgError not mapped", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("INSERT INTO skills").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23514", Message: "check violation"})

		repo := skill.NewRepository(mock)
		err = repo.Create(context.Background(), testSkill())

		assert.Error(t, err)
		assert.NotErrorIs(t, err, skill.ErrExists)
		assert.NotErrorIs(t, err, skill.ErrNotFound)

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

		id := uuid.New()
		mock.ExpectQuery("SELECT .* FROM skills").
			WithArgs(id).
			WillReturnError(pgx.ErrNoRows)

		repo := skill.NewRepository(mock)
		s, err := repo.Get(context.Background(), id)

		assert.ErrorIs(t, err, skill.ErrNotFound)
		assert.Nil(t, s)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByName no rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("SELECT .* FROM skills").
			WithArgs("missing").
			WillReturnError(pgx.ErrNoRows)

		repo := skill.NewRepository(mock)
		s, err := repo.GetByName(context.Background(), "missing")

		assert.ErrorIs(t, err, skill.ErrNotFound)
		assert.Nil(t, s)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Update zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("UPDATE skills").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		repo := skill.NewRepository(mock)
		err = repo.Update(context.Background(), testSkill())

		assert.ErrorIs(t, err, skill.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Delete zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec("DELETE FROM skills").
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := skill.NewRepository(mock)
		err = repo.Delete(context.Background(), id)

		assert.ErrorIs(t, err, skill.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteByName zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("DELETE FROM skills").
			WithArgs("missing").
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := skill.NewRepository(mock)
		err = repo.DeleteByName(context.Background(), "missing")

		assert.ErrorIs(t, err, skill.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
