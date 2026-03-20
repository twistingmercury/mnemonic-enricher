package enrichmentjob

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// Job represents a background enrichment task stored in the database.
type Job struct {
	// ID is the unique identifier for the job.
	ID uuid.UUID `db:"id"`

	// PatternID references the pattern to enrich. Nil for chunk-based jobs.
	PatternID *uuid.UUID `db:"pattern_id"`

	// ChunkID references the chunk to enrich. Nil for legacy pattern-level jobs.
	ChunkID *uuid.UUID `db:"chunk_id"`

	// Status is the job processing state.
	// Valid values: pending, processing, completed, failed
	Status JobStatus `db:"status"`

	// Attempts is the number of times processing has been attempted.
	Attempts int `db:"attempts"`

	// MaxAttempts is the maximum number of retries before giving up.
	MaxAttempts int `db:"max_attempts"`

	// LastError is the error message from the most recent failed attempt.
	LastError *string `db:"last_error"`

	// ScheduledFor is when the job should be processed (supports delayed retry).
	ScheduledFor time.Time `db:"scheduled_for"`

	// StartedAt is the timestamp when processing began.
	StartedAt *time.Time `db:"started_at"`

	// CompletedAt is the timestamp when processing finished successfully.
	CompletedAt *time.Time `db:"completed_at"`

	// CreatedAt is the timestamp when the job was created.
	CreatedAt time.Time `db:"created_at"`

	// UpdatedAt is the timestamp when the job was last modified.
	UpdatedAt time.Time `db:"updated_at"`
}

// JobStatus represents the valid job status values.
type JobStatus string

const (
	// StatusPending indicates the job is awaiting processing.
	StatusPending JobStatus = "pending"

	// StatusProcessing indicates the job is currently being processed by a worker.
	StatusProcessing JobStatus = "processing"

	// StatusCompleted indicates the job finished successfully.
	StatusCompleted JobStatus = "completed"

	// StatusFailed indicates processing failed (see LastError).
	StatusFailed JobStatus = "failed"
)

// ValidStatuses defines the valid values for the Status field.
var ValidStatuses = []JobStatus{
	StatusPending,
	StatusProcessing,
	StatusCompleted,
	StatusFailed,
}

// IsValidStatus checks if the given status is valid.
func IsValidStatus(status JobStatus) bool {
	return slices.Contains(ValidStatuses, status)
}

// IsPending returns true if the job is in pending state.
func (j *Job) IsPending() bool {
	return j.Status == StatusPending
}

// IsProcessing returns true if the job is currently being processed.
func (j *Job) IsProcessing() bool {
	return j.Status == StatusProcessing
}

// IsCompleted returns true if the job completed successfully.
func (j *Job) IsCompleted() bool {
	return j.Status == StatusCompleted
}

// IsFailed returns true if the job failed.
func (j *Job) IsFailed() bool {
	return j.Status == StatusFailed
}

// CanRetry returns true if the job can be retried.
func (j *Job) CanRetry() bool {
	return j.Attempts < j.MaxAttempts
}

// DefaultMaxAttempts is the default maximum number of attempts for a job.
const DefaultMaxAttempts = 3

// Filter defines filtering options for job queries.
type Filter struct {
	// Status filters by job status (pending, processing, completed, failed).
	Status *string

	// PatternID filters by the associated pattern ID.
	PatternID *uuid.UUID

	// ChunkID filters jobs by chunk_id.
	ChunkID *uuid.UUID
}
