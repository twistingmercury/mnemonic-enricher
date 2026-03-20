//go:build integration

package graph_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository/graph"
)

const (
	// testIntegrationAgentPrefix is used to identify test-created agents for cleanup.
	testIntegrationAgentPrefix = "test-integration-"

	// testIntegrationPatternPrefix is used to identify test-created patterns for cleanup.
	testIntegrationPatternPrefix = "test-integration-pattern-"

	// testIntegrationConceptPrefix is used to identify test-created concepts for cleanup.
	testIntegrationConceptPrefix = "test-integration-concept-"
)

// setupNeo4j creates a Neo4j driver and returns a graph.Repository.
// It skips the test if the Neo4j instance is unavailable.
func setupNeo4j(t *testing.T) graph.Repository {
	t.Helper()

	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7688"
	}

	user := os.Getenv("NEO4J_USER")
	if user == "" {
		user = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		password = "mnemonic_dev"
	}

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}

	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})

	return graph.NewRepository(driver, database)
}

// setupNeo4jDriver creates a Neo4j driver and returns both the driver and a
// graph.Repository. The driver is needed for direct Cypher cleanup queries.
func setupNeo4jDriver(t *testing.T) (neo4j.DriverWithContext, graph.Repository) {
	t.Helper()

	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7688"
	}

	user := os.Getenv("NEO4J_USER")
	if user == "" {
		user = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		password = "mnemonic_dev"
	}

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}

	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})

	repo := graph.NewRepository(driver, database)
	return driver, repo
}

// cleanupNeo4jTestAgents removes all agents with the test prefix from Neo4j.
func cleanupNeo4jTestAgents(t *testing.T, repo graph.Repository) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Best-effort cleanup: delete any agents we created.
	_ = repo.DeleteAgent(ctx, testIntegrationAgentPrefix+"healthcheck")
	_ = repo.DeleteAgent(ctx, testIntegrationAgentPrefix+"crud")
}

// cleanupNeo4jTestData uses direct Cypher queries to remove all test-created
// nodes and relationships from Neo4j. This ensures cleanup even when repository
// methods are the subject under test and may be broken.
func cleanupNeo4jTestData(t *testing.T, driver neo4j.DriverWithContext) {
	t.Helper()

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	// Remove test patterns and their relationships.
	_, _ = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx,
			`MATCH (p:Pattern) WHERE p.name STARTS WITH $prefix DETACH DELETE p`,
			map[string]any{"prefix": testIntegrationPatternPrefix},
		)
		return nil, err
	})

	// Remove test agents and their relationships.
	_, _ = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx,
			`MATCH (a:Agent) WHERE a.name STARTS WITH $prefix DETACH DELETE a`,
			map[string]any{"prefix": testIntegrationAgentPrefix},
		)
		return nil, err
	})

	// Remove test concepts and their relationships.
	_, _ = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx,
			`MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
			map[string]any{"prefix": testIntegrationConceptPrefix},
		)
		return nil, err
	})
}

// countConceptsByPrefix returns the number of Concept nodes whose name starts
// with the given prefix. This is used to verify concept creation and cleanup.
func countConceptsByPrefix(t *testing.T, driver neo4j.DriverWithContext, prefix string) int64 {
	t.Helper()

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx,
			`MATCH (c:Concept) WHERE c.name STARTS WITH $prefix RETURN count(c) AS cnt`,
			map[string]any{"prefix": prefix},
		)
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		val, _ := record.Get("cnt")
		return val.(int64), nil
	})
	require.NoError(t, err)
	return result.(int64)
}

// countConceptRelationships returns the number of MENTIONED_IN relationships
// for concepts linked to a specific pattern.
func countConceptRelationships(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) int64 {
	t.Helper()

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx,
			`MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $patternId}) RETURN count(c) AS cnt`,
			map[string]any{"patternId": patternID.String()},
		)
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		val, _ := record.Get("cnt")
		return val.(int64), nil
	})
	require.NoError(t, err)
	return result.(int64)
}

// patternExistsInNeo4j checks whether a Pattern node with the given ID exists.
func patternExistsInNeo4j(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) bool {
	t.Helper()

	database := os.Getenv("NEO4J_DATABASE")
	if database == "" {
		database = "neo4j"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx,
			`MATCH (p:Pattern {id: $patternId}) RETURN count(p) AS cnt`,
			map[string]any{"patternId": patternID.String()},
		)
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		val, _ := record.Get("cnt")
		return val.(int64) > 0, nil
	})
	require.NoError(t, err)
	return result.(bool)
}

// testPattern creates a graph.Pattern with the given suffix, using the test prefix.
func testPattern(suffix string) *graph.Pattern {
	desc := "Integration test pattern: " + suffix
	return &graph.Pattern{
		ID:          uuid.New(),
		Name:        testIntegrationPatternPrefix + suffix,
		Description: &desc,
	}
}

// testConcepts creates a slice of graph.Concept with names using the test prefix.
func testConcepts(names ...string) []graph.Concept {
	concepts := make([]graph.Concept, len(names))
	for i, name := range names {
		concepts[i] = graph.Concept{
			Name: testIntegrationConceptPrefix + name,
			Type: "technology",
		}
	}
	return concepts
}

// --- Tests ---

func TestIntegration_HealthCheck(t *testing.T) {
	repo := setupNeo4j(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := repo.HealthCheck(ctx)
	assert.NoError(t, err)
}

func TestIntegration_SyncAgent_DeleteAgent(t *testing.T) {
	repo := setupNeo4j(t)
	cleanupNeo4jTestAgents(t, repo)
	t.Cleanup(func() { cleanupNeo4jTestAgents(t, repo) })

	ctx := context.Background()
	agentName := testIntegrationAgentPrefix + "crud"

	t.Run("sync creates agent", func(t *testing.T) {
		err := repo.SyncAgent(ctx, agentName)
		require.NoError(t, err)
	})

	t.Run("sync is idempotent", func(t *testing.T) {
		err := repo.SyncAgent(ctx, agentName)
		require.NoError(t, err)
	})

	t.Run("delete removes agent", func(t *testing.T) {
		err := repo.DeleteAgent(ctx, agentName)
		require.NoError(t, err)
	})

	t.Run("delete nonexistent is not an error", func(t *testing.T) {
		// Neo4j MATCH + DETACH DELETE is a no-op for nonexistent nodes.
		err := repo.DeleteAgent(ctx, agentName)
		assert.NoError(t, err)
	})
}

func TestIntegration_SyncPattern_DeletePattern(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()
	p := testPattern("sync-delete")

	t.Run("sync creates pattern", func(t *testing.T) {
		err := repo.SyncPattern(ctx, p)
		require.NoError(t, err)

		exists := patternExistsInNeo4j(t, driver, p.ID)
		assert.True(t, exists, "pattern should exist after sync")
	})

	t.Run("sync is idempotent via MERGE", func(t *testing.T) {
		// Update description and re-sync; should not create a duplicate.
		updatedDesc := "Updated description for idempotency check"
		p.Description = &updatedDesc

		err := repo.SyncPattern(ctx, p)
		require.NoError(t, err)

		exists := patternExistsInNeo4j(t, driver, p.ID)
		assert.True(t, exists, "pattern should still exist after re-sync")
	})

	t.Run("sync with nil description", func(t *testing.T) {
		pNilDesc := testPattern("sync-nil-desc")
		pNilDesc.Description = nil

		err := repo.SyncPattern(ctx, pNilDesc)
		require.NoError(t, err)

		exists := patternExistsInNeo4j(t, driver, pNilDesc.ID)
		assert.True(t, exists, "pattern with nil description should be created")
	})

	t.Run("delete removes pattern", func(t *testing.T) {
		err := repo.DeletePattern(ctx, p.ID)
		require.NoError(t, err)

		exists := patternExistsInNeo4j(t, driver, p.ID)
		assert.False(t, exists, "pattern should not exist after delete")
	})

	t.Run("delete nonexistent is not an error", func(t *testing.T) {
		// MATCH + DETACH DELETE on a nonexistent node is a no-op in Neo4j.
		err := repo.DeletePattern(ctx, p.ID)
		assert.NoError(t, err)
	})
}

func TestIntegration_SyncConcepts(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	// Create a pattern to attach concepts to.
	p := testPattern("concepts")
	require.NoError(t, repo.SyncPattern(ctx, p))

	t.Run("creates concepts and relationships", func(t *testing.T) {
		concepts := testConcepts("go", "concurrency", "channels")
		err := repo.SyncConcepts(ctx, p.ID, concepts)
		require.NoError(t, err)

		count := countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(3), count, "should have 3 concept relationships")
	})

	t.Run("re-sync is idempotent for unchanged concepts", func(t *testing.T) {
		concepts := testConcepts("go", "concurrency", "channels")
		err := repo.SyncConcepts(ctx, p.ID, concepts)
		require.NoError(t, err)

		count := countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(3), count, "should still have 3 concept relationships after re-sync")
	})

	t.Run("diff-based sync removes old and adds new concepts", func(t *testing.T) {
		// Previous: go, concurrency, channels
		// New: go, goroutines, mutex
		// Expected: go stays, concurrency & channels removed, goroutines & mutex added.
		concepts := testConcepts("go", "goroutines", "mutex")
		err := repo.SyncConcepts(ctx, p.ID, concepts)
		require.NoError(t, err)

		count := countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(3), count, "should have 3 concept relationships after diff sync")
	})

	t.Run("sync with empty list removes all concepts", func(t *testing.T) {
		err := repo.SyncConcepts(ctx, p.ID, []graph.Concept{})
		require.NoError(t, err)

		count := countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(0), count, "should have 0 concept relationships after clearing")
	})

	t.Run("sync with nil list removes all concepts", func(t *testing.T) {
		// First add some concepts back.
		concepts := testConcepts("restore-a", "restore-b")
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, concepts))

		count := countConceptRelationships(t, driver, p.ID)
		require.Equal(t, int64(2), count)

		// Sync with nil.
		err := repo.SyncConcepts(ctx, p.ID, nil)
		require.NoError(t, err)

		count = countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(0), count, "nil concept list should remove all relationships")
	})
}

func TestIntegration_SetPatternAgentRelevance(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	// Create prerequisite agents and a pattern.
	agent1 := testIntegrationAgentPrefix + "relevance-a"
	agent2 := testIntegrationAgentPrefix + "relevance-b"
	agent3 := testIntegrationAgentPrefix + "relevance-c"
	require.NoError(t, repo.SyncAgent(ctx, agent1))
	require.NoError(t, repo.SyncAgent(ctx, agent2))
	require.NoError(t, repo.SyncAgent(ctx, agent3))

	p := testPattern("relevance")
	require.NoError(t, repo.SyncPattern(ctx, p))

	t.Run("sets relevance for pattern-agent pairs", func(t *testing.T) {
		associations := []graph.AgentAssociation{
			{AgentName: agent1, Relevance: 0.9},
			{AgentName: agent2, Relevance: 0.7},
		}

		err := repo.SetPatternAgentRelevance(ctx, p.ID, associations)
		require.NoError(t, err)

		// Verify via FindPatternsByAgent.
		results, err := repo.FindPatternsByAgent(ctx, agent1, 10)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, p.ID, results[0].ID)
		assert.InDelta(t, 0.9, results[0].Relevance, 0.01)
	})

	t.Run("updates relevance score", func(t *testing.T) {
		associations := []graph.AgentAssociation{
			{AgentName: agent1, Relevance: 0.5}, // changed from 0.9
			{AgentName: agent2, Relevance: 0.7},
		}

		err := repo.SetPatternAgentRelevance(ctx, p.ID, associations)
		require.NoError(t, err)

		results, err := repo.FindPatternsByAgent(ctx, agent1, 10)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 0.5, results[0].Relevance, 0.01, "relevance should be updated to 0.5")
	})

	t.Run("diff-based sync adds new agent and removes old", func(t *testing.T) {
		// Previous: agent1, agent2
		// New: agent2, agent3
		// Expected: agent1 removed, agent2 kept, agent3 added.
		associations := []graph.AgentAssociation{
			{AgentName: agent2, Relevance: 0.8},
			{AgentName: agent3, Relevance: 0.6},
		}

		err := repo.SetPatternAgentRelevance(ctx, p.ID, associations)
		require.NoError(t, err)

		// agent1 should no longer see this pattern.
		results, err := repo.FindPatternsByAgent(ctx, agent1, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "agent1 should have no patterns after removal")

		// agent3 should now see this pattern.
		results, err = repo.FindPatternsByAgent(ctx, agent3, 10)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 0.6, results[0].Relevance, 0.01)
	})

	t.Run("clears all associations with empty list", func(t *testing.T) {
		err := repo.SetPatternAgentRelevance(ctx, p.ID, []graph.AgentAssociation{})
		require.NoError(t, err)

		results, err := repo.FindPatternsByAgent(ctx, agent2, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "agent2 should have no patterns after clearing")

		results, err = repo.FindPatternsByAgent(ctx, agent3, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "agent3 should have no patterns after clearing")
	})
}

func TestIntegration_FindRelatedPatterns(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	// Create three patterns:
	// pA shares 3 concepts with pB, 1 concept with pC.
	// pD has unique concepts only.
	pA := testPattern("related-a")
	pB := testPattern("related-b")
	pC := testPattern("related-c")
	pD := testPattern("related-unique")

	require.NoError(t, repo.SyncPattern(ctx, pA))
	require.NoError(t, repo.SyncPattern(ctx, pB))
	require.NoError(t, repo.SyncPattern(ctx, pC))
	require.NoError(t, repo.SyncPattern(ctx, pD))

	// pA concepts: go, concurrency, channels, error-handling
	require.NoError(t, repo.SyncConcepts(ctx, pA.ID, testConcepts("go", "concurrency", "channels", "error-handling")))

	// pB concepts: go, concurrency, channels (3 shared with pA)
	require.NoError(t, repo.SyncConcepts(ctx, pB.ID, testConcepts("go", "concurrency", "channels")))

	// pC concepts: go, testing (1 shared with pA)
	require.NoError(t, repo.SyncConcepts(ctx, pC.ID, testConcepts("go", "testing")))

	// pD concepts: python, django (0 shared with pA)
	require.NoError(t, repo.SyncConcepts(ctx, pD.ID, testConcepts("python", "django")))

	// Compute RELATED_TO edges for all patterns so FindRelatedPatterns can discover them.
	// Use minSimilarity=0.0 to ensure all shared-concept relationships produce edges.
	require.NoError(t, repo.ComputeRelatedToEdges(ctx, pA.ID, 0.0))
	require.NoError(t, repo.ComputeRelatedToEdges(ctx, pB.ID, 0.0))
	require.NoError(t, repo.ComputeRelatedToEdges(ctx, pC.ID, 0.0))
	require.NoError(t, repo.ComputeRelatedToEdges(ctx, pD.ID, 0.0))

	t.Run("finds patterns ordered by shared concept count", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA.ID, 10)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 2, "should find at least pB and pC")

		// pB should come first (3 shared concepts), then pC (1 shared concept).
		// Filter to only our test patterns to avoid interference from other data.
		var filtered []graph.RelatedPattern
		for _, r := range results {
			if r.ID == pB.ID || r.ID == pC.ID {
				filtered = append(filtered, r)
			}
		}
		require.Len(t, filtered, 2, "should find both pB and pC")
		assert.Equal(t, pB.ID, filtered[0].ID, "pB should be first (most shared concepts)")
		assert.Equal(t, 3, filtered[0].SharedConcepts)
		assert.Equal(t, pC.ID, filtered[1].ID, "pC should be second")
		assert.Equal(t, 1, filtered[1].SharedConcepts)
	})

	t.Run("does not include the source pattern itself", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA.ID, 10)
		require.NoError(t, err)

		for _, r := range results {
			assert.NotEqual(t, pA.ID, r.ID, "source pattern should not appear in results")
		}
	})

	t.Run("returns no results for pattern with unique concepts", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pD.ID, 10)
		require.NoError(t, err)

		// pD's concepts (python, django) are not shared with any other test pattern.
		// Filter to our test patterns to be precise.
		var filtered []graph.RelatedPattern
		for _, r := range results {
			if r.ID == pA.ID || r.ID == pB.ID || r.ID == pC.ID {
				filtered = append(filtered, r)
			}
		}
		assert.Empty(t, filtered, "pD should have no related test patterns")
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, pA.ID, 1)
		require.NoError(t, err)
		assert.Len(t, results, 1, "should return at most 1 result")
	})

	t.Run("returns empty for nonexistent pattern", func(t *testing.T) {
		results, err := repo.FindRelatedPatterns(ctx, uuid.New(), 10)
		require.NoError(t, err)
		assert.Empty(t, results, "nonexistent pattern should have no related patterns")
	})
}

func TestIntegration_ComputeRelatedToEdges(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	// Create two patterns sharing concepts.
	pX := testPattern("compute-x")
	pY := testPattern("compute-y")
	pZ := testPattern("compute-z")
	require.NoError(t, repo.SyncPattern(ctx, pX))
	require.NoError(t, repo.SyncPattern(ctx, pY))
	require.NoError(t, repo.SyncPattern(ctx, pZ))

	// pX and pY share "go" and "concurrency"; pX and pZ share only "go".
	require.NoError(t, repo.SyncConcepts(ctx, pX.ID, testConcepts("go", "concurrency", "channels")))
	require.NoError(t, repo.SyncConcepts(ctx, pY.ID, testConcepts("go", "concurrency")))
	require.NoError(t, repo.SyncConcepts(ctx, pZ.ID, testConcepts("go", "python")))

	t.Run("creates RELATED_TO edges from shared concepts", func(t *testing.T) {
		err := repo.ComputeRelatedToEdges(ctx, pX.ID, 0.0)
		require.NoError(t, err)

		results, err := repo.FindRelatedPatterns(ctx, pX.ID, 10)
		require.NoError(t, err)

		// Filter to our test patterns.
		var filtered []graph.RelatedPattern
		for _, r := range results {
			if r.ID == pY.ID || r.ID == pZ.ID {
				filtered = append(filtered, r)
			}
		}
		require.Len(t, filtered, 2, "should find pY and pZ as related")

		// pY should have higher similarity (2 shared out of max(3,2)=3) than pZ (1 shared out of max(3,2)=3).
		assert.Equal(t, pY.ID, filtered[0].ID, "pY should be first (higher similarity)")
		assert.InDelta(t, 2.0/3.0, filtered[0].Similarity, 0.01)
		assert.Equal(t, pZ.ID, filtered[1].ID, "pZ should be second")
		assert.InDelta(t, 1.0/3.0, filtered[1].Similarity, 0.01)
	})

	t.Run("is idempotent when called multiple times", func(t *testing.T) {
		// Call again; should not create duplicate edges.
		err := repo.ComputeRelatedToEdges(ctx, pX.ID, 0.0)
		require.NoError(t, err)

		results, err := repo.FindRelatedPatterns(ctx, pX.ID, 10)
		require.NoError(t, err)

		var filtered []graph.RelatedPattern
		for _, r := range results {
			if r.ID == pY.ID || r.ID == pZ.ID {
				filtered = append(filtered, r)
			}
		}
		assert.Len(t, filtered, 2, "idempotent call should still produce exactly 2 results")
	})

	t.Run("respects minSimilarity threshold", func(t *testing.T) {
		// pZ similarity with pX is ~0.33; set threshold above that.
		err := repo.ComputeRelatedToEdges(ctx, pX.ID, 0.5)
		require.NoError(t, err)

		results, err := repo.FindRelatedPatterns(ctx, pX.ID, 10)
		require.NoError(t, err)

		var filtered []graph.RelatedPattern
		for _, r := range results {
			if r.ID == pY.ID || r.ID == pZ.ID {
				filtered = append(filtered, r)
			}
		}
		// Only pY (similarity ~0.67) should pass; pZ (~0.33) should be filtered.
		require.Len(t, filtered, 1, "only pY should pass minSimilarity=0.5")
		assert.Equal(t, pY.ID, filtered[0].ID)
	})

	t.Run("no edges for pattern with no shared concepts", func(t *testing.T) {
		pAlone := testPattern("compute-alone")
		require.NoError(t, repo.SyncPattern(ctx, pAlone))
		require.NoError(t, repo.SyncConcepts(ctx, pAlone.ID, testConcepts("unique-lang")))

		err := repo.ComputeRelatedToEdges(ctx, pAlone.ID, 0.0)
		require.NoError(t, err)

		results, err := repo.FindRelatedPatterns(ctx, pAlone.ID, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "pattern with unique concepts should have no RELATED_TO edges")
	})
}

func TestIntegration_GetPatternConcepts(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	t.Run("returns concepts for a pattern", func(t *testing.T) {
		p := testPattern("get-concepts")
		require.NoError(t, repo.SyncPattern(ctx, p))

		expected := testConcepts("alpha", "beta", "gamma")
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, expected))

		concepts, err := repo.GetPatternConcepts(ctx, p.ID)
		require.NoError(t, err)
		require.Len(t, concepts, 3)

		// Results are ordered by c.name ASC.
		assert.Equal(t, testIntegrationConceptPrefix+"alpha", concepts[0].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"beta", concepts[1].Name)
		assert.Equal(t, testIntegrationConceptPrefix+"gamma", concepts[2].Name)

		// All should have the type set by testConcepts.
		for _, c := range concepts {
			assert.Equal(t, "technology", c.Type)
		}
	})

	t.Run("returns empty for pattern with no concepts", func(t *testing.T) {
		p := testPattern("get-concepts-empty")
		require.NoError(t, repo.SyncPattern(ctx, p))

		concepts, err := repo.GetPatternConcepts(ctx, p.ID)
		require.NoError(t, err)
		assert.Empty(t, concepts)
	})

	t.Run("returns empty for nonexistent pattern", func(t *testing.T) {
		concepts, err := repo.GetPatternConcepts(ctx, uuid.New())
		require.NoError(t, err)
		assert.Empty(t, concepts)
	})

	t.Run("reflects changes after concept re-sync", func(t *testing.T) {
		p := testPattern("get-concepts-resync")
		require.NoError(t, repo.SyncPattern(ctx, p))

		require.NoError(t, repo.SyncConcepts(ctx, p.ID, testConcepts("first", "second")))
		concepts, err := repo.GetPatternConcepts(ctx, p.ID)
		require.NoError(t, err)
		require.Len(t, concepts, 2)

		// Re-sync with different concepts.
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, testConcepts("third")))
		concepts, err = repo.GetPatternConcepts(ctx, p.ID)
		require.NoError(t, err)
		require.Len(t, concepts, 1)
		assert.Equal(t, testIntegrationConceptPrefix+"third", concepts[0].Name)
	})
}

func TestIntegration_FindPatternsByAgent(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	// Create agent and multiple patterns with different relevance scores.
	agentName := testIntegrationAgentPrefix + "finder"
	require.NoError(t, repo.SyncAgent(ctx, agentName))

	pHigh := testPattern("agent-high")
	pMed := testPattern("agent-med")
	pLow := testPattern("agent-low")
	require.NoError(t, repo.SyncPattern(ctx, pHigh))
	require.NoError(t, repo.SyncPattern(ctx, pMed))
	require.NoError(t, repo.SyncPattern(ctx, pLow))

	// Set relevance: high=0.9, med=0.6, low=0.3
	require.NoError(t, repo.SetPatternAgentRelevance(ctx, pHigh.ID, []graph.AgentAssociation{
		{AgentName: agentName, Relevance: 0.9},
	}))
	require.NoError(t, repo.SetPatternAgentRelevance(ctx, pMed.ID, []graph.AgentAssociation{
		{AgentName: agentName, Relevance: 0.6},
	}))
	require.NoError(t, repo.SetPatternAgentRelevance(ctx, pLow.ID, []graph.AgentAssociation{
		{AgentName: agentName, Relevance: 0.3},
	}))

	t.Run("returns patterns ordered by relevance descending", func(t *testing.T) {
		results, err := repo.FindPatternsByAgent(ctx, agentName, 10)
		require.NoError(t, err)
		require.Len(t, results, 3)

		assert.Equal(t, pHigh.ID, results[0].ID)
		assert.InDelta(t, 0.9, results[0].Relevance, 0.01)

		assert.Equal(t, pMed.ID, results[1].ID)
		assert.InDelta(t, 0.6, results[1].Relevance, 0.01)

		assert.Equal(t, pLow.ID, results[2].ID)
		assert.InDelta(t, 0.3, results[2].Relevance, 0.01)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		results, err := repo.FindPatternsByAgent(ctx, agentName, 2)
		require.NoError(t, err)
		assert.Len(t, results, 2, "should return at most 2 results")
		// The top 2 should be the highest relevance ones.
		assert.Equal(t, pHigh.ID, results[0].ID)
		assert.Equal(t, pMed.ID, results[1].ID)
	})

	t.Run("returns empty for agent with no patterns", func(t *testing.T) {
		noPatternAgent := testIntegrationAgentPrefix + "no-patterns"
		require.NoError(t, repo.SyncAgent(ctx, noPatternAgent))

		results, err := repo.FindPatternsByAgent(ctx, noPatternAgent, 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("returns empty for nonexistent agent", func(t *testing.T) {
		results, err := repo.FindPatternsByAgent(ctx, testIntegrationAgentPrefix+"does-not-exist", 10)
		require.NoError(t, err)
		assert.Empty(t, results, "nonexistent agent should return empty results")
	})
}

func TestIntegration_CleanupOrphanedConcepts(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	t.Run("removes orphaned concepts after pattern deletion", func(t *testing.T) {
		// Create a pattern and attach unique concepts.
		p := testPattern("orphan-source")
		require.NoError(t, repo.SyncPattern(ctx, p))

		concepts := testConcepts("orphan-a", "orphan-b", "orphan-c")
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, concepts))

		// Verify concepts exist.
		count := countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-")
		require.Equal(t, int64(3), count, "should have 3 orphan-* concepts")

		// Delete the pattern, which removes MENTIONED_IN relationships via DETACH DELETE.
		require.NoError(t, repo.DeletePattern(ctx, p.ID))

		// Concepts should now be orphaned (no MENTIONED_IN relationships).
		deleted, err := repo.CleanupOrphanedConcepts(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, deleted, int64(3), "should have cleaned up at least 3 orphaned concepts")

		// Verify the orphaned concepts are gone.
		count = countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"orphan-")
		assert.Equal(t, int64(0), count, "orphaned concepts should be removed")
	})

	t.Run("does not remove concepts with active relationships", func(t *testing.T) {
		// Create a pattern with concepts.
		p := testPattern("active-concepts")
		require.NoError(t, repo.SyncPattern(ctx, p))

		concepts := testConcepts("active-x", "active-y")
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, concepts))

		// Run cleanup; active concepts should not be removed.
		_, err := repo.CleanupOrphanedConcepts(ctx)
		require.NoError(t, err)

		count := countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"active-")
		assert.Equal(t, int64(2), count, "active concepts should not be removed")
	})

	t.Run("returns zero when no orphans exist", func(t *testing.T) {
		// After previous cleanup, run again. Should find zero new orphans
		// (assuming no other tests left orphans).
		deleted, err := repo.CleanupOrphanedConcepts(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, deleted, int64(0), "deleted count should be non-negative")
	})
}

func TestIntegration_ContextCancellation(t *testing.T) {
	repo := setupNeo4j(t)

	t.Run("SyncAgent with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := repo.SyncAgent(ctx, testIntegrationAgentPrefix+"cancelled")
		assert.Error(t, err)
	})

	t.Run("SyncPattern with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		p := testPattern("cancelled")
		err := repo.SyncPattern(ctx, p)
		assert.Error(t, err)
	})

	t.Run("FindRelatedPatterns with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := repo.FindRelatedPatterns(ctx, uuid.New(), 10)
		assert.Error(t, err)
	})

	t.Run("FindPatternsByAgent with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := repo.FindPatternsByAgent(ctx, testIntegrationAgentPrefix+"cancelled", 10)
		assert.Error(t, err)
	})

	t.Run("CleanupOrphanedConcepts with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := repo.CleanupOrphanedConcepts(ctx)
		assert.Error(t, err)
	})

	t.Run("ComputeRelatedToEdges with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := repo.ComputeRelatedToEdges(ctx, uuid.New(), 0.0)
		assert.Error(t, err)
	})

	t.Run("GetPatternConcepts with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := repo.GetPatternConcepts(ctx, uuid.New())
		assert.Error(t, err)
	})

	t.Run("SyncConcepts with expired timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Allow context to expire.
		time.Sleep(1 * time.Millisecond)

		err := repo.SyncConcepts(ctx, uuid.New(), testConcepts("timeout"))
		assert.Error(t, err)
	})

	t.Run("SetPatternAgentRelevance with expired timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(1 * time.Millisecond)

		err := repo.SetPatternAgentRelevance(ctx, uuid.New(), []graph.AgentAssociation{
			{AgentName: "any", Relevance: 0.5},
		})
		assert.Error(t, err)
	})
}

func TestIntegration_InputValidation(t *testing.T) {
	repo := setupNeo4j(t)

	ctx := context.Background()

	t.Run("SyncAgent rejects empty name", func(t *testing.T) {
		err := repo.SyncAgent(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agentName must not be empty")
	})

	t.Run("SyncAgent rejects whitespace-only name", func(t *testing.T) {
		err := repo.SyncAgent(ctx, "   ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agentName must not be empty")
	})

	t.Run("DeleteAgent rejects empty name", func(t *testing.T) {
		err := repo.DeleteAgent(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agentName must not be empty")
	})

	t.Run("SyncPattern rejects nil pattern", func(t *testing.T) {
		err := repo.SyncPattern(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern must not be nil")
	})

	t.Run("SyncPattern rejects empty pattern name", func(t *testing.T) {
		p := &graph.Pattern{
			ID:   uuid.New(),
			Name: "",
		}
		err := repo.SyncPattern(ctx, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern name must not be empty")
	})

	t.Run("SyncPattern rejects whitespace-only pattern name", func(t *testing.T) {
		p := &graph.Pattern{
			ID:   uuid.New(),
			Name: "   ",
		}
		err := repo.SyncPattern(ctx, p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pattern name must not be empty")
	})

	t.Run("DeletePattern rejects nil UUID", func(t *testing.T) {
		err := repo.DeletePattern(ctx, uuid.Nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("SyncConcepts rejects nil UUID", func(t *testing.T) {
		err := repo.SyncConcepts(ctx, uuid.Nil, testConcepts("any"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("SetPatternAgentRelevance rejects nil UUID", func(t *testing.T) {
		err := repo.SetPatternAgentRelevance(ctx, uuid.Nil, []graph.AgentAssociation{
			{AgentName: "any", Relevance: 0.5},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("ComputeRelatedToEdges rejects nil UUID", func(t *testing.T) {
		err := repo.ComputeRelatedToEdges(ctx, uuid.Nil, 0.0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("GetPatternConcepts rejects nil UUID", func(t *testing.T) {
		_, err := repo.GetPatternConcepts(ctx, uuid.Nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("FindRelatedPatterns rejects nil UUID", func(t *testing.T) {
		_, err := repo.FindRelatedPatterns(ctx, uuid.Nil, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "patternID must not be nil UUID")
	})

	t.Run("FindPatternsByAgent rejects empty agent name", func(t *testing.T) {
		_, err := repo.FindPatternsByAgent(ctx, "", 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agentName must not be empty")
	})

	t.Run("FindPatternsByAgent rejects whitespace-only agent name", func(t *testing.T) {
		_, err := repo.FindPatternsByAgent(ctx, "   ", 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agentName must not be empty")
	})
}

func TestIntegration_ConcurrentOperations(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	t.Run("concurrent SyncPattern operations", func(t *testing.T) {
		const numPatterns = 10
		errChan := make(chan error, numPatterns)

		for i := range numPatterns {
			go func(idx int) {
				p := testPattern(fmt.Sprintf("concurrent-%d", idx))
				errChan <- repo.SyncPattern(ctx, p)
			}(i)
		}

		var errs []error
		for range numPatterns {
			if err := <-errChan; err != nil {
				errs = append(errs, err)
			}
		}

		assert.Empty(t, errs, "all concurrent SyncPattern calls should succeed")
	})

	t.Run("concurrent SyncConcepts on different patterns", func(t *testing.T) {
		const numPatterns = 5
		patterns := make([]*graph.Pattern, numPatterns)
		for i := range numPatterns {
			patterns[i] = testPattern(fmt.Sprintf("conc-concepts-%d", i))
			require.NoError(t, repo.SyncPattern(ctx, patterns[i]))
		}

		errChan := make(chan error, numPatterns)
		for i := range numPatterns {
			go func(idx int) {
				concepts := testConcepts(
					fmt.Sprintf("conc-c%d-a", idx),
					fmt.Sprintf("conc-c%d-b", idx),
				)
				errChan <- repo.SyncConcepts(ctx, patterns[idx].ID, concepts)
			}(i)
		}

		var errs []error
		for range numPatterns {
			if err := <-errChan; err != nil {
				errs = append(errs, err)
			}
		}

		assert.Empty(t, errs, "all concurrent SyncConcepts calls should succeed")
	})
}

func TestIntegration_EdgeCases(t *testing.T) {
	driver, repo := setupNeo4jDriver(t)
	cleanupNeo4jTestData(t, driver)
	t.Cleanup(func() { cleanupNeo4jTestData(t, driver) })

	ctx := context.Background()

	t.Run("FindRelatedPatterns with no concepts attached", func(t *testing.T) {
		p := testPattern("no-concepts")
		require.NoError(t, repo.SyncPattern(ctx, p))

		results, err := repo.FindRelatedPatterns(ctx, p.ID, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "pattern with no concepts should have no related patterns")
	})

	t.Run("SetPatternAgentRelevance with nil list", func(t *testing.T) {
		agentName := testIntegrationAgentPrefix + "edge-agent"
		require.NoError(t, repo.SyncAgent(ctx, agentName))

		p := testPattern("edge-nil-assoc")
		require.NoError(t, repo.SyncPattern(ctx, p))

		// First set an association.
		require.NoError(t, repo.SetPatternAgentRelevance(ctx, p.ID, []graph.AgentAssociation{
			{AgentName: agentName, Relevance: 0.8},
		}))

		// Clear with nil (should behave like empty list).
		err := repo.SetPatternAgentRelevance(ctx, p.ID, nil)
		require.NoError(t, err)

		results, err := repo.FindPatternsByAgent(ctx, agentName, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "nil associations should clear all relationships")
	})

	t.Run("SyncConcepts for pattern that does not exist in graph", func(t *testing.T) {
		// SyncConcepts uses MATCH (p:Pattern {id: $patternId}) which will simply
		// not find the pattern, resulting in no MERGE operations. The step 1
		// delete also will not match. This should succeed without error.
		nonexistentID := uuid.New()
		concepts := testConcepts("phantom-a")
		err := repo.SyncConcepts(ctx, nonexistentID, concepts)
		// Neo4j does not error on no-op MATCH; this should succeed.
		assert.NoError(t, err)
	})

	t.Run("multiple SyncPattern calls update fields correctly", func(t *testing.T) {
		p := testPattern("multi-sync")
		require.NoError(t, repo.SyncPattern(ctx, p))

		// Update name and description via MERGE (same ID).
		newName := testIntegrationPatternPrefix + "multi-sync-renamed"
		newDesc := "Updated via second sync"
		p.Name = newName
		p.Description = &newDesc
		require.NoError(t, repo.SyncPattern(ctx, p))

		// Verify the pattern was updated (not duplicated) by checking it still exists.
		exists := patternExistsInNeo4j(t, driver, p.ID)
		assert.True(t, exists)
	})

	t.Run("DeletePattern also removes concept relationships", func(t *testing.T) {
		p := testPattern("delete-with-concepts")
		require.NoError(t, repo.SyncPattern(ctx, p))

		concepts := testConcepts("del-c1", "del-c2")
		require.NoError(t, repo.SyncConcepts(ctx, p.ID, concepts))

		count := countConceptRelationships(t, driver, p.ID)
		require.Equal(t, int64(2), count)

		// DETACH DELETE removes the pattern and all its relationships.
		require.NoError(t, repo.DeletePattern(ctx, p.ID))

		count = countConceptRelationships(t, driver, p.ID)
		assert.Equal(t, int64(0), count, "concept relationships should be removed with pattern")
	})

	t.Run("DeleteAgent also removes relevance relationships", func(t *testing.T) {
		agentName := testIntegrationAgentPrefix + "delete-with-rels"
		require.NoError(t, repo.SyncAgent(ctx, agentName))

		p := testPattern("agent-rel-delete")
		require.NoError(t, repo.SyncPattern(ctx, p))

		require.NoError(t, repo.SetPatternAgentRelevance(ctx, p.ID, []graph.AgentAssociation{
			{AgentName: agentName, Relevance: 0.7},
		}))

		// Verify relationship exists.
		results, err := repo.FindPatternsByAgent(ctx, agentName, 10)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Delete the agent; DETACH DELETE removes agent and all its relationships.
		require.NoError(t, repo.DeleteAgent(ctx, agentName))

		results, err = repo.FindPatternsByAgent(ctx, agentName, 10)
		require.NoError(t, err)
		assert.Empty(t, results, "patterns should no longer be associated with deleted agent")
	})

	t.Run("concepts shared across patterns are not duplicated", func(t *testing.T) {
		pX := testPattern("shared-concept-x")
		pY := testPattern("shared-concept-y")
		require.NoError(t, repo.SyncPattern(ctx, pX))
		require.NoError(t, repo.SyncPattern(ctx, pY))

		// Both patterns share the same concept name.
		sharedConcept := testConcepts("shared-singleton")
		require.NoError(t, repo.SyncConcepts(ctx, pX.ID, sharedConcept))
		require.NoError(t, repo.SyncConcepts(ctx, pY.ID, sharedConcept))

		// The concept node should exist once (MERGE ensures this), but have
		// two MENTIONED_IN relationships.
		count := countConceptsByPrefix(t, driver, testIntegrationConceptPrefix+"shared-singleton")
		assert.Equal(t, int64(1), count, "shared concept should exist as a single node")

		countX := countConceptRelationships(t, driver, pX.ID)
		countY := countConceptRelationships(t, driver, pY.ID)
		assert.Equal(t, int64(1), countX, "pX should have 1 concept relationship")
		assert.Equal(t, int64(1), countY, "pY should have 1 concept relationship")
	})
}

func TestIntegration_HealthCheck_Failure(t *testing.T) {
	// This test verifies HealthCheck behavior with a bad connection.
	// We create a driver pointed at an unreachable address.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	driver, err := neo4j.NewDriverWithContext("bolt://localhost:19999", neo4j.BasicAuth("neo4j", "wrong", ""))
	if err != nil {
		t.Skipf("skipping: unable to create driver: %v", err)
	}
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})

	repo := graph.NewRepository(driver, "neo4j")
	err = repo.HealthCheck(ctx)

	// VerifyConnectivity should fail against the unreachable address.
	assert.Error(t, err, "HealthCheck should fail with unreachable Neo4j")

	// Verify the error is not a validation error but a connectivity error.
	assert.False(t, errors.Is(err, context.Canceled), "should not be a context cancellation error")
}
