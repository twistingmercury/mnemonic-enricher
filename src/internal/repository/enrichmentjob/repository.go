package enrichmentjob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/twistingmercury/mnemonic/internal/repository"
)

// Repository defines data access operations for enrichment jobs.
type Repository interface {
	// Create stores a new enrichment job.
	Create(ctx context.Context, job *Job) error

	// Get retrieves an enrichment job by ID. Returns ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*Job, error)

	// GetByPatternID retrieves the latest job for a pattern.
	// Returns ErrNotFound if no job exists for the pattern.
	GetByPatternID(ctx context.Context, patternID uuid.UUID) (*Job, error)

	// ClaimPending atomically claims a pending job for processing.
	// Uses FOR UPDATE SKIP LOCKED for safe concurrent processing.
	// Returns nil, nil if no pending jobs are available.
	ClaimPending(ctx context.Context) (*Job, error)

	// MarkProcessing updates job status to processing with start time.
	MarkProcessing(ctx context.Context, id uuid.UUID) error

	// MarkCompleted updates job status to completed with completion time.
	MarkCompleted(ctx context.Context, id uuid.UUID) error

	// MarkFailed updates job status to failed with error message.
	// Increments attempt count and schedules retry if under max_attempts.
	MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error

	// ReclaimStale reclaims jobs stuck in processing state.
	// Jobs older than timeout are reset to pending for retry.
	// Increments attempt count so jobs that consistently timeout respect max_attempts.
	ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error)

	// List retrieves enrichment jobs with filtering and pagination.
	// Returns the jobs, total count, and any error.
	List(ctx context.Context, filter Filter, opts repository.ListOptions) ([]*Job, int64, error)

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

// Create stores a new enrichment job in the database.
// Uses SQL now() for timestamps to ensure consistency with database time.
// Uses COALESCE to handle scheduled_for - if not provided, defaults to now().
func (r *pgxRepository) Create(ctx context.Context, job *Job) error {
	// Generate UUID if not provided
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}

	// Set defaults
	if job.Status == "" {
		job.Status = StatusPending
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = DefaultMaxAttempts
	}

	// Use COALESCE to default scheduled_for to now() if not set
	query := `
		INSERT INTO enrichment_jobs (
			id, pattern_id, chunk_id, status, attempts, max_attempts,
			last_error, scheduled_for, started_at, completed_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, now()), $9, $10, now(), now())
		RETURNING scheduled_for, created_at, updated_at
	`

	// Handle nil scheduled_for for COALESCE
	var scheduledForArg any
	if job.ScheduledFor.IsZero() {
		scheduledForArg = nil
	} else {
		scheduledForArg = job.ScheduledFor
	}

	err := r.db.QueryRow(ctx, query,
		job.ID,
		job.PatternID,
		job.ChunkID,
		string(job.Status),
		job.Attempts,
		job.MaxAttempts,
		job.LastError,
		scheduledForArg,
		job.StartedAt,
		job.CompletedAt,
	).Scan(&job.ScheduledFor, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case repository.PgErrCodeUniqueViolation:
				return ErrJobAlreadyPending
			case repository.PgErrCodeForeignKeyViolation:
				return ErrPatternNotFound
			case repository.PgErrCodeCheckViolation:
				return ErrInvalidJobTarget
			}
		}
		return fmt.Errorf("create enrichment job: %w", err)
	}

	return nil
}

// Get retrieves an enrichment job by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	query := `
		SELECT id, pattern_id, chunk_id, status, attempts, max_attempts,
			   last_error, scheduled_for, started_at, completed_at,
			   created_at, updated_at
		FROM enrichment_jobs
		WHERE id = $1
	`

	return r.scanJob(ctx, query, id)
}

// GetByPatternID retrieves the latest job for a pattern.
func (r *pgxRepository) GetByPatternID(ctx context.Context, patternID uuid.UUID) (*Job, error) {
	query := `
		SELECT id, pattern_id, chunk_id, status, attempts, max_attempts,
			   last_error, scheduled_for, started_at, completed_at,
			   created_at, updated_at
		FROM enrichment_jobs
		WHERE pattern_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	return r.scanJob(ctx, query, patternID)
}

// scanJob is a helper that executes a query and scans the result into a Job.
func (r *pgxRepository) scanJob(ctx context.Context, query string, args ...any) (*Job, error) {
	var job Job

	err := r.db.QueryRow(ctx, query, args...).Scan(
		&job.ID,
		&job.PatternID,
		&job.ChunkID,
		&job.Status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.LastError,
		&job.ScheduledFor,
		&job.StartedAt,
		&job.CompletedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan enrichment job: %w", err)
	}

	return &job, nil
}

// ClaimPending atomically claims a pending job for processing.
// Uses FOR UPDATE SKIP LOCKED for safe concurrent processing across multiple workers.
// Uses SQL now() for timestamps to ensure consistency with database time.
func (r *pgxRepository) ClaimPending(ctx context.Context) (*Job, error) {
	// Use FOR UPDATE SKIP LOCKED to safely claim a job without blocking
	// other workers. This query:
	// 1. Finds pending jobs that are ready to process (scheduled_for <= now)
	// 2. Orders by scheduled_for, then created_at for deterministic ordering
	// 3. Locks the row with SKIP LOCKED to prevent contention
	// 4. Updates status to processing and sets started_at
	query := `
		UPDATE enrichment_jobs
		SET status = $1, started_at = now(), updated_at = now()
		WHERE id = (
			SELECT id FROM enrichment_jobs
			WHERE status = $2 AND scheduled_for <= now()
			ORDER BY scheduled_for ASC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, pattern_id, chunk_id, status, attempts, max_attempts,
				  last_error, scheduled_for, started_at, completed_at,
				  created_at, updated_at
	`

	var job Job
	err := r.db.QueryRow(ctx, query,
		string(StatusProcessing),
		string(StatusPending),
	).Scan(
		&job.ID,
		&job.PatternID,
		&job.ChunkID,
		&job.Status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.LastError,
		&job.ScheduledFor,
		&job.StartedAt,
		&job.CompletedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No pending jobs available - this is not an error
			return nil, nil
		}
		return nil, err
	}

	return &job, nil
}

// MarkProcessing updates job status to processing with start time.
// Uses SQL now() for timestamps to ensure consistency with database time.
func (r *pgxRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE enrichment_jobs SET
			status = $2,
			started_at = now(),
			updated_at = now()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, string(StatusProcessing))
	if err != nil {
		return fmt.Errorf("mark job processing: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
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

// List retrieves enrichment jobs with filtering and pagination.
// Returns the jobs, total count, and any error.
func (r *pgxRepository) List(ctx context.Context, filter Filter, opts repository.ListOptions) ([]*Job, int64, error) {
	// Build the WHERE clause dynamically
	var conditions []string
	var args []any
	argIndex := 1

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.PatternID != nil {
		conditions = append(conditions, fmt.Sprintf("pattern_id = $%d", argIndex))
		args = append(args, *filter.PatternID)
		argIndex++
	}

	if filter.ChunkID != nil {
		conditions = append(conditions, fmt.Sprintf("chunk_id = $%d", argIndex))
		args = append(args, *filter.ChunkID)
		argIndex++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Build query with window function for total count
	query := fmt.Sprintf(`
		SELECT id, pattern_id, chunk_id, status, attempts, max_attempts,
			   last_error, scheduled_for, started_at, completed_at,
			   created_at, updated_at,
			   COUNT(*) OVER() as total_count
		FROM enrichment_jobs
		%s
		ORDER BY scheduled_for ASC, created_at ASC
	`, whereClause)

	// Add pagination
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, opts.Limit)
		argIndex++
		if opts.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argIndex)
			args = append(args, opts.Offset)
		}
	} else if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, opts.Offset)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	jobs := make([]*Job, 0)
	var totalCount int64

	for rows.Next() {
		var job Job

		err := rows.Scan(
			&job.ID,
			&job.PatternID,
			&job.ChunkID,
			&job.Status,
			&job.Attempts,
			&job.MaxAttempts,
			&job.LastError,
			&job.ScheduledFor,
			&job.StartedAt,
			&job.CompletedAt,
			&job.CreatedAt,
			&job.UpdatedAt,
			&totalCount,
		)
		if err != nil {
			return nil, 0, err
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return jobs, totalCount, nil
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
