package enrichmentjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/twistingmercury/mnemonic-enricher/internal/repository"
)

// Repository defines the job operations the enrichment worker needs.
type Repository interface {
	// Start marks a job as processing and returns it, in one statement.
	// Returns ErrNotFound if no job with the given ID exists.
	Start(ctx context.Context, id uuid.UUID) (*Job, error)

	// MarkCompleted updates job status to completed with completion time.
	MarkCompleted(ctx context.Context, id uuid.UUID) error

	// MarkFailed updates job status to failed with error message.
	// Increments attempt count and schedules retry if under max_attempts.
	MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error

	// ReclaimStale reclaims jobs stuck in processing state.
	// Jobs older than timeout are reset to pending for retry.
	// Increments attempt count so jobs that consistently timeout respect max_attempts.
	ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error)

	// DeleteCompleted removes completed jobs older than the retention period.
	DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error)

	// DeleteFailed removes failed jobs older than the retention period.
	DeleteFailed(ctx context.Context, retention time.Duration) (int64, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Start marks a job as processing and returns its target IDs. It replaces a
// SELECT followed by an UPDATE with one round trip.
func (r *pgxRepository) Start(ctx context.Context, id uuid.UUID) (*Job, error) {
	query := `
		UPDATE enrichment_jobs SET
			status = $2,
			started_at = now(),
			updated_at = now()
		WHERE id = $1
		RETURNING id, pattern_id, chunk_id
	`

	var job Job
	err := r.db.QueryRow(ctx, query, id, string(StatusProcessing)).Scan(&job.ID, &job.PatternID, &job.ChunkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("start job: %w", err)
	}

	return &job, nil
}

// MarkCompleted updates job status to completed with completion time.
// Uses SQL now() for timestamps to ensure consistency with database time.
func (r *pgxRepository) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE enrichment_jobs SET
			status = $2,
			completed_at = now(),
			last_error = NULL,
			updated_at = now()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, string(StatusCompleted))
	if err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkFailed updates job status to failed with error message.
// Uses a single atomic UPDATE with CASE expression to avoid TOCTOU race conditions.
// If attempts < max_attempts, the job is scheduled for retry (status='pending').
// If attempts >= max_attempts, the job remains in failed state.
// Uses SQL now() for timestamps to ensure consistency with database time.
func (r *pgxRepository) MarkFailed(ctx context.Context, id uuid.UUID, jobErr error, retryDelay time.Duration) error {
	// Use *string nil for empty error to set NULL in database
	var errMsg *string
	if jobErr != nil {
		msg := jobErr.Error()
		errMsg = &msg
	}

	// Single atomic UPDATE with CASE expression:
	// - Atomically increments attempts
	// - Sets status to 'pending' if retrying, 'failed' if max attempts reached
	// - Only updates scheduled_for when retrying
	// - Uses SQL now() + interval for retry delay calculation
	query := `
		UPDATE enrichment_jobs SET
			status = CASE WHEN attempts + 1 < max_attempts THEN $2 ELSE $3 END,
			attempts = attempts + 1,
			last_error = $4,
			scheduled_for = CASE WHEN attempts + 1 < max_attempts THEN now() + $5::interval ELSE scheduled_for END,
			started_at = CASE WHEN attempts + 1 < max_attempts THEN NULL ELSE started_at END,
			updated_at = now()
		WHERE id = $1
	`

	// Convert retryDelay to PostgreSQL interval string
	delayInterval := fmt.Sprintf("%d seconds", int(retryDelay.Seconds()))

	result, err := r.db.Exec(ctx, query,
		id,
		string(StatusPending),
		string(StatusFailed),
		errMsg,
		delayInterval,
	)
	if err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ReclaimStale reclaims jobs stuck in processing state.
// Jobs with started_at older than timeout are reset to pending for retry.
// Increments attempt count so jobs that consistently timeout respect max_attempts.
// Uses SQL now() for timestamps to ensure consistency with database time.
func (r *pgxRepository) ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error) {
	// Convert timeout to PostgreSQL interval string
	timeoutInterval := fmt.Sprintf("%d seconds", int(timeout.Seconds()))

	// Reclaim stale jobs:
	// - Only reclaim jobs where attempts + 1 < max_attempts (can still retry)
	// - Increment attempts to track timeout as a failed attempt
	// - Jobs that have exhausted retries are marked as failed instead
	query := `
		UPDATE enrichment_jobs SET
			status = CASE WHEN attempts + 1 < max_attempts THEN $1 ELSE $2 END,
			attempts = attempts + 1,
			started_at = NULL,
			last_error = CASE WHEN attempts + 1 >= max_attempts THEN 'job timed out' ELSE last_error END,
			updated_at = now()
		WHERE status = $3 AND started_at < now() - $4::interval
	`

	result, err := r.db.Exec(ctx, query,
		string(StatusPending),
		string(StatusFailed),
		string(StatusProcessing),
		timeoutInterval,
	)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale jobs: %w", err)
	}

	return result.RowsAffected(), nil
}

// DeleteCompleted removes completed jobs older than the retention period.
func (r *pgxRepository) DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)

	query := `
		DELETE FROM enrichment_jobs
		WHERE status = $1 AND completed_at < $2
	`

	result, err := r.db.Exec(ctx, query, string(StatusCompleted), cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete completed jobs: %w", err)
	}

	return result.RowsAffected(), nil
}

// DeleteFailed removes failed jobs older than the retention period.
func (r *pgxRepository) DeleteFailed(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)

	query := `
		DELETE FROM enrichment_jobs
		WHERE status = $1 AND updated_at < $2
	`

	result, err := r.db.Exec(ctx, query, string(StatusFailed), cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete failed jobs: %w", err)
	}

	return result.RowsAffected(), nil
}
