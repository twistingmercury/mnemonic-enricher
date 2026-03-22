package pipeline_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twistingmercury/mnemonic-enricher/tests/e2e/helpers"
)

func TestEnrichmentPipeline_HappyPath(t *testing.T) {
	pool := helpers.NewPGConn(t)

	patternID := helpers.SeedPattern(t, pool)
	chunkID := helpers.SeedChunk(t, pool, patternID)
	jobID := helpers.SeedEnrichmentJob(t, pool, chunkID)

	t.Cleanup(func() {
		helpers.CleanupPattern(t, pool, patternID)
	})

	driver := helpers.NewNeo4jDriver(t)

	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})

	t.Cleanup(func() {
		helpers.CleanupPatternGraph(t, driver, patternID)
	})

	helpers.PublishJob(t, jobID)

	status := helpers.PollJobStatus(t, pool, jobID, 30*time.Second)
	if status != "completed" {
		t.Fatalf("expected job status %q, got %q", "completed", status)
	}

	helpers.AssertChunkEmbeddingSet(t, pool, chunkID)
	helpers.AssertPatternNodeExists(t, driver, patternID)
	helpers.AssertConceptNodesExist(t, driver, patternID)
}

// TestEnrichmentPipeline_MalformedMessage verifies the worker continues
// processing valid jobs after receiving a message that is not valid JSON.
// A worker that deadlocks or exits after bad input would fail to complete
// the real job published below.
func TestEnrichmentPipeline_MalformedMessage(t *testing.T) {
	// Publish invalid JSON to the queue first.
	helpers.PublishRaw(t, []byte("not-json"))

	// Seed a real job and verify the worker picks it up and completes it.
	pool := helpers.NewPGConn(t)
	patternID := helpers.SeedPattern(t, pool)
	chunkID := helpers.SeedChunk(t, pool, patternID)
	jobID := helpers.SeedEnrichmentJob(t, pool, chunkID)

	driver := helpers.NewNeo4jDriver(t)
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	t.Cleanup(func() { helpers.CleanupPatternGraph(t, driver, patternID) })
	t.Cleanup(func() { helpers.CleanupPattern(t, pool, patternID) })

	helpers.PublishJob(t, jobID)

	status := helpers.PollJobStatus(t, pool, jobID, 30*time.Second)
	if status != "completed" {
		t.Fatalf("expected job status %q after malformed message, got %q", "completed", status)
	}

	helpers.AssertChunkEmbeddingSet(t, pool, chunkID)
}

// TestEnrichmentPipeline_JobNotFound verifies the worker continues processing
// valid jobs after receiving a job ID that has no corresponding database row.
// A worker that stalls on lookup errors would fail to complete the real job
// published below.
func TestEnrichmentPipeline_JobNotFound(t *testing.T) {
	// Publish a random UUID — no row exists in enrichment_jobs for this ID.
	helpers.PublishJob(t, uuid.New())

	// Seed a real job and verify the worker picks it up and completes it.
	pool := helpers.NewPGConn(t)
	patternID := helpers.SeedPattern(t, pool)
	chunkID := helpers.SeedChunk(t, pool, patternID)
	jobID := helpers.SeedEnrichmentJob(t, pool, chunkID)

	driver := helpers.NewNeo4jDriver(t)
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	t.Cleanup(func() { helpers.CleanupPatternGraph(t, driver, patternID) })
	t.Cleanup(func() { helpers.CleanupPattern(t, pool, patternID) })

	helpers.PublishJob(t, jobID)

	status := helpers.PollJobStatus(t, pool, jobID, 30*time.Second)
	if status != "completed" {
		t.Fatalf("expected job status %q after unknown job ID, got %q", "completed", status)
	}

	helpers.AssertChunkEmbeddingSet(t, pool, chunkID)
}

func TestEnrichmentPipeline_OpenAIFailure(t *testing.T) {
	openAIStubURL := os.Getenv("OPENAI_STUB_URL")
	if openAIStubURL == "" {
		openAIStubURL = "http://localhost:8090"
	}

	req, err := http.NewRequest(http.MethodPost, openAIStubURL+"/control/fail-next", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("failed to create fail-next request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	controlResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to POST %s/control/fail-next: %v", openAIStubURL, err)
	}
	defer controlResp.Body.Close()

	if controlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected fail-next response status 200, got %d", controlResp.StatusCode)
	}

	pool := helpers.NewPGConn(t)
	patternID := helpers.SeedPattern(t, pool)
	chunkID := helpers.SeedChunk(t, pool, patternID)
	jobID := helpers.SeedEnrichmentJob(t, pool, chunkID)

	driver := helpers.NewNeo4jDriver(t)
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})
	t.Cleanup(func() {
		helpers.CleanupPattern(t, pool, patternID)
	})
	t.Cleanup(func() {
		helpers.CleanupPatternGraph(t, driver, patternID)
	})

	helpers.PublishJob(t, jobID)

	status, lastError := helpers.PollJobStatusRaw(t, pool, jobID, 30*time.Second)
	if status != "failed" {
		t.Fatalf("expected job to fail, got status %q", status)
	}
	if lastError == "" {
		t.Fatalf("expected last_error to be set for failed job %s", jobID)
	}

	helpers.AssertPatternNodeNotExists(t, driver, patternID)
}
