package helpers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPostgresDSN = "postgres://mnemonic:mnemonic_dev@localhost:5432/mnemonic?sslmode=disable"

// NewPGConn creates a pgxpool.Pool from the POSTGRES_DSN env var.
// Falls back to the default local DSN if the env var is not set.
// Calls t.Fatal on connection error.
func NewPGConn(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultPostgresDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping postgres: %v", err)
	}

	return pool
}

// SeedPattern inserts a minimal pattern row and returns its ID.
// Uses a unique name to avoid conflicts. enrichment_status is set to "pending".
func SeedPattern(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	name := fmt.Sprintf("test-pattern-%s", uuid.New())

	_, err := pool.Exec(context.Background(), `
		INSERT INTO patterns (
			id, name, description, content, tags, entity_type, language, domain,
			version, related_patterns, enrichment_status, created_at, updated_at
		) VALUES (
			$1, $2, 'Test pattern description', 'Test pattern content',
			'[]'::jsonb, 'pattern', 'go', 'testing',
			'1.0.0', '[]'::jsonb, 'pending',
			NOW(), NOW()
		)`,
		id, name,
	)
	if err != nil {
		t.Fatalf("failed to seed pattern: %v", err)
	}

	return id
}

// SeedChunk inserts a chunk row linked to patternID and returns its ID.
func SeedChunk(t *testing.T, pool *pgxpool.Pool, patternID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()

	_, err := pool.Exec(context.Background(), `
		INSERT INTO pattern_chunks (
			id, pattern_id, section_title, chunk_index, content,
			enrichment_status, created_at, updated_at
		) VALUES (
			$1, $2, 'Test Section', 0, 'Test chunk content for enrichment.',
			'pending', NOW(), NOW()
		)`,
		id, patternID,
	)
	if err != nil {
		t.Fatalf("failed to seed chunk: %v", err)
	}

	return id
}

// SeedEnrichmentJob inserts an enrichment_job row with status="pending".
// Returns the job ID.
func SeedEnrichmentJob(t *testing.T, pool *pgxpool.Pool, chunkID, patternID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()

	_, err := pool.Exec(context.Background(), `
		INSERT INTO enrichment_jobs (
			id, pattern_id, chunk_id, status, attempts, max_attempts,
			scheduled_for, created_at, updated_at
		) VALUES (
			$1, $2, $3, 'pending', 0, 3,
			NOW(), NOW(), NOW()
		)`,
		id, patternID, chunkID,
	)
	if err != nil {
		t.Fatalf("failed to seed enrichment job: %v", err)
	}

	return id
}

// CleanupPattern deletes the pattern by ID. FK cascades handle chunks and jobs.
func CleanupPattern(t *testing.T, pool *pgxpool.Pool, patternID uuid.UUID) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		`DELETE FROM patterns WHERE id = $1`,
		patternID,
	)
	if err != nil {
		t.Fatalf("failed to cleanup pattern %s: %v", patternID, err)
	}
}

// PollJobStatus polls enrichment_jobs.status every 500ms until it reaches
// "completed" or "failed", or until timeout expires.
// Returns the final status string. Calls t.Fatal on timeout.
func PollJobStatus(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, timeout time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var status string
		var lastError *string

		err := pool.QueryRow(context.Background(),
			`SELECT status, last_error FROM enrichment_jobs WHERE id = $1`,
			jobID,
		).Scan(&status, &lastError)
		if err != nil {
			t.Fatalf("failed to query job status for %s: %v", jobID, err)
		}

		if status == "completed" || status == "failed" {
			if status == "failed" {
				errMsg := "<nil>"
				if lastError != nil {
					errMsg = *lastError
				}
				t.Fatalf("job %s failed: last_error=%s", jobID, errMsg)
			}
			return status
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for job %s to complete after %v", jobID, timeout)
	return ""
}

// PollJobStatusRaw polls enrichment_jobs until status is "completed" or "failed"
// or timeout expires. Returns (status, lastError). Does NOT call t.Fatal on "failed".
// Calls t.Fatal only on timeout or query error.
func PollJobStatusRaw(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, timeout time.Duration) (string, string) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var status string
		var lastError *string

		err := pool.QueryRow(context.Background(),
			`SELECT status, last_error FROM enrichment_jobs WHERE id = $1`,
			jobID,
		).Scan(&status, &lastError)
		if err != nil {
			t.Fatalf("failed to query job status for %s: %v", jobID, err)
		}

		if status == "completed" || status == "failed" {
			errMsg := ""
			if lastError != nil {
				errMsg = *lastError
			}
			return status, errMsg
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for job %s to complete after %v", jobID, timeout)
	return "", ""
}

// AssertChunkEmbeddingSet asserts that the chunk's embedding column is NOT NULL.
func AssertChunkEmbeddingSet(t *testing.T, pool *pgxpool.Pool, chunkID uuid.UUID) {
	t.Helper()

	var embeddingSet bool
	err := pool.QueryRow(context.Background(),
		`SELECT embedding IS NOT NULL FROM pattern_chunks WHERE id = $1`,
		chunkID,
	).Scan(&embeddingSet)
	if err != nil {
		t.Fatalf("failed to query embedding for chunk %s: %v", chunkID, err)
	}

	if !embeddingSet {
		t.Fatalf("expected embedding to be set for chunk %s, but it was NULL", chunkID)
	}
}
