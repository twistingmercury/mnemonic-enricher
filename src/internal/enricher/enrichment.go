package enricher

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/twistingmercury/mnemonic/internal/config"
	enrichmentjob "github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
	enrichmentsvc "github.com/twistingmercury/mnemonic/internal/service/enrichment"
)

// defaultMaintenanceInterval is the interval between maintenance loop iterations.
// Maintenance reclaims stale jobs and cleans up old completed/failed jobs.
const defaultMaintenanceInterval = 5 * time.Minute

// Worker polls for pending enrichment jobs and processes them using
// the EnrichmentService. It runs a configurable number of concurrent claim-process
// goroutines plus a single maintenance goroutine.
type Worker struct {
	svc    enrichmentsvc.Service
	cfg    config.EnrichmentConfig
	logger zerolog.Logger
}

// New creates a Worker that processes enrichment jobs using svc, configured by
// cfg. The logger is used for structured logging of worker lifecycle events and
// errors.
func New(svc enrichmentsvc.Service, cfg config.EnrichmentConfig, logger zerolog.Logger) *Worker {
	return &Worker{
		svc:    svc,
		cfg:    cfg,
		logger: logger.With().Str("component", "enrichment_worker").Logger(),
	}
}

// Run starts the worker pool and blocks until ctx is cancelled. It launches
// cfg.WorkerCount claim-process goroutines plus one maintenance goroutine.
//
// On shutdown (ctx cancellation), Run performs a two-phase graceful drain:
//  1. Stop claiming new jobs (workers exit their claim loops).
//  2. Wait for in-flight ProcessJob calls to complete, up to cfg.DrainTimeout.
//
// If the drain timeout expires, in-flight job contexts are cancelled and Run
// returns. Run always returns nil on shutdown; worker goroutines never crash
// the server.
func (w *Worker) Run(ctx context.Context) error {
	// drainCtx is intentionally derived from Background, not ctx — it must
	// remain live after ctx is cancelled to give in-flight jobs time to
	// complete before shutdown.
	drainCtx, drainCancel := context.WithCancel(context.Background())
	defer drainCancel()

	// inflight tracks the number of currently executing ProcessJob calls
	// so Run can wait for them during the drain phase.
	var inflight sync.WaitGroup

	// allDone tracks all goroutines (workers + maintenance) so Run can wait
	// for everything to finish before returning.
	var allDone sync.WaitGroup

	for i := range w.cfg.WorkerCount {
		workerID := i
		allDone.Add(1)
		go func() {
			defer allDone.Done()
			w.runWorker(ctx, drainCtx, workerID, &inflight)
		}()
	}

	allDone.Add(1)
	go func() {
		defer allDone.Done()
		w.runMaintenance(ctx)
	}()

	w.logger.Info().
		Int("worker_count", w.cfg.WorkerCount).
		Dur("poll_interval", w.cfg.PollInterval).
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Msg("enrichment worker started")

	// Block until the parent context is cancelled (shutdown signal).
	<-ctx.Done()

	// Drain phase: wait for in-flight ProcessJob calls to complete, bounded
	// by the drain timeout. Worker goroutines will stop claiming new jobs
	// (because ctx is cancelled) and will exit after their current
	// ProcessJob call finishes.
	w.logger.Info().
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Msg("draining in-flight jobs")

	w.drainInFlight(&inflight, drainCancel)

	// Wait for all goroutines to exit cleanly after drain completes.
	allDone.Wait()

	w.logger.Info().Msg("enrichment worker stopped")
	return nil
}

// drainInFlight waits for all in-flight ProcessJob calls tracked by wg to
// complete. If they do not finish within cfg.DrainTimeout, drainCancel is
// called to cancel the drain context, forcing in-flight calls to abort.
func (w *Worker) drainInFlight(wg *sync.WaitGroup, drainCancel context.CancelFunc) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		w.logger.Info().Msg("all in-flight jobs drained successfully")
	case <-time.After(w.cfg.DrainTimeout):
		w.logger.Warn().
			Dur("drain_timeout", w.cfg.DrainTimeout).
			Msg("drain timeout expired, cancelling in-flight jobs")
		drainCancel()
		// Wait for goroutines to finish after cancellation.
		<-done
	}
}

// runWorker is the claim-process loop for a single worker goroutine. It
// repeatedly claims and processes jobs until claimCtx is cancelled.
//
// When claimCtx is cancelled, the worker stops claiming new jobs. Any
// in-flight ProcessJob call uses drainCtx, which remains live during the
// drain phase so that the job can finish its work gracefully. The inflight
// WaitGroup tracks active ProcessJob calls for the drain phase.
func (w *Worker) runWorker(claimCtx, drainCtx context.Context, id int, inflight *sync.WaitGroup) {
	log := w.logger.With().Int("worker_id", id).Logger()
	log.Debug().Msg("worker goroutine started")

	for {
		select {
		case <-claimCtx.Done():
			log.Debug().Msg("worker goroutine stopping (no longer claiming jobs)")
			return
		default:
		}

		job, err := w.svc.ClaimNextJob(claimCtx)
		if err != nil {
			// Check for context cancellation to avoid noisy logging on shutdown.
			if claimCtx.Err() != nil {
				return
			}
			log.Error().Err(err).Msg("failed to claim job")
			w.sleep(claimCtx, w.cfg.PollInterval)
			continue
		}

		if job == nil {
			// No pending jobs; sleep before polling again.
			w.sleep(claimCtx, w.cfg.PollInterval)
			continue
		}

		logEvent := log.Info().Str("job_id", job.ID.String())
		if job.PatternID != nil {
			logEvent = logEvent.Str("pattern_id", job.PatternID.String())
		}
		if job.ChunkID != nil {
			logEvent = logEvent.Str("chunk_id", job.ChunkID.String())
		}
		logEvent.Msg("processing enrichment job")

		// Track the in-flight job for graceful drain. Use drainCtx for the
		// ProcessJob call so that in-flight work can complete even after
		// claimCtx is cancelled.
		inflight.Add(1)
		w.processJob(drainCtx, job, log)
		inflight.Done()
	}
}

// processJob runs the enrichment pipeline for a single job and logs the
// outcome. It uses the provided context, which during normal operation is the
// drain context (not the claim context) so that in-flight jobs can complete
// during shutdown.
func (w *Worker) processJob(ctx context.Context, job *enrichmentjob.Job, log zerolog.Logger) {
	if err := w.svc.ProcessJob(ctx, job); err != nil {
		// Non-nil error from ProcessJob means the failure could not be
		// recorded (unrecoverable). Log at error level.
		log.Error().
			Err(err).
			Str("job_id", job.ID.String()).
			Msg("enrichment job failed with unrecoverable error")
	} else {
		log.Info().
			Str("job_id", job.ID.String()).
			Msg("enrichment job completed")
	}
}

// runMaintenance periodically reclaims stale jobs and cleans up old
// completed/failed jobs. It runs until ctx is cancelled.
func (w *Worker) runMaintenance(ctx context.Context) {
	log := w.logger.With().Str("loop", "maintenance").Logger()
	log.Debug().Msg("maintenance goroutine started")

	ticker := time.NewTicker(defaultMaintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("maintenance goroutine stopping")
			return
		case <-ticker.C:
			w.doMaintenance(ctx, log)
		}
	}
}

// doMaintenance performs a single maintenance cycle: reclaim stale jobs,
// cleanup completed jobs, and cleanup failed jobs.
func (w *Worker) doMaintenance(ctx context.Context, log zerolog.Logger) {
	if ctx.Err() != nil {
		return
	}

	reclaimed, err := w.svc.ReclaimStaleJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to reclaim stale jobs")
	} else if reclaimed > 0 {
		log.Info().Int64("count", reclaimed).Msg("reclaimed stale jobs")
	}

	completedCleaned, err := w.svc.CleanupCompletedJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to cleanup completed jobs")
	} else if completedCleaned > 0 {
		log.Info().Int64("count", completedCleaned).Msg("cleaned up completed jobs")
	}

	failedCleaned, err := w.svc.CleanupFailedJobs(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to cleanup failed jobs")
	} else if failedCleaned > 0 {
		log.Info().Int64("count", failedCleaned).Msg("cleaned up failed jobs")
	}
}

// sleep blocks for the given duration or until ctx is cancelled, whichever
// comes first.
func (w *Worker) sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
