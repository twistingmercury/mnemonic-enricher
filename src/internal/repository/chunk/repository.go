package chunk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/twistingmercury/mnemonic/internal/repository"
)

// Compile-time interface check.
var _ Repository = (*pgxRepository)(nil)

// Repository defines data access operations for pattern chunks.
type Repository interface {
	// Create stores a new chunk, returning id, enrichment_status, created_at, updated_at.
	Create(ctx context.Context, c *Chunk) error

	// CreateBatch inserts multiple chunks in a single transaction.
	CreateBatch(ctx context.Context, chunks []*Chunk) error

	// Get retrieves a chunk by ID. Returns ErrNotFound when absent.
	Get(ctx context.Context, id uuid.UUID) (*Chunk, error)

	// ListByPatternID retrieves all chunks for a pattern, ordered by chunk_index.
	ListByPatternID(ctx context.Context, patternID uuid.UUID) ([]*Chunk, error)

	// DeleteByPatternID removes all chunks belonging to a pattern.
	DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error

	// UpdateEmbedding stores the embedding vector for a chunk.
	UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error

	// UpdateEnrichmentStatus updates the enrichment state of a chunk.
	// When status is "enriched", enriched_at is set to the current time.
	UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error

	// FindSimilar finds chunks similar to the given embedding vector.
	// Joins to patterns to return parent pattern metadata.
	FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error)

	// AllEnrichedForPattern returns true if every chunk for the pattern has status "enriched".
	AllEnrichedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error)

	// AnyFailedForPattern returns true if any chunk for the pattern has status "failed".
	AnyFailedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Create stores a new chunk in the database.
func (r *pgxRepository) Create(ctx context.Context, c *Chunk) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}

	query := `
		INSERT INTO pattern_chunks (
			id, pattern_id, section_title, chunk_index, content
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING enrichment_status, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		c.ID,
		c.PatternID,
		c.SectionTitle,
		c.ChunkIndex,
		c.Content,
	).Scan(&c.EnrichmentStatus, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating chunk: %w", err)
	}

	return nil
}

// CreateBatch inserts multiple chunks in a single transaction.
// When r.db is a *pgxpool.Pool it begins its own internal transaction.
// When r.db is a pgx.Tx the caller already owns the transaction, so the
// inserts are executed directly without creating a savepoint.
func (r *pgxRepository) CreateBatch(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Only begin an internal transaction when backed by a pool. pgx.Tx also
	// implements Begin (savepoints), but we must not create a nested savepoint
	// when the caller already owns the outer transaction.
	if pool, ok := r.db.(*pgxpool.Pool); ok {
		return r.createBatchWithTx(ctx, pool, chunks)
	}

	// Already in a transaction — execute directly.
	return r.createBatchDirect(ctx, r.db, chunks)
}

func (r *pgxRepository) createBatchWithTx(ctx context.Context, pool *pgxpool.Pool, chunks []*Chunk) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = r.createBatchDirect(ctx, tx, chunks); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func (r *pgxRepository) createBatchDirect(ctx context.Context, db repository.DBTX, chunks []*Chunk) error {
	query := `
		INSERT INTO pattern_chunks (
			id, pattern_id, section_title, chunk_index, content
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING enrichment_status, created_at, updated_at
	`

	for _, c := range chunks {
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}

		err := db.QueryRow(ctx, query,
			c.ID,
			c.PatternID,
			c.SectionTitle,
			c.ChunkIndex,
			c.Content,
		).Scan(&c.EnrichmentStatus, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return fmt.Errorf("creating chunk in batch: %w", err)
		}
	}

	return nil
}

// Get retrieves a chunk by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Chunk, error) {
	query := `
		SELECT id, pattern_id, section_title, chunk_index, content,
		       enrichment_status, enrichment_error, enriched_at,
		       created_at, updated_at
		FROM pattern_chunks
		WHERE id = $1
	`

	var c Chunk
	err := r.db.QueryRow(ctx, query, id).Scan(
		&c.ID,
		&c.PatternID,
		&c.SectionTitle,
		&c.ChunkIndex,
		&c.Content,
		&c.EnrichmentStatus,
		&c.EnrichmentError,
		&c.EnrichedAt,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting chunk: %w", err)
	}

	return &c, nil
}

// ListByPatternID retrieves all chunks for a pattern, ordered by chunk_index.
func (r *pgxRepository) ListByPatternID(ctx context.Context, patternID uuid.UUID) ([]*Chunk, error) {
	query := `
		SELECT id, pattern_id, section_title, chunk_index, content,
		       enrichment_status, enrichment_error, enriched_at,
		       created_at, updated_at
		FROM pattern_chunks
		WHERE pattern_id = $1
		ORDER BY chunk_index ASC
	`

	rows, err := r.db.Query(ctx, query, patternID)
	if err != nil {
		return nil, fmt.Errorf("listing chunks by pattern: %w", err)
	}
	defer rows.Close()

	chunks := make([]*Chunk, 0)

	for rows.Next() {
		var c Chunk
		err := rows.Scan(
			&c.ID,
			&c.PatternID,
			&c.SectionTitle,
			&c.ChunkIndex,
			&c.Content,
			&c.EnrichmentStatus,
			&c.EnrichmentError,
			&c.EnrichedAt,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("listing chunks by pattern: scanning row: %w", err)
		}
		chunks = append(chunks, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing chunks by pattern: iterating rows: %w", err)
	}

	return chunks, nil
}

// DeleteByPatternID removes all chunks belonging to a pattern.
func (r *pgxRepository) DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error {
	query := `DELETE FROM pattern_chunks WHERE pattern_id = $1`

	_, err := r.db.Exec(ctx, query, patternID)
	if err != nil {
		return fmt.Errorf("deleting chunks by pattern: %w", err)
	}

	return nil
}

// UpdateEmbedding stores the embedding vector for a chunk.
func (r *pgxRepository) UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error {
	now := time.Now()
	query := `
		UPDATE pattern_chunks SET
			embedding = $2,
			updated_at = $3
		WHERE id = $1
	`

	vec := pgvector.NewVector(embedding)
	result, err := r.db.Exec(ctx, query, id, vec, now)
	if err != nil {
		return fmt.Errorf("updating chunk embedding: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateEnrichmentStatus updates the enrichment state of a chunk.
// When status is "enriched", enriched_at is set to the current time.
func (r *pgxRepository) UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	if !repository.IsValidEnrichmentStatus(status) {
		return fmt.Errorf("invalid enrichment status: %q", status)
	}

	now := time.Now()

	var enrichedAt *time.Time
	if status == "enriched" {
		enrichedAt = &now
	}

	query := `
		UPDATE pattern_chunks SET
			enrichment_status = $2,
			enrichment_error = $3,
			enriched_at = $4,
			updated_at = $5
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, status, errMsg, enrichedAt, now)
	if err != nil {
		return fmt.Errorf("updating chunk enrichment status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// FindSimilar finds chunks similar to the given embedding vector.
// Joins to patterns to return parent pattern metadata.
func (r *pgxRepository) FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error) {
	vec := pgvector.NewVector(embedding)

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	// Build query dynamically to support optional PatternIDs and Tags filters.
	// Fixed parameters: $1 = embedding vector, $2 = min similarity, $3 = language, $4 = domain, $5 = limit.
	query := `
		SELECT pc.id, pc.pattern_id, p.name, p.entity_type, p.language, p.domain, p.tags,
		       pc.section_title, pc.chunk_index, pc.content,
		       1 - (pc.embedding <=> $1) AS similarity
		FROM pattern_chunks pc
		JOIN patterns p ON p.id = pc.pattern_id
		WHERE pc.embedding IS NOT NULL
		  AND pc.enrichment_status = 'enriched'
		  AND 1 - (pc.embedding <=> $1) >= $2
		  AND ($3::text = '' OR p.language = $3)
		  AND ($4::text = '' OR p.domain = $4)
	`

	args := []any{vec, opts.MinSimilarity, opts.Language, opts.Domain}
	nextParam := 5

	if len(opts.PatternIDs) > 0 {
		query += fmt.Sprintf(" AND pc.pattern_id = ANY($%d)", nextParam)
		args = append(args, opts.PatternIDs)
		nextParam++
	}

	if len(opts.Tags) > 0 {
		tagsJSON, err := json.Marshal(opts.Tags)
		if err != nil {
			return nil, fmt.Errorf("finding similar chunks: marshaling tags filter: %w", err)
		}
		query += fmt.Sprintf(" AND p.tags @> $%d::jsonb", nextParam)
		args = append(args, string(tagsJSON))
		nextParam++
	}

	query += fmt.Sprintf(" ORDER BY pc.embedding <=> $1 LIMIT $%d", nextParam)
	args = append(args, maxResults)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("finding similar chunks: %w", err)
	}
	defer rows.Close()

	matches := make([]*Match, 0)

	for rows.Next() {
		var m Match
		var tagsJSON []byte

		err := rows.Scan(
			&m.ChunkID,
			&m.PatternID,
			&m.PatternName,
			&m.EntityType,
			&m.Language,
			&m.Domain,
			&tagsJSON,
			&m.SectionTitle,
			&m.ChunkIndex,
			&m.Content,
			&m.Similarity,
		)
		if err != nil {
			return nil, fmt.Errorf("finding similar chunks: scanning row: %w", err)
		}

		if err := json.Unmarshal(tagsJSON, &m.Tags); err != nil {
			return nil, fmt.Errorf("finding similar chunks: unmarshaling tags: %w", err)
		}

		matches = append(matches, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finding similar chunks: iterating rows: %w", err)
	}

	return matches, nil
}

// AllEnrichedForPattern returns true if every chunk for the pattern has status "enriched"
// and the pattern has at least one chunk. Returns false for patterns with no chunks.
func (r *pgxRepository) AllEnrichedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error) {
	query := `
		SELECT COUNT(*) > 0
		   AND COUNT(*) FILTER (WHERE enrichment_status != 'enriched') = 0
		FROM pattern_chunks
		WHERE pattern_id = $1
	`

	var allEnriched bool
	err := r.db.QueryRow(ctx, query, patternID).Scan(&allEnriched)
	if err != nil {
		return false, fmt.Errorf("checking all enriched for pattern: %w", err)
	}

	return allEnriched, nil
}

// AnyFailedForPattern returns true if any chunk for the pattern has status "failed".
func (r *pgxRepository) AnyFailedForPattern(ctx context.Context, patternID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM pattern_chunks
			WHERE pattern_id = $1
			  AND enrichment_status = 'failed'
		)
	`

	var anyFailed bool
	err := r.db.QueryRow(ctx, query, patternID).Scan(&anyFailed)
	if err != nil {
		return false, fmt.Errorf("checking any failed for pattern: %w", err)
	}

	return anyFailed, nil
}
