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

func pollHealthOK(t *testing.T, enricherURL string) {
	t.Helper()
	for i := 0; i < 10; i++ {
		resp, err := http.Get(enricherURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("enricher health check at %s did not return 200 after 3s", enricherURL)
}

func TestEnrichmentPipeline_MalformedMessage(t *testing.T) {
	helpers.PublishRaw(t, []byte("not-json"))

	enricherURL := os.Getenv("ENRICHER_URL")
	if enricherURL == "" {
		enricherURL = "http://localhost:8080"
	}
	pollHealthOK(t, enricherURL)
}

func TestEnrichmentPipeline_JobNotFound(t *testing.T) {
	randomJobID := uuid.New()

	helpers.PublishJob(t, randomJobID)

	enricherURL := os.Getenv("ENRICHER_URL")
	if enricherURL == "" {
		enricherURL = "http://localhost:8080"
	}
	pollHealthOK(t, enricherURL)
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
