// Package enrichment provides the business logic layer for processing enrichment
// jobs. It orchestrates the enrichment pipeline: claiming jobs, running embedding
// generation, concept extraction, Neo4j graph sync, and handling failures with
// retry scheduling.
//
// The enrichment worker goroutine calls these methods; REST/MCP handlers do not.
//
// Documentation:
//   - Design: docs/design/service-layer.md (EnrichmentService, Enrichment Job Lifecycle)
//   - Design: docs/design/pattern-processing.md (Enrichment Pipeline)
package enrichment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/config"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/repository/chunk"
	enrichmentjob "github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/mnemonic/internal/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	openaisvc "github.com/twistingmercury/mnemonic/internal/service/openai"
)

// errPipelineFailed is a sentinel returned by runGraphPipeline when a pipeline
// step fails but the failure was successfully recorded (via failJob). It signals
// callers to stop processing and return nil to the worker.
var errPipelineFailed = errors.New("pipeline step failed; failure recorded")

// Service defines the worker-facing interface for processing enrichment jobs.
// The enrichment worker goroutine calls these methods; REST/MCP handlers do not.
type Service interface {
	// ClaimNextJob atomically claims the next pending enrichment job.
	// Returns (nil, nil) if no jobs are available.
	ClaimNextJob(ctx context.Context) (*enrichmentjob.Job, error)

	// ProcessJob runs the full enrichment pipeline for a claimed job:
	//   1. Load pattern from Postgres
	//   2. Generate embedding via EmbeddingService
	//   3. Store embedding in Postgres
	//   4. Extract concepts via ExtractionService
	//   5. Sync pattern node to Neo4j
	//   6. Sync concepts and MENTIONED_IN edges to Neo4j
	//   7. Sync agent associations to Neo4j
	//   8. Compute RELATED_TO edges
	//   9. Mark job completed and pattern enriched
	//
	// On failure at any pipeline step, marks the job as failed with error detail.
	// Returns nil when a step fails but the failure is recorded successfully.
	// Returns a non-nil error only for unrecoverable system errors (e.g., failure
	// to mark the job as failed or to update enrichment status).
	ProcessJob(ctx context.Context, job *enrichmentjob.Job) error

	// ReclaimStaleJobs resets jobs stuck in processing state back to pending.
	// Called periodically by the worker lifecycle manager.
	ReclaimStaleJobs(ctx context.Context) (int64, error)

	// CleanupCompletedJobs removes completed jobs older than the retention period.
	CleanupCompletedJobs(ctx context.Context) (int64, error)

	// CleanupFailedJobs removes failed jobs older than the retention period.
	CleanupFailedJobs(ctx context.Context) (int64, error)
}

// enrichmentService implements the Service interface.
type enrichmentService struct {
	jobRepo       enrichmentjob.Repository
	patternRepo   patternrepo.Repository
	agentRepo     agentrepo.Repository
	graphRepo     graphrepo.Repository
	chunkRepo     chunkrepo.Repository
	embeddingSvc  openaisvc.EmbeddingService
	extractionSvc openaisvc.ExtractionService
	cfg           config.EnrichmentConfig
	logger        zerolog.Logger
}

// New creates a new enrichment Service backed by the given dependencies.
// Returns an error if any required dependency is nil.
func New(
	jobRepo enrichmentjob.Repository,
	patternRepo patternrepo.Repository,
	agentRepo agentrepo.Repository,
	graphRepo graphrepo.Repository,
	embeddingSvc openaisvc.EmbeddingService,
	extractionSvc openaisvc.ExtractionService,
	cfg config.EnrichmentConfig,
	chunkRepo chunkrepo.Repository,
	logger zerolog.Logger,
) (Service, error) {
	if jobRepo == nil {
		return nil, fmt.Errorf("enrichment.New: jobRepo is required")
	}
	if patternRepo == nil {
		return nil, fmt.Errorf("enrichment.New: patternRepo is required")
	}
	if agentRepo == nil {
		return nil, fmt.Errorf("enrichment.New: agentRepo is required")
	}
	if graphRepo == nil {
		return nil, fmt.Errorf("enrichment.New: graphRepo is required")
	}
	if embeddingSvc == nil {
		return nil, fmt.Errorf("enrichment.New: embeddingSvc is required")
	}
	if extractionSvc == nil {
		return nil, fmt.Errorf("enrichment.New: extractionSvc is required")
	}
	if chunkRepo == nil {
		return nil, fmt.Errorf("enrichment.New: chunkRepo is required")
	}
	return &enrichmentService{
		jobRepo:       jobRepo,
		patternRepo:   patternRepo,
		agentRepo:     agentRepo,
		graphRepo:     graphRepo,
		chunkRepo:     chunkRepo,
		embeddingSvc:  embeddingSvc,
		extractionSvc: extractionSvc,
		cfg:           cfg,
		logger:        logger,
	}, nil
}

// ClaimNextJob atomically claims the next pending enrichment job.
// Returns (nil, nil) if no jobs are available.
func (s *enrichmentService) ClaimNextJob(ctx context.Context) (*enrichmentjob.Job, error) {
	return s.jobRepo.ClaimPending(ctx)
}

// ProcessJob dispatches to the chunk-based or pattern-based pipeline based on
// which ID field the job carries.
func (s *enrichmentService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	if job.ChunkID != nil {
		return s.processChunkJob(ctx, job)
	}
	return s.processPatternJob(ctx, job)
}

// processChunkJob runs the chunk-based enrichment pipeline:
//  1. Load chunk from repository
//  2. Load parent pattern from repository
//  3. Build enriched embedding text (name | tags | section_title + content)
//  4. Generate embedding via OpenAI
//  5. Update chunk embedding in repository
//  6. Update chunk enrichment status
//  7. Update enrichment job status
func (s *enrichmentService) processChunkJob(ctx context.Context, job *enrichmentjob.Job) error {
	// Step 1: Load chunk from Postgres.
	chunk, err := s.chunkRepo.Get(ctx, *job.ChunkID)
	if err != nil {
		return s.failJob(ctx, job, fmt.Errorf("load chunk: %w", err))
	}

	// Step 2: Load parent pattern to build enriched embed text.
	pattern, err := s.patternRepo.Get(ctx, chunk.PatternID)
	if err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("load pattern for chunk: %w", err))
	}

	// Step 3: Generate embedding for enriched chunk text.
	// Prepend pattern name, tags (when present), and section title so the embedding
	// captures semantic context beyond the raw code/prose of the section body.
	var embedText string
	if len(pattern.Tags) > 0 {
		tags := strings.Join(pattern.Tags, ", ")
		embedText = fmt.Sprintf("%s | %s | %s\n\n%s", pattern.Name, tags, chunk.SectionTitle, chunk.Content)
	} else {
		embedText = fmt.Sprintf("%s | %s\n\n%s", pattern.Name, chunk.SectionTitle, chunk.Content)
	}
	embedding, err := s.embeddingSvc.Embed(ctx, embedText)
	if err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("embed chunk: %w", err))
	}

	// Step 4: Store embedding on the chunk row.
	if err := s.chunkRepo.UpdateEmbedding(ctx, chunk.ID, embedding); err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("store chunk embedding: %w", err))
	}

	// Step 5: Mark chunk enriched.
	if err := s.chunkRepo.UpdateEnrichmentStatus(ctx, chunk.ID, "enriched", nil); err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("update chunk enrichment status: %w", err))
	}

	// Step 6: Check aggregate status.
	anyFailed, err := s.chunkRepo.AnyFailedForPattern(ctx, chunk.PatternID)
	if err != nil {
		// Aggregate query failure: transient DB issue should not block this chunk's
		// successful completion. Log and continue without updating pattern status.
		s.logger.Error().
			Err(err).
			Str("pattern_id", chunk.PatternID.String()).
			Msg("failed to check any-failed aggregate; continuing")
	} else if anyFailed {
		errMsg := "one or more chunks failed enrichment"
		if updateErr := s.patternRepo.UpdateEnrichmentStatus(ctx, chunk.PatternID, "failed", &errMsg); updateErr != nil {
			s.logger.Error().
				Err(updateErr).
				Str("pattern_id", chunk.PatternID.String()).
				Msg("failed to mark pattern as failed after chunk failure")
		}
		// This chunk succeeded; another chunk caused the aggregate failure.
		// Mark job completed and return.
		if markErr := s.jobRepo.MarkCompleted(ctx, job.ID); markErr != nil {
			return fmt.Errorf("mark job completed: %w", markErr)
		}
		return nil
	}

	allEnriched, err := s.chunkRepo.AllEnrichedForPattern(ctx, chunk.PatternID)
	if err != nil {
		// Aggregate query failure: transient DB issue should not block this chunk's
		// successful completion. Log and continue without updating pattern status.
		s.logger.Error().
			Err(err).
			Str("pattern_id", chunk.PatternID.String()).
			Msg("failed to check all-enriched aggregate; continuing")
	} else if allEnriched {
		// AllEnrichedForPattern cannot be vacuously true here: at minimum the chunk
		// this job processed exists, so the count is >= 1.
		//
		// All chunks are enriched: run concept extraction + graph sync.
		// runGraphPipeline returns errPipelineFailed when a step fails and
		// the failure was recorded, or a real error when recording failed.
		// In either failure case we must not call MarkCompleted.
		if pipeErr := s.runGraphPipeline(ctx, job, pattern); pipeErr != nil {
			if errors.Is(pipeErr, errPipelineFailed) {
				// Failure was recorded successfully; return nil to the worker.
				return nil
			}
			// Unrecoverable error recording the failure.
			return pipeErr
		}
		// All graph steps succeeded; mark pattern enriched.
		if updateErr := s.patternRepo.UpdateEnrichmentStatus(ctx, chunk.PatternID, "enriched", nil); updateErr != nil {
			return fmt.Errorf("update enrichment status: %w", updateErr)
		}
	}

	// Step 7: Mark job completed.
	if err := s.jobRepo.MarkCompleted(ctx, job.ID); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	return nil
}

// runGraphPipeline extracts concepts from a pattern and syncs the Neo4j graph.
// The caller is responsible for loading the pattern and passing it in; this
// avoids a redundant Postgres round-trip when the pattern was already fetched
// earlier in the pipeline.
//
// On a pipeline step failure it calls failJob to record the failure and wraps
// the result in errPipelineFailed (or returns the unrecoverable error directly).
// Callers must check errors.Is(err, errPipelineFailed) to distinguish a
// successfully-recorded failure from an unrecoverable recording error.
func (s *enrichmentService) runGraphPipeline(ctx context.Context, job *enrichmentjob.Job, pattern *patternrepo.Pattern) error {
	recordFail := func(cause error) error {
		if err := s.failJob(ctx, job, cause); err != nil {
			return err // unrecoverable
		}
		return errPipelineFailed // recorded successfully
	}

	// Extract concepts via OpenAI.
	concepts, err := s.extractionSvc.Extract(ctx, pattern.Content)
	if err != nil {
		return recordFail(fmt.Errorf("extract concepts: %w", err))
	}

	// Sync pattern node to Neo4j.
	graphPattern := &graphrepo.Pattern{
		ID:          pattern.ID,
		Name:        pattern.Name,
		Description: pattern.Description,
	}
	if err := s.graphRepo.SyncPattern(ctx, graphPattern); err != nil {
		return recordFail(fmt.Errorf("sync pattern to neo4j: %w", err))
	}

	// Sync concepts and MENTIONED_IN edges to Neo4j.
	// Explicit mapping between openai.Concept and graphrepo.Concept is intentional:
	// the types are structurally similar but belong to different packages with
	// different responsibilities (extraction vs. graph storage).
	graphConcepts := make([]graphrepo.Concept, len(concepts))
	for i, c := range concepts {
		graphConcepts[i] = graphrepo.Concept{Name: c.Name, Type: c.Type}
	}
	if err := s.graphRepo.SyncConcepts(ctx, pattern.ID, graphConcepts); err != nil {
		return recordFail(fmt.Errorf("sync concepts to neo4j: %w", err))
	}

	// Sync agent associations to Neo4j.
	// patternrepo.AgentAssociation has AgentID (UUID), but Neo4j Agent nodes are
	// keyed by name. Resolve agent names via agentRepo before syncing.
	graphAssocs, err := s.resolveAgentAssociations(ctx, pattern.ID)
	if err != nil {
		return recordFail(fmt.Errorf("get agent associations: %w", err))
	}
	if err := s.graphRepo.SetPatternAgentRelevance(ctx, pattern.ID, graphAssocs); err != nil {
		return recordFail(fmt.Errorf("sync associations to neo4j: %w", err))
	}

	// Compute RELATED_TO edges based on shared concepts.
	if err := s.graphRepo.ComputeRelatedToEdges(ctx, pattern.ID, s.cfg.RelatedToMinSimilarity); err != nil {
		return recordFail(fmt.Errorf("compute related_to: %w", err))
	}

	return nil
}

// processPatternJob runs the legacy pattern-level enrichment pipeline for jobs
// where PatternID is set and ChunkID is nil.
func (s *enrichmentService) processPatternJob(ctx context.Context, job *enrichmentjob.Job) error {
	// Step 1: Validate job has a pattern ID.
	if job.PatternID == nil {
		return s.failJob(ctx, job, fmt.Errorf("load pattern: job has no pattern_id"))
	}

	// Step 1: Load pattern from Postgres.
	pattern, err := s.patternRepo.Get(ctx, *job.PatternID)
	if err != nil {
		if failErr := s.failJob(ctx, job, fmt.Errorf("load pattern: %w", err)); failErr != nil {
			return failErr
		}
		return nil
	}

	// Steps 2-3: Per-chunk embedding pipeline replaces pattern-level embedding (Task 6).

	// Steps 4-8: Extract concepts and sync graph.
	if err := s.runGraphPipeline(ctx, job, pattern); err != nil {
		if errors.Is(err, errPipelineFailed) {
			// Failure recorded successfully; return nil to the worker.
			return nil
		}
		// Unrecoverable error recording the pipeline failure.
		return err
	}

	// Step 9: Update pattern enrichment status (unrecoverable if this fails).
	if err := s.patternRepo.UpdateEnrichmentStatus(ctx, *job.PatternID, "enriched", nil); err != nil {
		return fmt.Errorf("update enrichment status: %w", err)
	}

	// Step 10: Mark job completed (unrecoverable if this fails).
	if err := s.jobRepo.MarkCompleted(ctx, job.ID); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	return nil
}

// failChunkJob marks both the chunk and its parent pattern as failed, then
// marks the job failed. Use this when a chunk has been loaded (so PatternID
// is known) but a subsequent step fails.
func (s *enrichmentService) failChunkJob(ctx context.Context, job *enrichmentjob.Job, chunkID uuid.UUID, patternID uuid.UUID, cause error) error {
	errMsg := cause.Error()
	if err := s.chunkRepo.UpdateEnrichmentStatus(ctx, chunkID, "failed", &errMsg); err != nil {
		s.logger.Error().
			Err(err).
			Str("chunk_id", chunkID.String()).
			Msg("failed to update chunk enrichment status")
	}
	if err := s.patternRepo.UpdateEnrichmentStatus(ctx, patternID, "failed", &errMsg); err != nil {
		s.logger.Error().
			Err(err).
			Str("pattern_id", patternID.String()).
			Msg("failed to update pattern enrichment status")
	}
	return s.jobRepo.MarkFailed(ctx, job.ID, cause, s.cfg.RetryDelay)
}

// resolveAgentAssociations loads pattern-agent associations from Postgres and
// resolves agent UUIDs to names for Neo4j sync. Agents that cannot be resolved
// are logged and skipped.
func (s *enrichmentService) resolveAgentAssociations(ctx context.Context, patternID uuid.UUID) ([]graphrepo.AgentAssociation, error) {
	associations, err := s.patternRepo.GetAgentAssociations(ctx, patternID)
	if err != nil {
		return nil, err
	}

	graphAssocs := make([]graphrepo.AgentAssociation, 0, len(associations))
	for _, a := range associations {
		agent, err := s.agentRepo.GetByID(ctx, a.AgentID)
		if err != nil {
			s.logger.Warn().
				Err(err).
				Str("agent_id", a.AgentID.String()).
				Msg("skip association: agent not found")
			continue
		}
		graphAssocs = append(graphAssocs, graphrepo.AgentAssociation{
			AgentName: agent.Name,
			Relevance: a.Relevance,
		})
	}

	return graphAssocs, nil
}

// failJob marks the job as failed and updates the pattern enrichment status.
// Returns nil on success (the failure is recorded). Returns an error only when
// the failure itself cannot be recorded (unrecoverable).
func (s *enrichmentService) failJob(ctx context.Context, job *enrichmentjob.Job, cause error) error {
	if err := s.jobRepo.MarkFailed(ctx, job.ID, cause, s.cfg.RetryDelay); err != nil {
		s.logger.Error().
			Err(err).
			Str("job_id", job.ID.String()).
			Msg("failed to mark job as failed")
		return fmt.Errorf("mark job failed: %w (original cause: %v)", err, cause)
	}

	errMsg := cause.Error()
	if job.PatternID != nil {
		if err := s.patternRepo.UpdateEnrichmentStatus(ctx, *job.PatternID, "failed", &errMsg); err != nil {
			s.logger.Error().
				Err(err).
				Str("pattern_id", job.PatternID.String()).
				Msg("failed to update pattern enrichment status")
			return fmt.Errorf("update enrichment status: %w (original cause: %v)", err, cause)
		}
	}

	return nil
}

// ReclaimStaleJobs resets jobs stuck in processing state back to pending.
func (s *enrichmentService) ReclaimStaleJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.ReclaimStale(ctx, s.cfg.JobTimeout)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale jobs: %w", err)
	}
	return count, nil
}

// CleanupCompletedJobs removes completed jobs older than the retention period.
func (s *enrichmentService) CleanupCompletedJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.DeleteCompleted(ctx, s.cfg.CompletedRetention)
	if err != nil {
		return 0, fmt.Errorf("cleanup completed jobs: %w", err)
	}
	return count, nil
}

// CleanupFailedJobs removes failed jobs older than the retention period.
func (s *enrichmentService) CleanupFailedJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.DeleteFailed(ctx, s.cfg.FailedRetention)
	if err != nil {
		return 0, fmt.Errorf("cleanup failed jobs: %w", err)
	}
	return count, nil
}

// Compile-time check that *enrichmentService implements Service.
var _ Service = (*enrichmentService)(nil)

// Compile-time check that New returns (Service, error).
var _ func(
	enrichmentjob.Repository,
	patternrepo.Repository,
	agentrepo.Repository,
	graphrepo.Repository,
	openaisvc.EmbeddingService,
	openaisvc.ExtractionService,
	config.EnrichmentConfig,
	chunkrepo.Repository,
	zerolog.Logger,
) (Service, error) = New
