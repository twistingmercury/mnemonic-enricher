package enricher_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/mnemonic-enricher/internal/config"
	"github.com/twistingmercury/mnemonic-enricher/internal/enricher"
	queue "github.com/twistingmercury/mnemonic-enricher/internal/queue"
	enrichmentjob "github.com/twistingmercury/mnemonic-enricher/internal/repository/enrichmentjob"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// concurrentService is a hand-written mock for tests that need dynamic return
// values. GetJob and ProcessJob are set via function fields.
type concurrentService struct {
	getJobFunc   func(ctx context.Context, id uuid.UUID) (*enrichmentjob.Job, error)
	processFunc  func(ctx context.Context, job *enrichmentjob.Job) error
}

func (s *concurrentService) GetJob(ctx context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
	if s.getJobFunc != nil {
		return s.getJobFunc(ctx, id)
	}
	return nil, nil
}

func (s *concurrentService) MarkJobProcessing(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (s *concurrentService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	if s.processFunc != nil {
		return s.processFunc(ctx, job)
	}
	return nil
}

func (s *concurrentService) ReclaimStaleJobs(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *concurrentService) CleanupCompletedJobs(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *concurrentService) CleanupFailedJobs(_ context.Context) (int64, error) {
	return 0, nil
}

// mockService implements enrichmentsvc.Service for testing via testify/mock.
type mockService struct {
	mock.Mock
}

func (m *mockService) GetJob(ctx context.Context, jobID uuid.UUID) (*enrichmentjob.Job, error) {
	args := m.Called(ctx, jobID)
	job, _ := args.Get(0).(*enrichmentjob.Job)
	return job, args.Error(1)
}

func (m *mockService) MarkJobProcessing(ctx context.Context, jobID uuid.UUID) error {
	return m.Called(ctx, jobID).Error(0)
}

func (m *mockService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	args := m.Called(ctx, job)
	return args.Error(0)
}

func (m *mockService) ReclaimStaleJobs(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockService) CleanupCompletedJobs(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockService) CleanupFailedJobs(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// mockSubscriber implements queue.Subscriber for testing. It emits the
// configured deliveries then closes the channel (or blocks once exhausted,
// closing when ctx is done).
type mockSubscriber struct {
	deliveries []queue.Delivery
	name       string
	mu         sync.Mutex
	idx        int
	closed     bool
}

func (s *mockSubscriber) Subscribe(ctx context.Context) (<-chan queue.Delivery, error) {
	out := make(chan queue.Delivery)
	go func() {
		defer close(out)
		for {
			s.mu.Lock()
			if s.idx < len(s.deliveries) {
				d := s.deliveries[s.idx]
				s.idx++
				s.mu.Unlock()
				select {
				case out <- d:
				case <-ctx.Done():
					return
				}
			} else {
				s.mu.Unlock()
				// All deliveries exhausted — block until ctx is done.
				<-ctx.Done()
				return
			}
		}
	}()
	return out, nil
}

func (s *mockSubscriber) Name() string { return s.name }

func (s *mockSubscriber) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testConfig returns an EnrichmentConfig with fast intervals for testing.
func testConfig(workerCount int) config.EnrichmentConfig {
	return config.EnrichmentConfig{
		WorkerCount:            workerCount,
		MaxAttempts:            3,
		RetryDelay:             10 * time.Millisecond,
		JobTimeout:             1 * time.Second,
		DrainTimeout:           500 * time.Millisecond,
		CompletedRetention:     1 * time.Hour,
		FailedRetention:        1 * time.Hour,
		RelatedToMinSimilarity: 0.3,
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

// newTestJob creates a Job with a random ID and pattern ID.
func newTestJob() *enrichmentjob.Job {
	pid := uuid.New()
	return &enrichmentjob.Job{
		ID:        uuid.New(),
		PatternID: &pid,
		Status:    enrichmentjob.StatusProcessing,
		Attempts:  1,
	}
}

// newPatternOnlyJob creates a Job with PatternID set and ChunkID nil.
func newPatternOnlyJob() *enrichmentjob.Job {
	pid := uuid.New()
	return &enrichmentjob.Job{
		ID:        uuid.New(),
		PatternID: &pid,
		ChunkID:   nil,
		Status:    enrichmentjob.StatusProcessing,
		Attempts:  1,
	}
}

// newChunkOnlyJob creates a Job with ChunkID set and PatternID nil.
func newChunkOnlyJob() *enrichmentjob.Job {
	cid := uuid.New()
	return &enrichmentjob.Job{
		ID:        uuid.New(),
		PatternID: nil,
		ChunkID:   &cid,
		Status:    enrichmentjob.StatusProcessing,
		Attempts:  1,
	}
}

// newBothIDsJob creates a Job with both PatternID and ChunkID set.
func newBothIDsJob() *enrichmentjob.Job {
	pid := uuid.New()
	cid := uuid.New()
	return &enrichmentjob.Job{
		ID:        uuid.New(),
		PatternID: &pid,
		ChunkID:   &cid,
		Status:    enrichmentjob.StatusProcessing,
		Attempts:  1,
	}
}

// makeDelivery builds a queue.Delivery whose Body encodes the given job ID and
// whose Ack/Nack callbacks record whether they were called.
func makeDelivery(jobID uuid.UUID, ackCalled, nackCalled *atomic.Bool) queue.Delivery {
	body, _ := json.Marshal(map[string]string{"job_id": jobID.String()})
	return queue.Delivery{
		Body: body,
		Ack: func() error {
			ackCalled.Store(true)
			return nil
		},
		Nack: func(_ bool) error {
			nackCalled.Store(true)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWorkerProcessesAvailableJobs(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(nil)
	svc.On("ProcessJob", mock.Anything, job).Return(nil)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.True(t, ackCalled.Load(), "delivery should be acked after successful processing")
	assert.False(t, nackCalled.Load(), "delivery should not be nacked on success")
	svc.AssertCalled(t, "ProcessJob", mock.Anything, job)
}

func TestWorkerNacksOnGetJobFailure(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(nil, errors.New("db error"))
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.False(t, ackCalled.Load(), "delivery should not be acked when GetJob fails")
	assert.True(t, nackCalled.Load(), "delivery should be nacked when GetJob fails")
}

func TestWorkerNacksOnMarkJobProcessingFailure(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(errors.New("state error"))
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.False(t, ackCalled.Load(), "delivery should not be acked when MarkJobProcessing fails")
	assert.True(t, nackCalled.Load(), "delivery should be nacked when MarkJobProcessing fails")
}

func TestWorkerNacksOnProcessJobFailure(t *testing.T) {
	t.Parallel()

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(nil)
	svc.On("ProcessJob", mock.Anything, job).Return(errors.New("pipeline error"))
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.False(t, ackCalled.Load(), "delivery should not be acked when ProcessJob returns error")
	assert.True(t, nackCalled.Load(), "delivery should be nacked when ProcessJob returns unrecoverable error")
}

func TestWorkerNacksOnInvalidDeliveryBody(t *testing.T) {
	t.Parallel()

	var nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name: "test-queue",
		deliveries: []queue.Delivery{
			{
				Body: []byte("not json"),
				Ack:  func() error { return nil },
				Nack: func(_ bool) error { nackCalled.Store(true); return nil },
			},
		},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.True(t, nackCalled.Load(), "delivery with invalid body should be nacked")
}

func TestWorkerGracefulShutdown(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	// No deliveries; worker will block until ctx is cancelled.
	sub := &mockSubscriber{name: "test-queue"}

	w := enricher.New(svc, sub, testConfig(2), testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Give the worker time to start, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "Run should return nil on graceful shutdown")
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}

func TestMultipleWorkersConcurrency(t *testing.T) {
	t.Parallel()

	jobs := make([]*enrichmentjob.Job, 4)
	deliveries := make([]queue.Delivery, 4)
	for i := range jobs {
		jobs[i] = newTestJob()
		var ack, nack atomic.Bool
		deliveries[i] = makeDelivery(jobs[i].ID, &ack, &nack)
	}

	var processedCount atomic.Int32

	svc := &concurrentService{
		getJobFunc: func(_ context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
			for _, j := range jobs {
				if j.ID == id {
					return j, nil
				}
			}
			return nil, errors.New("job not found")
		},
		processFunc: func(_ context.Context, _ *enrichmentjob.Job) error {
			processedCount.Add(1)
			return nil
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: deliveries,
	}

	w := enricher.New(svc, sub, testConfig(2), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int32(4), processedCount.Load(), "all jobs should be processed")
}

func TestMaintenanceLoopRuns(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	// Since we cannot inject the maintenance interval, we verify the maintenance
	// goroutine does not prevent shutdown. The actual maintenance calls happen on
	// a 5-minute ticker, which is too slow for unit tests. We verify the wiring
	// works by testing the exported Run method completes cleanly.
	sub := &mockSubscriber{name: "test-queue"}
	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err, "worker should shut down cleanly even with maintenance goroutine")
}

func TestNewReturnsNonNil(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	sub := &mockSubscriber{name: "test-queue"}
	w := enricher.New(svc, sub, testConfig(2), testLogger())
	assert.NotNil(t, w)
}

// --- Graceful drain tests ---

func TestGracefulDrainWaitsForInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that when the context is cancelled while a job is
	// being processed, Run waits for the in-flight job to complete before
	// returning.

	var (
		processStarted = make(chan struct{})
		processDone    atomic.Bool
	)

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := &concurrentService{
		getJobFunc: func(_ context.Context, _ uuid.UUID) (*enrichmentjob.Job, error) {
			return job, nil
		},
		processFunc: func(ctx context.Context, _ *enrichmentjob.Job) error {
			close(processStarted)
			// Simulate a long-running job (100ms).
			select {
			case <-time.After(100 * time.Millisecond):
				processDone.Store(true)
				return nil
			case <-ctx.Done():
				// If the drain context is cancelled, the job was not given
				// enough time.
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	cfg := testConfig(1)
	cfg.DrainTimeout = 2 * time.Second // Generous drain timeout.

	w := enricher.New(svc, sub, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for the job to start processing, then cancel.
	<-processStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		assert.True(t, processDone.Load(), "in-flight job should have completed before Run returned")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

func TestGracefulDrainTimeoutCancelsInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that if in-flight jobs take longer than the drain
	// timeout, their context is cancelled and Run returns.

	var (
		processStarted   = make(chan struct{}, 1)
		contextCancelled atomic.Bool
	)

	job := newTestJob()
	var ackCalled, nackCalled atomic.Bool

	svc := &concurrentService{
		getJobFunc: func(_ context.Context, _ uuid.UUID) (*enrichmentjob.Job, error) {
			return job, nil
		},
		processFunc: func(ctx context.Context, _ *enrichmentjob.Job) error {
			select {
			case processStarted <- struct{}{}:
			default:
			}
			// Simulate a very long job that exceeds the drain timeout.
			select {
			case <-time.After(10 * time.Second):
				return nil
			case <-ctx.Done():
				contextCancelled.Store(true)
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	cfg := testConfig(1)
	cfg.DrainTimeout = 50 * time.Millisecond // Short drain timeout.

	w := enricher.New(svc, sub, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for the job to start processing, then cancel.
	<-processStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		assert.True(t, contextCancelled.Load(), "drain timeout should have cancelled the in-flight job's context")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

func TestGracefulDrainMultipleInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that the drain phase waits for multiple in-flight
	// jobs across multiple workers.

	const workerCount = 3

	var (
		allStarted = make(chan struct{})
		started    atomic.Int32
		completed  atomic.Int32
	)

	jobs := make([]*enrichmentjob.Job, workerCount)
	deliveries := make([]queue.Delivery, workerCount)
	for i := range jobs {
		jobs[i] = newTestJob()
		var ack, nack atomic.Bool
		deliveries[i] = makeDelivery(jobs[i].ID, &ack, &nack)
	}

	svc := &concurrentService{
		getJobFunc: func(_ context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
			for _, j := range jobs {
				if j.ID == id {
					return j, nil
				}
			}
			return nil, errors.New("job not found")
		},
		processFunc: func(ctx context.Context, _ *enrichmentjob.Job) error {
			count := started.Add(1)
			if int(count) == workerCount {
				close(allStarted)
			}
			// Simulate work that takes 100ms.
			select {
			case <-time.After(100 * time.Millisecond):
				completed.Add(1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: deliveries,
	}

	cfg := testConfig(workerCount)
	cfg.DrainTimeout = 2 * time.Second

	w := enricher.New(svc, sub, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for all workers to start processing, then cancel.
	<-allStarted
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		require.Equal(t, int32(workerCount), completed.Load(),
			"all in-flight jobs should complete during drain")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

// --- Nil-pointer guard tests for PatternID / ChunkID ---

// TestRunWorkerPatternOnlyJobNoPanic verifies that a job with PatternID set and
// ChunkID nil is processed without panicking.
func TestRunWorkerPatternOnlyJobNoPanic(t *testing.T) {
	t.Parallel()

	job := newPatternOnlyJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(nil)
	svc.On("ProcessJob", mock.Anything, job).Return(nil)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, ackCalled.Load(), "pattern-only job should have been acked")
}

// TestRunWorkerChunkOnlyJobNoPanic verifies that a job with ChunkID set and
// PatternID nil is processed without panicking.
func TestRunWorkerChunkOnlyJobNoPanic(t *testing.T) {
	t.Parallel()

	job := newChunkOnlyJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(nil)
	svc.On("ProcessJob", mock.Anything, job).Return(nil)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, ackCalled.Load(), "chunk-only job should have been acked")
}

// TestRunWorkerBothIDsJobNoPanic verifies that a job with both PatternID and
// ChunkID set is processed without panicking.
func TestRunWorkerBothIDsJobNoPanic(t *testing.T) {
	t.Parallel()

	job := newBothIDsJob()
	var ackCalled, nackCalled atomic.Bool

	svc := new(mockService)
	svc.On("GetJob", mock.Anything, job.ID).Return(job, nil)
	svc.On("MarkJobProcessing", mock.Anything, job.ID).Return(nil)
	svc.On("ProcessJob", mock.Anything, job).Return(nil)
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	sub := &mockSubscriber{
		name:       "test-queue",
		deliveries: []queue.Delivery{makeDelivery(job.ID, &ackCalled, &nackCalled)},
	}

	w := enricher.New(svc, sub, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, ackCalled.Load(), "job with both IDs should have been acked")
}
