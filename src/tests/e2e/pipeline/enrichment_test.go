package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/twistingmercury/mnemonic-enricher/tests/e2e/helpers"
)

func TestEnrichmentPipeline_HappyPath(t *testing.T) {
	pool := helpers.NewPGConn(t)

	patternID := helpers.SeedPattern(t, pool)
	chunkID := helpers.SeedChunk(t, pool, patternID)
	jobID := helpers.SeedEnrichmentJob(t, pool, chunkID, patternID)

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
