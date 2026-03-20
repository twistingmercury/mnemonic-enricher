package enrichmentjob_test

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

	"github.com/twistingmercury/mnemonic/internal/repository"
	"github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
)

// testJob returns a sample enrichment job for testing.
func testJob() *enrichmentjob.Job {
	pid := uuid.New()
	return &enrichmentjob.Job{
		ID:           uuid.New(),
		PatternID:    &pid,
		Status:       enrichmentjob.StatusPending,
		Attempts:     0,
		MaxAttempts:  3,
		ScheduledFor: time.Now(),
	}
}

func TestRepository_Create(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name      string
		job       *enrichmentjob.Job
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name: "successful creation - pattern-based job",
			job:  testJob(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"scheduled_for", "created_at", "updated_at"}).
					AddRow(now, now, now)
				mock.ExpectQuery("INSERT INTO enrichment_jobs").
					WithArgs(
						pgxmock.AnyArg(), // id
						pgxmock.AnyArg(), // pattern_id
						pgxmock.AnyArg(), // chunk_id
						"pending",
						0,                // attempts
						3,                // max_attempts
						pgxmock.AnyArg(), // last_error
						pgxmock.AnyArg(), // scheduled_for
						pgxmock.AnyArg(), // started_at
						pgxmock.AnyArg(), // completed_at
					).
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name: "successful creation - chunk-based job",
			job: func() *enrichmentjob.Job {
				cid := uuid.New()
				return &enrichmentjob.Job{
					ID:          uuid.New(),
					PatternID:   nil,
					ChunkID:     &cid,
					Status:      enrichmentjob.StatusPending,
					Attempts:    0,
					MaxAttempts: 3,
				}
			}(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"scheduled_for", "created_at", "updated_at"}).
					AddRow(now, now, now)
				mock.ExpectQuery("INSERT INTO enrichment_jobs").
					WithArgs(
						pgxmock.AnyArg(), // id
						pgxmock.AnyArg(), // pattern_id (nil)
						pgxmock.AnyArg(), // chunk_id
						"pending",
						0,                // attempts
						3,                // max_attempts
						pgxmock.AnyArg(), // last_error
						pgxmock.AnyArg(), // scheduled_for
						pgxmock.AnyArg(), // started_at
						pgxmock.AnyArg(), // completed_at
					).
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name: "pattern not found returns ErrPatternNotFound",
			job:  testJob(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO enrichment_jobs").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnError(&pgconn.PgError{Code: "23503"})
			},
			wantErr: enrichmentjob.ErrPatternNotFound,
		},
		{
			name: "check violation returns ErrInvalidJobTarget",
			job:  testJob(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("INSERT INTO enrichment_jobs").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnError(&pgconn.PgError{Code: "23514"})
			},
			wantErr: enrichmentjob.ErrInvalidJobTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			err = repo.Create(context.Background(), tt.job)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.job.CreatedAt.IsZero())
				assert.False(t, tt.job.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Create_GeneratesUUID(t *testing.T) {
	t.Parallel()

	now := time.Now()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	pid := uuid.New()
	job := &enrichmentjob.Job{
		// ID is not set - should be generated
		PatternID: &pid,
	}

	rows := pgxmock.NewRows([]string{"scheduled_for", "created_at", "updated_at"}).
		AddRow(now, now, now)
	mock.ExpectQuery("INSERT INTO enrichment_jobs").
		WithArgs(
			pgxmock.AnyArg(), // generated id
			pgxmock.AnyArg(), // pattern_id
			pgxmock.AnyArg(), // chunk_id
			"pending",        // default status
			0,                // default attempts
			3,                // default max_attempts
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnRows(rows)

	repo := enrichmentjob.NewRepository(mock)
	err = repo.Create(context.Background(), job)

	assert.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, job.ID)
	assert.Equal(t, enrichmentjob.StatusPending, job.Status)
	assert.Equal(t, 3, job.MaxAttempts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	now := time.Now()
	jobID := uuid.New()
	patternID := uuid.New()

	tests := []struct {
		name      string
		jobID     uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantJob   *enrichmentjob.Job
		wantErr   error
	}{
		{
			name:  "successful retrieval",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at",
				}).AddRow(
					jobID,
					&patternID,
					(*uuid.UUID)(nil),
					"pending",
					0,
					3,
					nil,
					now,
					nil,
					nil,
					now,
					now,
				)
				mock.ExpectQuery("SELECT .* FROM enrichment_jobs").
					WithArgs(jobID).
					WillReturnRows(rows)
			},
			wantJob: &enrichmentjob.Job{
				ID:           jobID,
				PatternID:    &patternID,
				Status:       enrichmentjob.StatusPending,
				Attempts:     0,
				MaxAttempts:  3,
				ScheduledFor: now,
				CreatedAt:    now,
				UpdatedAt:    now,
			},
			wantErr: nil,
		},
		{
			name:  "not found returns ErrNotFound",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM enrichment_jobs").
					WithArgs(jobID).
					WillReturnError(pgx.ErrNoRows)
			},
			wantJob: nil,
			wantErr: enrichmentjob.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			job, err := repo.Get(context.Background(), tt.jobID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, job)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantJob.ID, job.ID)
				assert.Equal(t, tt.wantJob.PatternID, job.PatternID)
				assert.Equal(t, tt.wantJob.Status, job.Status)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_GetByPatternID(t *testing.T) {
	t.Parallel()

	now := time.Now()
	jobID := uuid.New()
	patternID := uuid.New()

	tests := []struct {
		name      string
		patternID uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "successful retrieval",
			patternID: patternID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at",
				}).AddRow(
					jobID,
					&patternID,
					(*uuid.UUID)(nil),
					"completed",
					1,
					3,
					nil,
					now,
					&now,
					&now,
					now,
					now,
				)
				mock.ExpectQuery("SELECT .* FROM enrichment_jobs WHERE pattern_id").
					WithArgs(patternID).
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			patternID: patternID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM enrichment_jobs WHERE pattern_id").
					WithArgs(patternID).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: enrichmentjob.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			job, err := repo.GetByPatternID(context.Background(), tt.patternID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, job)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, job.PatternID)
				assert.Equal(t, tt.patternID, *job.PatternID)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Get_ScanError(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Return a row with wrong column count to force a scan error.
	rows := pgxmock.NewRows([]string{"id"}).AddRow(jobID)
	mock.ExpectQuery("SELECT .* FROM enrichment_jobs").
		WithArgs(jobID).
		WillReturnRows(rows)

	repo := enrichmentjob.NewRepository(mock)
	job, err := repo.Get(context.Background(), jobID)

	assert.Nil(t, job)
	assert.ErrorContains(t, err, "scan enrichment job")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_ClaimPending(t *testing.T) {
	t.Parallel()

	now := time.Now()
	jobID := uuid.New()
	patternID := uuid.New()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantJob   bool
		wantErr   error
	}{
		{
			name: "successfully claims a pending job",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at",
				}).AddRow(
					jobID,
					&patternID,
					(*uuid.UUID)(nil),
					"processing",
					0,
					3,
					nil,
					now,
					&now,
					nil,
					now,
					now,
				)
				mock.ExpectQuery("UPDATE enrichment_jobs").
					WithArgs(
						"processing",
						"pending",
					).
					WillReturnRows(rows)
			},
			wantJob: true,
			wantErr: nil,
		},
		{
			name: "no pending jobs returns nil",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs").
					WithArgs(
						"processing",
						"pending",
					).
					WillReturnError(pgx.ErrNoRows)
			},
			wantJob: false,
			wantErr: nil,
		},
		{
			name: "database error is propagated",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs").
					WithArgs(
						"processing",
						"pending",
					).
					WillReturnError(errors.New("connection failed"))
			},
			wantJob: false,
			wantErr: errors.New("connection failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			job, err := repo.ClaimPending(context.Background())

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantJob {
					assert.NotNil(t, job)
					assert.Equal(t, enrichmentjob.StatusProcessing, job.Status)
				} else {
					assert.Nil(t, job)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_MarkProcessing(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()

	tests := []struct {
		name      string
		jobID     uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful update",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:  "not found returns ErrNotFound",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing").
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: enrichmentjob.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			err = repo.MarkProcessing(context.Background(), tt.jobID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_MarkCompleted(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()

	tests := []struct {
		name      string
		jobID     uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful update",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "completed").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:  "not found returns ErrNotFound",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "completed").
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: enrichmentjob.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			err = repo.MarkCompleted(context.Background(), tt.jobID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_MarkFailed_WithRetry(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Single atomic UPDATE with CASE expression
	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			pgxmock.AnyArg(), // last_error (*string)
			"300 seconds",    // retry delay interval
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, errors.New("test error"), retryDelay)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_MarkFailed_MaxAttemptsReached(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Single atomic UPDATE with CASE expression
	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			pgxmock.AnyArg(), // last_error (*string)
			"300 seconds",    // retry delay interval
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, errors.New("final error"), retryDelay)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_MarkFailed_NotFound(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			pgxmock.AnyArg(),
			"300 seconds",
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, errors.New("error"), retryDelay)

	assert.ErrorIs(t, err, enrichmentjob.ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_MarkFailed_NilError(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// When jobErr is nil, last_error should be nil (*string nil, not interface nil)
	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			pgxmock.AnyArg(), // last_error is *string nil (NULL in DB)
			"300 seconds",    // retry delay interval
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, nil, retryDelay)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_ReclaimStale(t *testing.T) {
	t.Parallel()

	timeout := 30 * time.Minute

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "reclaims stale jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", "1800 seconds").
					WillReturnResult(pgxmock.NewResult("UPDATE", 5))
			},
			wantCount: 5,
		},
		{
			name: "no stale jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", "1800 seconds").
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", "1800 seconds").
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("reclaim stale jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.ReclaimStale(context.Background(), timeout)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "reclaim stale jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_DeleteCompleted(t *testing.T) {
	t.Parallel()

	retention := 7 * 24 * time.Hour // 7 days

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "deletes old completed jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 10))
			},
			wantCount: 10,
		},
		{
			name: "no jobs to delete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("delete completed jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.DeleteCompleted(context.Background(), retention)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "delete completed jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_DeleteFailed(t *testing.T) {
	t.Parallel()

	retention := 30 * 24 * time.Hour // 30 days

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "deletes old failed jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 3))
			},
			wantCount: 3,
		},
		{
			name: "no jobs to delete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("delete failed jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.DeleteFailed(context.Background(), retention)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "delete failed jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_List(t *testing.T) {
	t.Parallel()

	now := time.Now()
	jobID := uuid.New()
	patternID := uuid.New()
	chunkID := uuid.New()

	tests := []struct {
		name      string
		filter    enrichmentjob.Filter
		opts      repository.ListOptions
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantTotal int64
		wantErr   error
	}{
		{
			name:   "list all jobs without filter",
			filter: enrichmentjob.Filter{},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(jobID, &patternID, (*uuid.UUID)(nil), "pending", 0, 3, nil, now, nil, nil, now, now, int64(2)).
					AddRow(uuid.New(), (*uuid.UUID)(nil), &chunkID, "completed", 1, 3, nil, now, &now, &now, now, now, int64(2))

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs ORDER BY scheduled_for").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "list with status filter",
			filter: enrichmentjob.Filter{
				Status: ptr("pending"),
			},
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(jobID, &patternID, (*uuid.UUID)(nil), "pending", 0, 3, nil, now, nil, nil, now, now, int64(1))

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs WHERE status").
					WithArgs("pending").
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name: "list with pattern_id filter",
			filter: enrichmentjob.Filter{
				PatternID: &patternID,
			},
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(jobID, &patternID, (*uuid.UUID)(nil), "pending", 0, 3, nil, now, nil, nil, now, now, int64(1))

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs WHERE pattern_id").
					WithArgs(patternID).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name: "list with chunk_id filter",
			filter: enrichmentjob.Filter{
				ChunkID: &chunkID,
			},
			opts: repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(uuid.New(), (*uuid.UUID)(nil), &chunkID, "pending", 0, 3, nil, now, nil, nil, now, now, int64(1))

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs WHERE chunk_id").
					WithArgs(chunkID).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name:   "list with pagination",
			filter: enrichmentjob.Filter{},
			opts:   repository.ListOptions{Limit: 1, Offset: 1},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(jobID, &patternID, (*uuid.UUID)(nil), "pending", 0, 3, nil, now, nil, nil, now, now, int64(2))

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs ORDER BY scheduled_for ASC, created_at ASC LIMIT").
					WithArgs(1, 1).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 2,
		},
		{
			name:   "empty list returns empty slice",
			filter: enrichmentjob.Filter{},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "pattern_id", "chunk_id", "status", "attempts", "max_attempts",
					"last_error", "scheduled_for", "started_at", "completed_at",
					"created_at", "updated_at", "total_count",
				})

				mock.ExpectQuery("SELECT .* FROM enrichment_jobs ORDER BY scheduled_for").
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			jobs, total, err := repo.List(context.Background(), tt.filter, tt.opts)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTotal, total)
				assert.Len(t, jobs, tt.wantCount)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ptr is a helper function to create a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}

func TestJob_StatusHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status       enrichmentjob.JobStatus
		isPending    bool
		isProcessing bool
		isCompleted  bool
		isFailed     bool
	}{
		{enrichmentjob.StatusPending, true, false, false, false},
		{enrichmentjob.StatusProcessing, false, true, false, false},
		{enrichmentjob.StatusCompleted, false, false, true, false},
		{enrichmentjob.StatusFailed, false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			job := &enrichmentjob.Job{Status: tt.status}

			assert.Equal(t, tt.isPending, job.IsPending())
			assert.Equal(t, tt.isProcessing, job.IsProcessing())
			assert.Equal(t, tt.isCompleted, job.IsCompleted())
			assert.Equal(t, tt.isFailed, job.IsFailed())
		})
	}
}

func TestJob_CanRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		canRetry    bool
	}{
		{"zero attempts", 0, 3, true},
		{"one attempt", 1, 3, true},
		{"two attempts", 2, 3, true},
		{"max attempts reached", 3, 3, false},
		{"over max attempts", 4, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := &enrichmentjob.Job{
				Attempts:    tt.attempts,
				MaxAttempts: tt.maxAttempts,
			}

			assert.Equal(t, tt.canRetry, job.CanRetry())
		})
	}
}

func TestIsValidStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status enrichmentjob.JobStatus
		want   bool
	}{
		{enrichmentjob.StatusPending, true},
		{enrichmentjob.StatusProcessing, true},
		{enrichmentjob.StatusCompleted, true},
		{enrichmentjob.StatusFailed, true},
		{"invalid", false},
		{"PENDING", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, enrichmentjob.IsValidStatus(tt.status))
		})
	}
}
