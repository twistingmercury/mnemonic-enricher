package enrichmentjob_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/mnemonic-enricher/internal/repository/enrichmentjob"
)

func TestRepository_Start(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	chunkID := uuid.New()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      *enrichmentjob.Job
		wantErr   error
	}{
		{
			name: "marks processing and returns the job",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing").
					WillReturnRows(pgxmock.NewRows([]string{"id", "pattern_id", "chunk_id"}).
						AddRow(jobID, (*uuid.UUID)(nil), &chunkID))
			},
			want: &enrichmentjob.Job{ID: jobID, ChunkID: &chunkID},
		},
		{
			name: "not found returns ErrNotFound",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: enrichmentjob.ErrNotFound,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: errors.New("start job: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			got, err := enrichmentjob.NewRepository(mock).Start(context.Background(), jobID)

			switch {
			case errors.Is(tt.wantErr, enrichmentjob.ErrNotFound):
				assert.ErrorIs(t, err, tt.wantErr)
			case tt.wantErr != nil:
				assert.EqualError(t, err, tt.wantErr.Error())
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
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
