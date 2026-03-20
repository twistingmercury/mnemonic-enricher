// Package pattern provides the business logic layer for pattern lifecycle management.
// It coordinates between the PostgreSQL pattern, enrichment job, and agent repositories,
// and the Neo4j graph repository, handling agent name resolution, enrichment job
// creation, and best-effort graph synchronization.
package pattern

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/repository"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/repository/chunk"
	enrichmentrepo "github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/mnemonic/internal/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	"github.com/twistingmercury/mnemonic/internal/service"
)

// Service defines the operations for managing pattern lifecycle.
// Agent associations use agent names as the external identifier; the service
// resolves names to UUIDs internally via the agent repository.
type Service interface {
	// Create stores a new pattern in Postgres, creates an enrichment job,
	// and best-effort syncs agent associations to Neo4j.
	// Returns service.ErrNotFound if any referenced agent name does not exist.
	// Returns service.ErrConflict if the pattern name already exists.
	Create(ctx context.Context, input CreateInput) (*patternrepo.Pattern, error)

	// Get retrieves a pattern by ID. Returns service.ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error)

	// GetWithGraph retrieves a pattern and, if enriched, its graph context
	// (related patterns and concepts). Neo4j failures degrade gracefully,
	// returning (pattern, nil, nil) instead of an error.
	GetWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *GraphContext, error)

	// Update modifies an existing pattern, creates a new enrichment job,
	// and best-effort syncs to Neo4j.
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (*patternrepo.Pattern, error)

	// Delete removes a pattern from Postgres (CASCADE handles associations and
	// jobs) and best-effort cleans up Neo4j.
	Delete(ctx context.Context, id uuid.UUID) error

	// List retrieves patterns with filtering and pagination.
	List(ctx context.Context, filter patternrepo.Filter, opts ListOptions) ([]*patternrepo.Pattern, int64, error)

	// SetAgentAssociations replaces all agent associations for a pattern.
	// Agent names are resolved to UUIDs; returns service.ErrNotFound if any
	// agent name does not exist.
	SetAgentAssociations(ctx context.Context, patternID uuid.UUID, associations []AssociationInput) error

	// GetAgentAssociations retrieves all agent associations for a pattern.
	GetAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]patternrepo.AgentAssociation, error)

	// ResolveAgentNames maps a slice of agent UUIDs to their human-readable
	// names. Unknown IDs are omitted from the result map without error.
	ResolveAgentNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)

	// FindRelated finds patterns related to the given pattern via the Neo4j
	// knowledge graph. Returns service.ErrNotFound if the pattern does not exist.
	FindRelated(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPatternResult, error)

	// ListChunks retrieves all chunks for a pattern, ordered by chunk_index.
	// Returns an empty slice when the pattern exists but has no chunks.
	// Returns service.ErrNotFound if the pattern does not exist.
	ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error)
}

// CreateInput contains fields for creating a pattern.
type CreateInput struct {
	Name              string
	Description       *string
	Content           string
	Tags              []string
	AgentAssociations []AssociationInput
	EntityType        string
	Language          string
	Domain            string
	Version           *string
	RelatedPatterns   []string
}

// UpdateInput contains fields for updating a pattern.
type UpdateInput struct {
	Name              string
	Description       *string
	Content           string
	Tags              []string
	AgentAssociations []AssociationInput
	EntityType        string
	Language          string
	Domain            string
	Version           *string
	RelatedPatterns   []string
}

// AssociationInput represents an agent association specified by agent name.
type AssociationInput struct {
	AgentName string
	Relevance float64
}

// GraphContext holds the knowledge graph context for a pattern.
type GraphContext struct {
	RelatedPatterns []RelatedPatternResult
	Concepts        []ConceptResult
}

// RelatedPatternResult represents a pattern discovered through shared concepts.
type RelatedPatternResult struct {
	ID             uuid.UUID
	Name           string
	Relationship   string
	Similarity     float64
	SharedConcepts []string
}

// ConceptResult represents a concept linked to a pattern.
type ConceptResult struct {
	Name string
	Type string
}

// ListOptions defines service-layer pagination parameters.
type ListOptions struct {
	Offset int
	Limit  int
}

// Compile-time interface check.
var _ Service = (*patternService)(nil)

// patternService implements the Service interface.
type patternService struct {
	patternRepo    patternrepo.Repository
	enrichmentRepo enrichmentrepo.Repository
	graphRepo      graphrepo.Repository
	agentRepo      agentrepo.Repository
	chunkRepo      chunkrepo.Repository
	pool           repository.TxBeginner
	logger         zerolog.Logger
}

// New creates a new pattern Service backed by the given repositories.
// chunkRepo may be nil during the transitional period; pass a real implementation
// once it is wired (Task 9).
func New(
	patternRepo patternrepo.Repository,
	enrichmentRepo enrichmentrepo.Repository,
	graphRepo graphrepo.Repository,
	agentRepo agentrepo.Repository,
	pool repository.TxBeginner,
	chunkRepo chunkrepo.Repository,
	logger zerolog.Logger,
) Service {
	return &patternService{
		patternRepo:    patternRepo,
		enrichmentRepo: enrichmentRepo,
		graphRepo:      graphRepo,
		agentRepo:      agentRepo,
		chunkRepo:      chunkRepo,
		pool:           pool,
		logger:         logger,
	}
}

// chunk is a parsed chunk from content.
type chunk struct {
	Title   string
	Content string
}

// splitIntoChunks parses markdown content and returns a chunk for each section
// preceded by a "[//]: pattern" decorator line. The heading following the
// decorator becomes the chunk title; all subsequent lines until the next
// decorator (or EOF) form the body. Lines outside decorated sections are
// discarded.
func splitIntoChunks(content string) []chunk {
	var chunks []chunk
	var currentTitle string
	var currentLines []string
	pendingPattern := false

	flush := func() {
		if currentTitle == "" {
			return
		}
		body := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if body != "" {
			chunks = append(chunks, chunk{Title: currentTitle, Content: body})
		}
		currentTitle = ""
		currentLines = nil
	}

	for line := range strings.SplitSeq(content, "\n") {
		if line == "[//]: pattern" {
			flush()
			pendingPattern = true
		} else if pendingPattern && strings.HasPrefix(line, "#") {
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentTitle = title
			currentLines = nil
			pendingPattern = false
		} else if currentTitle != "" {
			currentLines = append(currentLines, line)
		}
		// else: outside any decorated section — discard line
	}
	flush()

	return chunks
}

// Create stores a new pattern, sets agent associations, creates an enrichment
// job, and best-effort syncs to Neo4j.
func (s *patternService) Create(ctx context.Context, input CreateInput) (*patternrepo.Pattern, error) {
	// Resolve agent names to UUIDs before any mutations.
	resolvedAssocs, graphAssocs, err := s.resolveAgentAssociations(ctx, input.AgentAssociations)
	if err != nil {
		return nil, err
	}

	pattern := patternrepo.Pattern{
		Name:            input.Name,
		Description:     input.Description,
		Content:         input.Content,
		Tags:            input.Tags,
		EntityType:      input.EntityType,
		Language:        input.Language,
		Domain:          input.Domain,
		Version:         input.Version,
		RelatedPatterns: input.RelatedPatterns,
	}

	if err := s.patternRepo.Create(ctx, &pattern); err != nil {
		if errors.Is(err, patternrepo.ErrNameExists) {
			return nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, input.Name)
		}
		return nil, fmt.Errorf("create pattern: %w", err)
	}

	// Set agent associations if provided.
	if len(resolvedAssocs) > 0 {
		if err := s.patternRepo.SetAgentAssociations(ctx, pattern.ID, resolvedAssocs); err != nil {
			return nil, fmt.Errorf("create pattern: setting associations: %w", err)
		}
	}

	// Split content into chunks and create them (when chunk repo is wired).
	if s.chunkRepo != nil {
		rawChunks := splitIntoChunks(input.Content)
		chunks := make([]*chunkrepo.Chunk, len(rawChunks))
		for i, rc := range rawChunks {
			chunks[i] = &chunkrepo.Chunk{
				PatternID:    pattern.ID,
				SectionTitle: rc.Title,
				ChunkIndex:   i,
				Content:      rc.Content,
			}
		}
		if err := s.chunkRepo.CreateBatch(ctx, chunks); err != nil {
			return nil, fmt.Errorf("create pattern: creating chunks: %w", err)
		}

		// Create one enrichment job per chunk. Failures are best-effort: a
		// missed job means that chunk won't be embedded initially, but manual
		// re-enrichment or a future retry can recover it without data loss.
		var jobFailures int
		for _, c := range chunks {
			chunkID := c.ID
			job := enrichmentrepo.Job{ChunkID: &chunkID}
			if jobErr := s.enrichmentRepo.Create(ctx, &job); jobErr != nil {
				jobFailures++
			}
		}
		if jobFailures > 0 {
			s.logger.Warn().
				Int("failed", jobFailures).
				Int("total", len(chunks)).
				Msg("failed to create chunk enrichment jobs — affected chunks will not be embedded until re-PUT")
		}
	} else {
		// Fallback: create a single pattern-level enrichment job.
		pid := pattern.ID
		job := enrichmentrepo.Job{
			PatternID: &pid,
			Status:    enrichmentrepo.StatusPending,
		}
		if err := s.enrichmentRepo.Create(ctx, &job); err != nil {
			return nil, fmt.Errorf("create pattern: creating enrichment job: %w", err)
		}
	}

	// Best-effort Neo4j sync for agent associations.
	if len(graphAssocs) > 0 {
		s.syncNeo4j("pattern:agent-relevance:"+pattern.ID.String(), func() error {
			return s.graphRepo.SetPatternAgentRelevance(ctx, pattern.ID, graphAssocs)
		})
	}

	return &pattern, nil
}

// Get retrieves a pattern by ID.
func (s *patternService) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	pattern, err := s.patternRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return nil, fmt.Errorf("get pattern: %w", err)
	}
	return pattern, nil
}

// GetWithGraph retrieves a pattern and its graph context. If the pattern is not
// enriched or Neo4j is unavailable, the graph context is nil.
func (s *patternService) GetWithGraph(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, *GraphContext, error) {
	pattern, err := s.patternRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return nil, nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return nil, nil, fmt.Errorf("get pattern with graph: %w", err)
	}

	if pattern.EnrichmentStatus != "enriched" {
		return pattern, nil, nil
	}

	// Fetch graph context; degrade gracefully on Neo4j failure.
	graphCtx, err := s.fetchGraphContext(ctx, id)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("pattern_id", id.String()).
			Msg("failed to fetch graph context, returning without graph")
		return pattern, nil, nil
	}

	return pattern, graphCtx, nil
}

// Update modifies an existing pattern, triggers re-enrichment, and best-effort
// syncs to Neo4j.
func (s *patternService) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (*patternrepo.Pattern, error) {
	// Verify pattern exists.
	existing, err := s.patternRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return nil, fmt.Errorf("update pattern: %w", err)
	}

	// Resolve agent names to UUIDs.
	resolvedAssocs, graphAssocs, err := s.resolveAgentAssociations(ctx, input.AgentAssociations)
	if err != nil {
		return nil, err
	}

	// Build updated pattern preserving the ID.
	existing.Name = input.Name
	existing.Description = input.Description
	existing.Content = input.Content
	existing.Tags = input.Tags
	existing.EntityType = input.EntityType
	existing.Language = input.Language
	existing.Domain = input.Domain
	existing.Version = input.Version
	existing.RelatedPatterns = input.RelatedPatterns

	// Trigger re-enrichment. When a chunk repository is configured, perform the
	// four mutating writes (pattern update, agent associations, delete stale chunks,
	// create new chunks) inside a single Postgres transaction so a crash between
	// steps cannot leave chunks without enrichment jobs.
	// Without a chunk repository, fall back to a legacy pattern-level job.
	var newChunks []*chunkrepo.Chunk
	if s.chunkRepo != nil {
		var txErr error
		newChunks, txErr = s.updateWithTransaction(ctx, existing, resolvedAssocs)
		if txErr != nil {
			return nil, txErr
		}
	} else {
		if err := s.patternRepo.Update(ctx, existing); err != nil {
			if errors.Is(err, patternrepo.ErrNameExists) {
				return nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, input.Name)
			}
			return nil, fmt.Errorf("update pattern: %w", err)
		}

		// Set agent associations if provided.
		if len(resolvedAssocs) > 0 {
			if err := s.patternRepo.SetAgentAssociations(ctx, existing.ID, resolvedAssocs); err != nil {
				return nil, fmt.Errorf("update pattern: setting associations: %w", err)
			}
		}

		// Legacy path: pattern-level enrichment job.
		eid := existing.ID
		job := enrichmentrepo.Job{
			PatternID: &eid,
			Status:    enrichmentrepo.StatusPending,
		}
		if err := s.enrichmentRepo.Create(ctx, &job); err != nil {
			if !errors.Is(err, enrichmentrepo.ErrJobAlreadyPending) {
				return nil, fmt.Errorf("update pattern: creating enrichment job: %w", err)
			}
			// A pending job already exists; skip creating a duplicate.
		}
	}

	// Enqueue per-chunk enrichment jobs outside the transaction. Failures are
	// best-effort: a missed job can be recovered by manual re-enrichment.
	var jobFailures int
	for _, c := range newChunks {
		chunkID := c.ID
		job := enrichmentrepo.Job{
			ChunkID: &chunkID,
			Status:  enrichmentrepo.StatusPending,
		}
		if err := s.enrichmentRepo.Create(ctx, &job); err != nil {
			jobFailures++
		}
	}
	if jobFailures > 0 {
		s.logger.Warn().
			Int("failed", jobFailures).
			Int("total", len(newChunks)).
			Msg("failed to create chunk enrichment jobs — affected chunks will not be embedded until re-PUT")
	}

	// Best-effort Neo4j sync for agent associations.
	if len(graphAssocs) > 0 {
		s.syncNeo4j("pattern:agent-relevance:"+existing.ID.String(), func() error {
			return s.graphRepo.SetPatternAgentRelevance(ctx, existing.ID, graphAssocs)
		})
	}

	return existing, nil
}

// updateWithTransaction performs the four mutating writes for the chunk-aware
// Update path inside a single Postgres transaction:
//  1. Update the pattern row.
//  2. Replace agent associations (when provided).
//  3. Delete stale chunks (cascades to their enrichment jobs).
//  4. Insert new chunks.
//
// Returns the new chunks so the caller can enqueue enrichment jobs outside
// the transaction. The transaction is rolled back automatically if any step
// fails — the caller never sees a partial state.
func (s *patternService) updateWithTransaction(
	ctx context.Context,
	existing *patternrepo.Pattern,
	resolvedAssocs []patternrepo.AgentAssociation,
) ([]*chunkrepo.Chunk, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("update pattern: begin transaction: %w", err)
	}
	// Rollback is a no-op after a successful Commit (pgx guarantees this).
	defer func() { _ = tx.Rollback(ctx) }()

	// Step 1: update the pattern row.
	if err = s.patternRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, patternrepo.ErrNameExists) {
			return nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, existing.Name)
		}
		return nil, fmt.Errorf("update pattern: %w", err)
	}

	// Step 2: replace agent associations (conditional).
	if len(resolvedAssocs) > 0 {
		if err = s.patternRepo.SetAgentAssociations(ctx, existing.ID, resolvedAssocs); err != nil {
			return nil, fmt.Errorf("update pattern: setting associations: %w", err)
		}
	}

	// Step 3: delete stale chunks (cascades to their enrichment jobs via ON DELETE CASCADE).
	if err = s.chunkRepo.DeleteByPatternID(ctx, existing.ID); err != nil {
		return nil, fmt.Errorf("update pattern: delete stale chunks: %w", err)
	}

	// Step 4: re-split and insert new chunks.
	rawChunks := splitIntoChunks(existing.Content)
	newChunks := make([]*chunkrepo.Chunk, len(rawChunks))
	for i, rc := range rawChunks {
		newChunks[i] = &chunkrepo.Chunk{
			PatternID:    existing.ID,
			SectionTitle: rc.Title,
			ChunkIndex:   i,
			Content:      rc.Content,
		}
	}
	if err = s.chunkRepo.CreateBatch(ctx, newChunks); err != nil {
		return nil, fmt.Errorf("update pattern: create chunks: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update pattern: commit transaction: %w", err)
	}

	return newChunks, nil
}

// Delete removes a pattern from Postgres and best-effort cleans up Neo4j.
func (s *patternService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.patternRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return fmt.Errorf("delete pattern: %w", err)
	}

	// Best-effort Neo4j cleanup.
	s.syncNeo4j("pattern:delete:"+id.String(), func() error {
		return s.graphRepo.DeletePattern(ctx, id)
	})
	s.syncNeo4j("pattern:cleanup-orphans", func() error {
		_, err := s.graphRepo.CleanupOrphanedConcepts(ctx)
		return err
	})

	return nil
}

// List retrieves patterns with filtering and pagination.
func (s *patternService) List(ctx context.Context, filter patternrepo.Filter, opts ListOptions) ([]*patternrepo.Pattern, int64, error) {
	patterns, total, err := s.patternRepo.List(ctx, filter, repository.ListOptions{
		Offset: opts.Offset,
		Limit:  opts.Limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list patterns: %w", err)
	}
	return patterns, total, nil
}

// SetAgentAssociations replaces all agent associations for a pattern.
func (s *patternService) SetAgentAssociations(ctx context.Context, patternID uuid.UUID, associations []AssociationInput) error {
	resolvedAssocs, graphAssocs, err := s.resolveAgentAssociations(ctx, associations)
	if err != nil {
		return err
	}

	if err := s.patternRepo.SetAgentAssociations(ctx, patternID, resolvedAssocs); err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return fmt.Errorf("%w: pattern %s", service.ErrNotFound, patternID)
		}
		return fmt.Errorf("set agent associations: %w", err)
	}

	// Best-effort Neo4j sync.
	s.syncNeo4j("pattern:agent-relevance:"+patternID.String(), func() error {
		return s.graphRepo.SetPatternAgentRelevance(ctx, patternID, graphAssocs)
	})

	return nil
}

// GetAgentAssociations retrieves all agent associations for a pattern.
// Returns service.ErrNotFound if the pattern does not exist.
func (s *patternService) GetAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]patternrepo.AgentAssociation, error) {
	if _, err := s.patternRepo.Get(ctx, patternID); err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, patternID)
		}
		return nil, fmt.Errorf("get agent associations: %w", err)
	}

	assocs, err := s.patternRepo.GetAgentAssociations(ctx, patternID)
	if err != nil {
		return nil, fmt.Errorf("get agent associations: %w", err)
	}
	return assocs, nil
}

// ResolveAgentNames maps agent UUIDs to their human-readable names.
// Unknown IDs are silently omitted from the returned map.
func (s *patternService) ResolveAgentNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]string{}, nil
	}

	names := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		agent, err := s.agentRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, agentrepo.ErrNotFound) {
				s.logger.Warn().
					Str("agent_id", id.String()).
					Msg("agent not found during name resolution, omitting")
				continue
			}
			return nil, fmt.Errorf("resolving agent %s: %w", id, err)
		}
		names[id] = agent.Name
	}

	return names, nil
}

// FindRelated finds patterns related to the given pattern via the knowledge graph.
func (s *patternService) FindRelated(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPatternResult, error) {
	// Verify pattern exists.
	exists, err := s.patternRepo.Exists(ctx, patternID)
	if err != nil {
		return nil, fmt.Errorf("find related: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, patternID)
	}

	related, err := s.graphRepo.FindRelatedPatterns(ctx, patternID, limit)
	if err != nil {
		return nil, fmt.Errorf("find related: %w", err)
	}

	results := make([]RelatedPatternResult, len(related))
	for i, r := range related {
		results[i] = RelatedPatternResult{
			ID:             r.ID,
			Name:           r.Name,
			Relationship:   "RELATED_TO",
			Similarity:     r.Similarity,
			SharedConcepts: r.ConceptNames,
		}
	}

	return results, nil
}

// ListChunks retrieves all chunks for a pattern, ordered by chunk_index.
// Returns service.ErrNotFound if the pattern does not exist.
func (s *patternService) ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	if _, err := s.patternRepo.Get(ctx, patternID); err != nil {
		if errors.Is(err, patternrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, patternID)
		}
		return nil, fmt.Errorf("list chunks: %w", err)
	}

	if s.chunkRepo == nil {
		return []*chunkrepo.Chunk{}, nil
	}

	chunks, err := s.chunkRepo.ListByPatternID(ctx, patternID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	return chunks, nil
}

// resolveAgentAssociations converts agent names to UUIDs and builds both the
// Postgres and Neo4j association slices. Returns service.ErrNotFound if any
// agent name cannot be resolved.
func (s *patternService) resolveAgentAssociations(
	ctx context.Context,
	inputs []AssociationInput,
) ([]patternrepo.AgentAssociation, []graphrepo.AgentAssociation, error) {
	if len(inputs) == 0 {
		return nil, nil, nil
	}

	pgAssocs := make([]patternrepo.AgentAssociation, len(inputs))
	graphAssocs := make([]graphrepo.AgentAssociation, len(inputs))

	for i, input := range inputs {
		agent, err := s.agentRepo.Get(ctx, input.AgentName)
		if err != nil {
			if errors.Is(err, agentrepo.ErrNotFound) {
				return nil, nil, fmt.Errorf("%w: agent %q", service.ErrNotFound, input.AgentName)
			}
			return nil, nil, fmt.Errorf("resolving agent %q: %w", input.AgentName, err)
		}

		pgAssocs[i] = patternrepo.AgentAssociation{
			AgentID:   agent.ID,
			Relevance: input.Relevance,
		}
		graphAssocs[i] = graphrepo.AgentAssociation{
			AgentName: input.AgentName,
			Relevance: input.Relevance,
		}
	}

	return pgAssocs, graphAssocs, nil
}

// fetchGraphContext retrieves related patterns and concepts from Neo4j.
func (s *patternService) fetchGraphContext(ctx context.Context, patternID uuid.UUID) (*GraphContext, error) {
	related, err := s.graphRepo.FindRelatedPatterns(ctx, patternID, 10)
	if err != nil {
		return nil, fmt.Errorf("fetching related patterns: %w", err)
	}

	concepts, err := s.graphRepo.GetPatternConcepts(ctx, patternID)
	if err != nil {
		return nil, fmt.Errorf("fetching concepts: %w", err)
	}

	relatedResults := make([]RelatedPatternResult, len(related))
	for i, r := range related {
		relatedResults[i] = RelatedPatternResult{
			ID:             r.ID,
			Name:           r.Name,
			Relationship:   "RELATED_TO",
			Similarity:     r.Similarity,
			SharedConcepts: r.ConceptNames,
		}
	}

	conceptResults := make([]ConceptResult, len(concepts))
	for i, c := range concepts {
		conceptResults[i] = ConceptResult{
			Name: c.Name,
			Type: c.Type,
		}
	}

	return &GraphContext{
		RelatedPatterns: relatedResults,
		Concepts:        conceptResults,
	}, nil
}

// syncNeo4j runs fn as a best-effort Neo4j operation.
// Errors are logged but not returned to the caller.
func (s *patternService) syncNeo4j(entityDesc string, fn func() error) {
	if err := fn(); err != nil {
		s.logger.Warn().
			Err(err).
			Str("entity", entityDesc).
			Msg("neo4j sync failed")
	}
}
