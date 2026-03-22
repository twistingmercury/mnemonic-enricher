# E2E Test Suite Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete 5 disabled test files that test non-existent endpoints, and improve the two shallow pipeline resilience tests to actually verify the worker continues processing after bad input.

**Architecture:** `mnemonic-enricher` is a queue worker — it has no REST API beyond `/health`, `/version`, and `/metrics`. The only meaningful E2E tests are operations smoke tests and enrichment pipeline tests. The improved resilience tests follow a publish-bad-input → seed-real-job → assert-completion pattern to prove the worker did not stall.

**Tech Stack:** Go, `testing` stdlib, `pgx/v5` (PostgreSQL), `neo4j-go-driver/v5`, `amqp091-go` (RabbitMQ), OpenAI stub at `localhost:8090`

**Spec:** `docs/superpowers/specs/2026-03-21-e2e-test-rebuild-design.md`

---

## File Map

**Delete (5 files):**
- `src/tests/e2e/api/agents_test.go`
- `src/tests/e2e/api/patterns_test.go`
- `src/tests/e2e/api/skills_test.go`
- `src/tests/e2e/api/skillfiles_test.go`
- `src/tests/e2e/mcp/mcp_test.go`

**Modify (1 file):**
- `src/tests/e2e/pipeline/enrichment_test.go` — replace 2 shallow tests and remove unused `pollHealthOK` helper

**No changes to:**
- `src/tests/e2e/api/operations_test.go`
- `src/tests/e2e/helpers/helpers.go`
- `src/tests/e2e/helpers/types.go`
- `src/tests/e2e/helpers/db.go`
- `src/tests/e2e/helpers/amqp.go`
- `src/tests/e2e/helpers/neo4j.go`

---

## Task 1: Delete the disabled test files

These files are guarded with `//go:build ignore`, reference undefined helpers, and test HTTP endpoints that do not exist in the service. Delete all 5.

**Files:** Delete `src/tests/e2e/api/agents_test.go`, `patterns_test.go`, `skills_test.go`, `skillfiles_test.go`, `src/tests/e2e/mcp/mcp_test.go`

- [ ] **Step 1: Delete the files**

```bash
git rm src/tests/e2e/api/agents_test.go \
       src/tests/e2e/api/patterns_test.go \
       src/tests/e2e/api/skills_test.go \
       src/tests/e2e/api/skillfiles_test.go \
       src/tests/e2e/mcp/mcp_test.go
```

- [ ] **Step 2: Verify the `api/` package still compiles (operations_test.go remains)**

```bash
cd src && go build ./tests/e2e/api/...
```

Expected: no output (success)

- [ ] **Step 3: Verify the mcp/ directory is gone**

```bash
ls src/tests/e2e/mcp/ 2>&1
```

Expected: `ls: cannot access '...' No such file or directory` (or similar — directory removed by git rm)

- [ ] **Step 4: Commit**

```bash
git commit -S -m "test(e2e): delete disabled test files for non-existent endpoints

Removes agents_test.go, patterns_test.go, skills_test.go,
skillfiles_test.go, and mcp_test.go. These files were guarded
with //go:build ignore, referenced undefined helpers, and tested
HTTP endpoints that do not exist — mnemonic-enricher is a queue
worker with no REST API beyond /health and /version."
```

---

## Task 2: Improve the two shallow pipeline resilience tests

The current `TestEnrichmentPipeline_MalformedMessage` and `TestEnrichmentPipeline_JobNotFound` publish bad input then call `pollHealthOK` — a worker that deadlocks after a bad message would still return 200 on `/health` briefly. The improved tests prove the worker *continued processing* by submitting a real job afterward and asserting it completes.

**File:** Modify `src/tests/e2e/pipeline/enrichment_test.go`

- [ ] **Step 1: Read the current file**

Open `src/tests/e2e/pipeline/enrichment_test.go` and note:
- The `pollHealthOK` helper function (lines ~48-62) — will be deleted
- `TestEnrichmentPipeline_MalformedMessage` (lines ~64-72) — will be replaced
- `TestEnrichmentPipeline_JobNotFound` (lines ~74-84) — will be replaced
- Current imports include `net/http`, `os`, `strings` — used by both `pollHealthOK` and `TestEnrichmentPipeline_OpenAIFailure`. Only `pollHealthOK` is being removed; all three imports must be kept.

- [ ] **Step 2: Replace the file content**

Replace `src/tests/e2e/pipeline/enrichment_test.go` with:

```go
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
```

- [ ] **Step 3: Verify the file compiles**

```bash
cd src && go build ./tests/e2e/pipeline/...
```

Expected: no output (success)

- [ ] **Step 4: Verify no `pollHealthOK` references remain**

```bash
grep -r "pollHealthOK" src/tests/e2e/
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add src/tests/e2e/pipeline/enrichment_test.go
git commit -S -m "test(e2e): improve resilience tests to verify worker continues processing

Replace pollHealthOK pattern with seed-real-job-after-bad-input:
- TestEnrichmentPipeline_MalformedMessage: publishes invalid JSON then
  a real job; asserts the real job completes and embedding is set
- TestEnrichmentPipeline_JobNotFound: publishes an unknown UUID then
  a real job; asserts the real job completes and embedding is set

A deadlocked worker returns 200 on /health briefly — the new pattern
proves the worker actually continued processing."
```

---

## Task 3: Verify the full suite compiles

- [ ] **Step 1: Build all E2E packages**

```bash
cd src && go build ./tests/e2e/...
```

Expected: no output (success). If there are import errors, check that all imports in each file match what is actually used.

- [ ] **Step 2: Verify no disabled test files remain**

```bash
grep -r "go:build ignore" src/tests/e2e/
```

Expected: no output

- [ ] **Step 3: Verify test count**

```bash
cd src && go test -list '.*' ./tests/e2e/... 2>/dev/null | grep -c '^Test'
```

Expected: 15 tests total (11 operations + 4 pipeline)
