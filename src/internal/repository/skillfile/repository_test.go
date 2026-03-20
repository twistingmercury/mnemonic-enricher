package skillfile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository/skillfile"
)

var testSkillID = uuid.New()

// testSkillFile returns a sample skill file for testing.
func testSkillFile() *skillfile.SkillFile {
	return &skillfile.SkillFile{
		SkillID: testSkillID,
		Path:    "scripts/setup.sh",
		Content: "#!/bin/bash\necho hello",
		CRC64:   "9876543210",
	}
}

// skillFileColumns returns the column names for a full skill_files SELECT.
func skillFileColumns() []string {
	return []string{"id", "skill_id", "path", "content", "crc64", "created_at", "updated_at"}
}

// ---------- Create ----------

func TestRepository_Create(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()

	tests := []struct {
		name      string
		file      *skillfile.SkillFile
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name: "successful creation",
			file: testSkillFile(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
					AddRow(testID, now, now)
				mock.ExpectQuery("INSERT INTO skill_files").
					WithArgs(testSkillID, "scripts/setup.sh", "#!/bin/bash\necho hello", "9876543210").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name: "duplicate path returns ErrExists",
			file: testSkillFile(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO skill_files").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(&pgconn.PgError{Code: "23505"})
			},
			wantErr: skillfile.ErrExists,
		},
		{
			name: "foreign key violation propagated",
			file: testSkillFile(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO skill_files").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(&pgconn.PgError{Code: "23503", Message: "foreign key violation"})
			},
			wantErr: &pgconn.PgError{Code: "23503"},
		},
		{
			name: "database error propagated",
			file: testSkillFile(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO skill_files").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
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

			repo := skillfile.NewRepository(mock)
			err = repo.Create(context.Background(), tt.file)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skillfile.ErrExists) {
					assert.ErrorIs(t, err, skillfile.ErrExists)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, testID, tt.file.ID)
				assert.False(t, tt.file.CreatedAt.IsZero())
				assert.False(t, tt.file.UpdatedAt.IsZero())
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
				rows := pgxmock.NewRows(skillFileColumns()).
					AddRow(testID, testSkillID, "scripts/setup.sh", "#!/bin/bash\necho hello", "9876543210", now, now)
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testID).
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: skillfile.ErrNotFound,
		},
		{
			name: "database error propagated",
			id:   uuid.New(),
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: errors.New("connection lost"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setup(mock)

			repo := skillfile.NewRepository(mock)
			f, err := repo.Get(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skillfile.ErrNotFound) {
					assert.ErrorIs(t, err, skillfile.ErrNotFound)
				}
				assert.Nil(t, f)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testID, f.ID)
				assert.Equal(t, testSkillID, f.SkillID)
				assert.Equal(t, "scripts/setup.sh", f.Path)
				assert.Equal(t, "#!/bin/bash\necho hello", f.Content)
				assert.Equal(t, "9876543210", f.CRC64)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- GetByPath ----------

func TestRepository_GetByPath(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testID := uuid.New()

	tests := []struct {
		name    string
		skillID uuid.UUID
		path    string
		setup   func(mock pgxmock.PgxPoolIface)
		wantErr error
	}{
		{
			name:    "successful retrieval",
			skillID: testSkillID,
			path:    "scripts/setup.sh",
			setup: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(skillFileColumns()).
					AddRow(testID, testSkillID, "scripts/setup.sh", "#!/bin/bash\necho hello", "9876543210", now, now)
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testSkillID, "scripts/setup.sh").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:    "not found returns ErrNotFound",
			skillID: testSkillID,
			path:    "nonexistent.txt",
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testSkillID, "nonexistent.txt").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: skillfile.ErrNotFound,
		},
		{
			name:    "database error propagated",
			skillID: testSkillID,
			path:    "any.txt",
			setup: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testSkillID, "any.txt").
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: errors.New("connection lost"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setup(mock)

			repo := skillfile.NewRepository(mock)
			f, err := repo.GetByPath(context.Background(), tt.skillID, tt.path)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skillfile.ErrNotFound) {
					assert.ErrorIs(t, err, skillfile.ErrNotFound)
				}
				assert.Nil(t, f)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testID, f.ID)
				assert.Equal(t, testSkillID, f.SkillID)
				assert.Equal(t, "scripts/setup.sh", f.Path)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Update ----------

func TestRepository_Update(t *testing.T) {
	t.Parallel()

	testID := uuid.New()

	tests := []struct {
		name      string
		file      *skillfile.SkillFile
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name: "successful update",
			file: &skillfile.SkillFile{
				ID:      testID,
				SkillID: testSkillID,
				Path:    "scripts/setup.sh",
				Content: "#!/bin/bash\necho updated",
				CRC64:   "1111111111",
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skill_files").
					WithArgs(
						testID,
						"#!/bin/bash\necho updated", // content
						"1111111111",                // crc64
						pgxmock.AnyArg(),            // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			file: &skillfile.SkillFile{
				ID:      uuid.New(),
				SkillID: testSkillID,
				Path:    "nonexistent.txt",
				Content: "content",
				CRC64:   "0000000000",
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skill_files").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: skillfile.ErrNotFound,
		},
		{
			name: "database error propagated",
			file: &skillfile.SkillFile{
				ID:      testID,
				SkillID: testSkillID,
				Path:    "scripts/setup.sh",
				Content: "content",
				CRC64:   "1111111111",
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE skill_files").
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

			repo := skillfile.NewRepository(mock)
			err = repo.Update(context.Background(), tt.file)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skillfile.ErrNotFound) {
					assert.ErrorIs(t, err, skillfile.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.file.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- Delete ----------

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
				mock.ExpectExec("DELETE FROM skill_files").
					WithArgs(testID).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name: "not found returns ErrNotFound",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skill_files").
					WithArgs(pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: skillfile.ErrNotFound,
		},
		{
			name: "database error propagated",
			id:   uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM skill_files").
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

			repo := skillfile.NewRepository(mock)
			err = repo.Delete(context.Background(), tt.id)

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, skillfile.ErrNotFound) {
					assert.ErrorIs(t, err, skillfile.ErrNotFound)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------- ListBySkill ----------

func TestRepository_ListBySkill(t *testing.T) {
	t.Parallel()

	now := time.Now()
	id1, id2 := uuid.New(), uuid.New()

	tests := []struct {
		name      string
		skillID   uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantPaths []string
		wantErr   error
	}{
		{
			name:    "returns all files ordered by path",
			skillID: testSkillID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(skillFileColumns()).
					AddRow(id1, testSkillID, "config/settings.yaml", "key: value", "111", now, now).
					AddRow(id2, testSkillID, "scripts/setup.sh", "#!/bin/bash", "222", now, now)
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testSkillID).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantPaths: []string{"config/settings.yaml", "scripts/setup.sh"},
		},
		{
			name:    "empty result returns empty slice",
			skillID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(skillFileColumns())
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantPaths: []string{},
		},
		{
			name:    "database error propagated",
			skillID: testSkillID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM skill_files").
					WithArgs(testSkillID).
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

			repo := skillfile.NewRepository(mock)
			files, err := repo.ListBySkill(context.Background(), tt.skillID)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, files, tt.wantCount)

				for i, expectedPath := range tt.wantPaths {
					assert.Equal(t, expectedPath, files[i].Path)
				}
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
		skillID   uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		want      []skillfile.ManifestEntry
		wantErr   bool
	}{
		{
			name:    "returns all entries ordered by path",
			skillID: testSkillID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"path", "crc64"}).
					AddRow("config/settings.yaml", "111").
					AddRow("scripts/setup.sh", "222").
					AddRow("templates/main.tmpl", "333")
				mock.ExpectQuery("SELECT path, crc64 FROM skill_files").
					WithArgs(testSkillID).
					WillReturnRows(rows)
			},
			want: []skillfile.ManifestEntry{
				{Path: "config/settings.yaml", CRC64: "111"},
				{Path: "scripts/setup.sh", CRC64: "222"},
				{Path: "templates/main.tmpl", CRC64: "333"},
			},
		},
		{
			name:    "empty table returns empty slice",
			skillID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"path", "crc64"})
				mock.ExpectQuery("SELECT path, crc64 FROM skill_files").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(rows)
			},
			want: []skillfile.ManifestEntry{},
		},
		{
			name:    "database error propagated",
			skillID: testSkillID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT path, crc64 FROM skill_files").
					WithArgs(testSkillID).
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

			repo := skillfile.NewRepository(mock)
			entries, err := repo.GetManifest(context.Background(), tt.skillID)

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
		mock.ExpectQuery("SELECT .* FROM skill_files").
			WithArgs(id).
			WillReturnError(context.Canceled)

		repo := skillfile.NewRepository(mock)
		f, err := repo.Get(ctx, id)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, f)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByPath respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT .* FROM skill_files").
			WithArgs(testSkillID, "scripts/setup.sh").
			WillReturnError(context.Canceled)

		repo := skillfile.NewRepository(mock)
		f, err := repo.GetByPath(ctx, testSkillID, "scripts/setup.sh")

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, f)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("INSERT INTO skill_files").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(context.Canceled)

		repo := skillfile.NewRepository(mock)
		err = repo.Create(ctx, testSkillFile())

		assert.ErrorIs(t, err, context.Canceled)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ListBySkill respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT .* FROM skill_files").
			WithArgs(testSkillID).
			WillReturnError(context.Canceled)

		repo := skillfile.NewRepository(mock)
		files, err := repo.ListBySkill(ctx, testSkillID)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, files)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetManifest respects cancelled context", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mock.ExpectQuery("SELECT path, crc64 FROM skill_files").
			WithArgs(testSkillID).
			WillReturnError(context.Canceled)

		repo := skillfile.NewRepository(mock)
		entries, err := repo.GetManifest(ctx, testSkillID)

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

		mock.ExpectQuery("INSERT INTO skill_files").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key"})

		repo := skillfile.NewRepository(mock)
		err = repo.Create(context.Background(), testSkillFile())

		assert.ErrorIs(t, err, skillfile.ErrExists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create foreign key violation not mapped to ErrExists", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("INSERT INTO skill_files").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(&pgconn.PgError{Code: "23503", Message: "foreign key violation"})

		repo := skillfile.NewRepository(mock)
		err = repo.Create(context.Background(), testSkillFile())

		assert.Error(t, err)
		assert.NotErrorIs(t, err, skillfile.ErrExists)
		assert.NotErrorIs(t, err, skillfile.ErrNotFound)

		var pgErr *pgconn.PgError
		assert.True(t, errors.As(err, &pgErr))
		assert.Equal(t, "23503", pgErr.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Get no rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectQuery("SELECT .* FROM skill_files").
			WithArgs(id).
			WillReturnError(pgx.ErrNoRows)

		repo := skillfile.NewRepository(mock)
		f, err := repo.Get(context.Background(), id)

		assert.ErrorIs(t, err, skillfile.ErrNotFound)
		assert.Nil(t, f)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByPath no rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("SELECT .* FROM skill_files").
			WithArgs(testSkillID, "missing.txt").
			WillReturnError(pgx.ErrNoRows)

		repo := skillfile.NewRepository(mock)
		f, err := repo.GetByPath(context.Background(), testSkillID, "missing.txt")

		assert.ErrorIs(t, err, skillfile.ErrNotFound)
		assert.Nil(t, f)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Update zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectExec("UPDATE skill_files").
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		repo := skillfile.NewRepository(mock)
		err = repo.Update(context.Background(), &skillfile.SkillFile{
			ID:      uuid.New(),
			Content: "content",
			CRC64:   "0000000000",
		})

		assert.ErrorIs(t, err, skillfile.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Delete zero rows maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec("DELETE FROM skill_files").
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := skillfile.NewRepository(mock)
		err = repo.Delete(context.Background(), id)

		assert.ErrorIs(t, err, skillfile.ErrNotFound)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
