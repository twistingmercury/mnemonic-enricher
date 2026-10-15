package enricher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic-enricher/internal/config"
	"github.com/twistingmercury/mnemonic-enricher/internal/metrics"
	queue "github.com/twistingmercury/mnemonic-enricher/internal/queue"
	enrichmentsvc "github.com/twistingmercury/mnemonic-enricher/internal/service/enrichment"
)

// defaultMaintenanceInterval is the interval between maintenance loop iterations.
// Maintenance reclaims stale jobs and cleans up old completed/failed jobs.
const defaultMaintenanceInterval = 5 * time.Minute

var tracer = otel.Tracer("mnemonic/enricher")

// jobMessage is the JSON payload delivered by the queue for each enrichment job.
type jobMessage struct {
	JobID uuid.UUID `json:"job_id"`
}

// Worker consumes enrichment job messages from a queue.Subscriber and processes
// them using the EnrichmentService. A configurable semaphore bounds concurrent
// job goroutines, and a single maintenance goroutine handles periodic cleanup.
type Worker struct {
	svc        enrichmentsvc.Service
	sub        queue.Subscriber
	cfg        config.EnrichmentConfig
	logger     zerolog.Logger
	jobMetrics *metrics.Jobs
}

// New creates a Worker that processes enrichment jobs using svc, consuming
// messages from sub, configured by cfg. The logger is used for structured
// logging of worker lifecycle events and errors. jobMetrics, when non-nil,
// records the count and duration of every handled delivery.
func New(svc enrichmentsvc.Service, sub queue.Subscriber, cfg config.EnrichmentConfig, logger zerolog.Logger, jobMetrics *metrics.Jobs) *Worker {
	return &Worker{
		svc:        svc,
		sub:        sub,
		cfg:        cfg,
		logger:     logger.With().Str("component", "enrichment_worker").Logger(),
		jobMetrics: jobMetrics,
	}
}

// Run subscribes to the queue and fans out deliveries to bounded goroutines.
// It blocks until ctx is cancelled.
//
// On shutdown (ctx cancellation), Run performs a two-phase graceful drain:
//  1. Stop consuming new deliveries (the subscriber stops emitting when ctx is cancelled).
//  2. Wait for in-flight handleDelivery calls to complete, up to cfg.DrainTimeout.
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

	// inflight tracks the number of currently executing handleDelivery calls
	// so Run can wait for them during the drain phase.
	var inflight sync.WaitGroup

	// allDone tracks all goroutines (delivery handlers + maintenance) so Run
	// can wait for everything to finish before returning.
	var allDone sync.WaitGroup

	// semaphore limits the number of concurrently executing delivery handlers.
	semaphore := make(chan struct{}, w.cfg.WorkerCount)

	deliveries, err := w.sub.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("enrichment worker: subscribe: %w", err)
	}

	allDone.Add(1)
	go func() {
		defer allDone.Done()
		w.runMaintenance(ctx)
	}()

	w.logger.Info().
		Int("worker_count", w.cfg.WorkerCount).
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Str("queue", w.sub.Name()).
		Msg("enrichment worker started")

	// Fan-out: for each delivery, acquire a semaphore slot and process in a
	// goroutine. The loop exits when the deliveries channel is closed (which
	// happens when ctx is cancelled by the subscriber implementation).
	for d := range deliveries {
		d := d // capture loop variable
		semaphore <- struct{}{}
		inflight.Add(1)
		allDone.Add(1)
		go func() {
			defer func() {
				<-semaphore
				inflight.Done()
				allDone.Done()
			}()
			w.handleDelivery(drainCtx, d)
		}()
	}

	// Drain phase: wait for in-flight handleDelivery calls to complete, bounded
	// by the drain timeout.
	w.logger.Info().
		Dur("drain_timeout", w.cfg.DrainTimeout).
		Msg("draining in-flight jobs")

	w.drainInFlight(&inflight, drainCancel)

	if err := w.sub.Close(); err != nil {
		w.logger.Error().Err(err).Msg("error closing subscriber")
	}

	// Wait for all goroutines (maintenance + delivery handlers) to exit.
	allDone.Wait()

	w.logger.Info().Msg("enrichment worker stopped")
	return nil
}

// handleDelivery unmarshals a queue delivery, starts the job, and runs the
// enrichment pipeline. It calls d.Ack when the pipeline ran (successfully or
// with a recorded failure) and d.Nack(false) on unrecoverable errors (parse
// failures, start failures, unrecorded pipeline failures). drainCtx remains live through the
// drain phase so that in-flight work can complete gracefully after the main
// ctx is cancelled.
//
// Each delivery gets a consumer span that continues the producer's trace from
// the message headers, and one record in the job metrics.
func (w *Worker) handleDelivery(drainCtx context.Context, d queue.Delivery) {
	start := time.Now()
	ctx := otel.GetTextMapPropagator().Extract(drainCtx, propagation.MapCarrier(d.Headers))
	ctx, span := tracer.Start(ctx, "process "+w.sub.Name(),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", w.sub.Name()),
		),
	)

	outcome := metrics.OutcomeRejected
	var spanErr error
	defer func() {
		if spanErr != nil {
			span.RecordError(spanErr)
			span.SetStatus(codes.Error, spanErr.Error())
		}
		span.End()
		if w.jobMetrics != nil {
			w.jobMetrics.Record(ctx, outcome, time.Since(start))
		}
	}()

	log := w.logger
	if sc := span.SpanContext(); sc.IsValid() {
		log = log.With().Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String()).Logger()
	}

	// Parse the job message.
	var msg jobMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		spanErr = err
		log.Error().Err(err).Msg("failed to parse delivery body; nacking")
		if nackErr := d.Nack(false); nackErr != nil {
			log.Error().Err(nackErr).Msg("nack failed after parse error")
		}
		return
	}

	jobID := msg.JobID
	span.SetAttributes(attribute.String("mnemonic.job_id", jobID.String()))
	log = log.With().Str("job_id", jobID.String()).Logger()

	// Mark the job as processing and load what it targets.
	job, err := w.svc.StartJob(ctx, jobID)
	if err != nil {
		spanErr = err
		log.Error().Err(err).Msg("failed to start job; nacking")
		if nackErr := d.Nack(false); nackErr != nil {
			log.Error().Err(nackErr).Msg("nack failed after StartJob error")
		}
		return
	}

	logEvent := log.Info()
	if job.PatternID != nil {
		span.SetAttributes(attribute.String("mnemonic.pattern_id", job.PatternID.String()))
		logEvent = logEvent.Str("pattern_id", job.PatternID.String())
	}
	if job.ChunkID != nil {
		span.SetAttributes(attribute.String("mnemonic.chunk_id", job.ChunkID.String()))
		logEvent = logEvent.Str("chunk_id", job.ChunkID.String())
	}
	logEvent.Msg("processing enrichment job")

	// Run the enrichment pipeline.
	err = w.svc.ProcessJob(ctx, job)
	switch {
	case err == nil:
		outcome = metrics.OutcomeCompleted
		log.Info().Msg("enrichment job completed")
	case errors.Is(err, enrichmentsvc.ErrJobFailed):
		// The failure is recorded on the job for retry, so the message is done.
		outcome, spanErr = metrics.OutcomeFailed, err
		log.Warn().Err(err).Msg("enrichment job failed; failure recorded")
	default:
		// The failure could not be recorded (unrecoverable). Nack without
		// requeue so we don't loop on a job stuck in an unrecoverable state.
		outcome, spanErr = metrics.OutcomeError, err
		log.Error().Err(err).Msg("enrichment job failed with unrecoverable error")
		if nackErr := d.Nack(false); nackErr != nil {
			log.Error().Err(nackErr).Msg("nack failed after ProcessJob error")
		}
		return
	}

	if ackErr := d.Ack(); ackErr != nil {
		log.Error().Err(ackErr).Msg("ack failed after processing")
	}
}

// drainInFlight waits for all in-flight handleDelivery calls tracked by wg to
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
