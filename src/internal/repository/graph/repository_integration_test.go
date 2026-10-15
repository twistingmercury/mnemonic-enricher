//go:build integration

package graph_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/mnemonic-enricher/internal/repository/graph"
)

const testConceptPrefix = "test-integration-concept-"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// setupNeo4j connects to the Neo4j named by NEO4J_* env vars, or skips.
func setupNeo4j(t *testing.T) (neo4j.DriverWithContext, graph.Repository) {
	t.Helper()

	driver, err := neo4j.NewDriverWithContext(
		envOr("NEO4J_URI", "bolt://localhost:7688"),
		neo4j.BasicAuth(envOr("NEO4J_USER", "neo4j"), envOr("NEO4J_PASSWORD", "mnemonic_dev"), ""))
	if err != nil {
		t.Skipf("skipping integration test: unable to create Neo4j driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		t.Skipf("skipping integration test: Neo4j connectivity check failed: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	return driver, graph.NewRepository(driver, envOr("NEO4J_DATABASE", "neo4j"), nil)
}

func query(t *testing.T, driver neo4j.DriverWithContext, cypher string, params map[string]any) []*neo4j.Record {
	t.Helper()
	res, err := neo4j.ExecuteQuery(context.Background(), driver, cypher, params, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	return res.Records
}

func TestIntegration_SyncPattern(t *testing.T) {
	driver, repo := setupNeo4j(t)
	ctx := context.Background()

	a, b := uuid.New(), uuid.New()
	t.Cleanup(func() {
		query(t, driver, `MATCH (p:Pattern) WHERE p.id IN $ids DETACH DELETE p`,
			map[string]any{"ids": []string{a.String(), b.String()}})
		query(t, driver, `MATCH (c:Concept) WHERE c.name STARTS WITH $prefix DETACH DELETE c`,
			map[string]any{"prefix": testConceptPrefix})
	})

	shared := graph.Concept{Name: testConceptPrefix + "go", Type: "technology"}
	onlyA := graph.Concept{Name: testConceptPrefix + "retries", Type: "practice"}

	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: b, Name: "b"}, []graph.Concept{shared}, 0.3))
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: a, Name: "a"}, []graph.Concept{shared, onlyA}, 0.3))

	concepts := query(t, driver, `MATCH (c:Concept)-[:MENTIONED_IN]->(:Pattern {id: $id}) RETURN c.name AS name ORDER BY name`,
		map[string]any{"id": a.String()})
	assert.Len(t, concepts, 2)

	// a shares 1 of max(2, 1) concepts with b: similarity 0.5.
	related := query(t, driver, `MATCH (:Pattern {id: $a})-[r:RELATED_TO]->(:Pattern {id: $b}) RETURN r.similarity AS s`,
		map[string]any{"a": a.String(), "b": b.String()})
	require.Len(t, related, 1)
	s, _ := related[0].Get("s")
	assert.InDelta(t, 0.5, s, 0.0001)

	// Re-syncing with fewer concepts drops the stale MENTIONED_IN and RELATED_TO.
	require.NoError(t, repo.SyncPattern(ctx, graph.Pattern{ID: a, Name: "a"}, []graph.Concept{onlyA}, 0.3))
	concepts = query(t, driver, `MATCH (c:Concept)-[:MENTIONED_IN]->(:Pattern {id: $id}) RETURN c.name AS name`,
		map[string]any{"id": a.String()})
	assert.Len(t, concepts, 1)
	related = query(t, driver, `MATCH (:Pattern {id: $a})-[r:RELATED_TO]-() RETURN r`, map[string]any{"a": a.String()})
	assert.Empty(t, related)
}
