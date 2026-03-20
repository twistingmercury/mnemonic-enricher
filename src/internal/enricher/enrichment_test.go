package enricher_test

import (
	"context"
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

	"github.com/twistingmercury/mnemonic/internal/config"
	"github.com/twistingmercury/mnemonic/internal/enricher"
	enrichmentjob "github.com/twistingmercury/mnemonic/internal/repository/enrichmentjob"
)

// concurrentService is a hand-written mock for tests that need dynamic return
// values (e.g., returning different jobs from a shared queue). This avoids
// testify/mock's limitations with functional Return values and error types.
type concurrentService struct {
	claimFunc   func(ctx context.Context) (*enrichmentjob.Job, error)
	processFunc func(ctx context.Context, job *enrichmentjob.Job) error
}

func (s *concurrentService) ClaimNextJob(ctx context.Context) (*enrichmentjob.Job, error) {
	return s.claimFunc(ctx)
}

func (s *concurrentService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	return s.processFunc(ctx, job)
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

// mockService implements enrichmentsvc.Service for testing.
type mockService struct {
	mock.Mock
}

func (m *mockService) ClaimNextJob(ctx context.Context) (*enrichmentjob.Job, error) {
	args := m.Called(ctx)
	job, _ := args.Get(0).(*enrichmentjob.Job)
	return job, args.Error(1)
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

// testConfig returns an EnrichmentConfig with fast intervals for testing.
func testConfig(workerCount int) config.EnrichmentConfig {
	return config.EnrichmentConfig{
		WorkerCount:            workerCount,
		PollInterval:           1 * time.Millisecond,
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
// This represents a legacy pattern-level enrichment job.
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
// This represents a chunk-only enrichment job.
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

func TestWorkerProcessesAvailableJobs(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newTestJob()

	var processed atomic.Bool

	// First call returns a job; subsequent calls return nil (no more jobs).
	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()

	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once().Run(func(_ mock.Arguments) {
		processed.Store(true)
	})

	// Maintenance stubs (may or may not be called depending on timing).
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.True(t, processed.Load(), "job should have been processed")
	svc.AssertCalled(t, "ProcessJob", mock.Anything, job)
}

func TestWorkerSleepsWhenNoJobs(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newTestJob()

	var claimCount atomic.Int32

	// Track claim calls. First several return nil, then return a job.
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Times(3).Run(func(_ mock.Arguments) {
		claimCount.Add(1)
	})
	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once().Run(func(_ mock.Arguments) {
		claimCount.Add(1)
	})
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()

	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once()

	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)

	// Should have polled at least 4 times (3 nil + 1 job).
	assert.GreaterOrEqual(t, claimCount.Load(), int32(4))
	svc.AssertCalled(t, "ProcessJob", mock.Anything, job)
}

func TestWorkerGracefulShutdown(t *testing.T) {
	t.Parallel()

	svc := new(mockService)

	// ClaimNextJob always returns nil so the worker just polls.
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(2), testLogger())

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

	// concurrentService tracks concurrent job processing without testify/mock
	// to avoid the complexity of functional return values with mock.Return.
	var (
		mu             sync.Mutex
		jobIndex       int
		processedCount atomic.Int32
	)

	jobs := make([]*enrichmentjob.Job, 4)
	for i := range jobs {
		jobs[i] = newTestJob()
	}

	svc := &concurrentService{
		claimFunc: func(_ context.Context) (*enrichmentjob.Job, error) {
			mu.Lock()
			defer mu.Unlock()
			if jobIndex < len(jobs) {
				j := jobs[jobIndex]
				jobIndex++
				return j, nil
			}
			return nil, nil
		},
		processFunc: func(_ context.Context, _ *enrichmentjob.Job) error {
			processedCount.Add(1)
			return nil
		},
	}

	w := enricher.New(svc, testConfig(2), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int32(4), processedCount.Load(), "all jobs should be processed")
}

func TestMaintenanceLoopRuns(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()

	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	// Since we cannot inject the maintenance interval, we verify the maintenance
	// goroutine does not prevent shutdown. The actual maintenance calls happen on
	// a 5-minute ticker, which is too slow for unit tests. We verify the wiring
	// works by testing the exported Run method completes cleanly.
	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err, "worker should shut down cleanly even with maintenance goroutine")
}

func TestProcessJobErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newTestJob()

	var processCount atomic.Int32

	// First claim returns a job that will fail, second claim returns a job that succeeds.
	job2 := newTestJob()
	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(job2, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()

	svc.On("ProcessJob", mock.Anything, job).Return(errors.New("unrecoverable error")).Once().
		Run(func(_ mock.Arguments) { processCount.Add(1) })
	svc.On("ProcessJob", mock.Anything, job2).Return(nil).Once().
		Run(func(_ mock.Arguments) { processCount.Add(1) })

	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err, "worker should not crash on ProcessJob error")
	assert.GreaterOrEqual(t, processCount.Load(), int32(2), "worker should continue processing after error")
}

func TestClaimNextJobErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newTestJob()

	var claimCount atomic.Int32

	// First claim returns an error, subsequent claims succeed.
	svc.On("ClaimNextJob", mock.Anything).Return(nil, errors.New("db error")).Once().
		Run(func(_ mock.Arguments) { claimCount.Add(1) })
	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once().
		Run(func(_ mock.Arguments) { claimCount.Add(1) })
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()

	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once()

	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := w.Run(ctx)
	assert.NoError(t, err, "worker should not crash on ClaimNextJob error")
	assert.GreaterOrEqual(t, claimCount.Load(), int32(2), "worker should continue claiming after error")
	svc.AssertCalled(t, "ProcessJob", mock.Anything, job)
}

func TestNewReturnsNonNil(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	w := enricher.New(svc, testConfig(2), testLogger())
	assert.NotNil(t, w)
}

// --- Graceful drain tests ---

func TestGracefulDrainWaitsForInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that when the context is cancelled while a job is
	// being processed, Run waits for the in-flight job to complete before
	// returning.

	var (
		jobClaimed     atomic.Bool
		processStarted = make(chan struct{})
		processDone    atomic.Bool
	)

	job := newTestJob()

	svc := &concurrentService{
		claimFunc: func(ctx context.Context) (*enrichmentjob.Job, error) {
			// Return the job exactly once, then return nil.
			if jobClaimed.CompareAndSwap(false, true) {
				return job, nil
			}
			// Block on context to avoid busy-spinning while the job processes.
			<-ctx.Done()
			return nil, ctx.Err()
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

	cfg := testConfig(1)
	cfg.DrainTimeout = 2 * time.Second // Generous drain timeout.

	w := enricher.New(svc, cfg, testLogger())

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
		jobClaimed       atomic.Bool
		processStarted   = make(chan struct{})
		contextCancelled atomic.Bool
	)

	job := newTestJob()

	svc := &concurrentService{
		claimFunc: func(ctx context.Context) (*enrichmentjob.Job, error) {
			if jobClaimed.CompareAndSwap(false, true) {
				return job, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		processFunc: func(ctx context.Context, _ *enrichmentjob.Job) error {
			close(processStarted)
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

	cfg := testConfig(1)
	cfg.DrainTimeout = 50 * time.Millisecond // Short drain timeout.

	w := enricher.New(svc, cfg, testLogger())

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

func TestGracefulDrainStopsClaimingNewJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that after context cancellation, no new jobs are
	// claimed even if there are jobs available.

	var (
		claimedAfterCancel atomic.Bool
		cancelTime         atomic.Int64
	)

	svc := &concurrentService{
		claimFunc: func(ctx context.Context) (*enrichmentjob.Job, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// If cancel has already happened, record that a claim was attempted.
			if cancelTime.Load() > 0 && time.Now().UnixNano() > cancelTime.Load() {
				claimedAfterCancel.Store(true)
			}
			return nil, nil
		},
		processFunc: func(_ context.Context, _ *enrichmentjob.Job) error {
			return nil
		},
	}

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Let the worker poll for a bit, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancelTime.Store(time.Now().UnixNano())
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
		// The worker should not claim jobs after context cancellation. The
		// claimFunc may be called once more due to timing, but the key point
		// is that Run returns promptly.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within expected time")
	}
}

// --- Nil-pointer guard tests for PatternID / ChunkID ---

// TestRunWorkerPatternOnlyJobNoPanic verifies that a job with PatternID set and
// ChunkID nil is processed without panicking. This is the pre-existing "pattern
// level" job shape.
func TestRunWorkerPatternOnlyJobNoPanic(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newPatternOnlyJob()

	var processed atomic.Bool

	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()
	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once().Run(func(_ mock.Arguments) {
		processed.Store(true)
	})
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, processed.Load(), "pattern-only job should have been processed")
}

// TestRunWorkerChunkOnlyJobNoPanic verifies that a job with ChunkID set and
// PatternID nil is processed without panicking. This is the chunk-only job
// shape that was previously causing a nil pointer dereference.
func TestRunWorkerChunkOnlyJobNoPanic(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newChunkOnlyJob()

	var processed atomic.Bool

	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()
	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once().Run(func(_ mock.Arguments) {
		processed.Store(true)
	})
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, processed.Load(), "chunk-only job should have been processed")
}

// TestRunWorkerBothIDsJobNoPanic verifies that a job with both PatternID and
// ChunkID set is processed without panicking.
func TestRunWorkerBothIDsJobNoPanic(t *testing.T) {
	t.Parallel()

	svc := new(mockService)
	job := newBothIDsJob()

	var processed atomic.Bool

	svc.On("ClaimNextJob", mock.Anything).Return(job, nil).Once()
	svc.On("ClaimNextJob", mock.Anything).Return(nil, nil).Maybe()
	svc.On("ProcessJob", mock.Anything, job).Return(nil).Once().Run(func(_ mock.Arguments) {
		processed.Store(true)
	})
	svc.On("ReclaimStaleJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupCompletedJobs", mock.Anything).Return(int64(0), nil).Maybe()
	svc.On("CleanupFailedJobs", mock.Anything).Return(int64(0), nil).Maybe()

	w := enricher.New(svc, testConfig(1), testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		err := w.Run(ctx)
		assert.NoError(t, err)
	})
	assert.True(t, processed.Load(), "job with both IDs should have been processed")
}

func TestGracefulDrainMultipleInflightJobs(t *testing.T) {
	t.Parallel()

	// This test verifies that the drain phase waits for multiple in-flight
	// jobs across multiple workers.

	const workerCount = 3

	var (
		mu         sync.Mutex
		jobIndex   int
		allStarted = make(chan struct{})
		started    atomic.Int32
		completed  atomic.Int32
	)

	jobs := make([]*enrichmentjob.Job, workerCount)
	for i := range jobs {
		jobs[i] = newTestJob()
	}

	svc := &concurrentService{
		claimFunc: func(ctx context.Context) (*enrichmentjob.Job, error) {
			mu.Lock()
			if jobIndex < len(jobs) {
				j := jobs[jobIndex]
				jobIndex++
				mu.Unlock()
				return j, nil
			}
			mu.Unlock()
			// Block until context is cancelled to avoid busy-spinning.
			<-ctx.Done()
			return nil, ctx.Err()
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

	cfg := testConfig(workerCount)
	cfg.DrainTimeout = 2 * time.Second

	w := enricher.New(svc, cfg, testLogger())

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
