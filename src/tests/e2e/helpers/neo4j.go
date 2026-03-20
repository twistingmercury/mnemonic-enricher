package helpers

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// NewNeo4jDriver creates a neo4j.DriverWithContext from env vars.
// Reads NEO4J_URI (default: "bolt://localhost:7687"), NEO4J_USER (default: "neo4j"),
// and NEO4J_PASSWORD (default: "mnemonic_dev").
// Calls t.Fatal on error. The caller is responsible for closing the driver.
func NewNeo4jDriver(t *testing.T) neo4j.DriverWithContext {
	t.Helper()

	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}

	user := os.Getenv("NEO4J_USER")
	if user == "" {
		user = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		password = "mnemonic_dev"
	}

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		t.Fatalf("failed to create neo4j driver: %v", err)
	}

	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("failed to verify neo4j connectivity: %v", err)
	}

	return driver
}

// AssertPatternNodeExists asserts that a Pattern node with the given ID exists in Neo4j.
// Calls t.Fatal if no node is found.
func AssertPatternNodeExists(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()

	result, err := neo4j.ExecuteQuery(
		context.Background(),
		driver,
		"MATCH (p:Pattern {id: $id}) RETURN p",
		map[string]any{"id": patternID.String()},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithReadersRouting(),
	)
	if err != nil {
		t.Fatalf("failed to query pattern node %s: %v", patternID, err)
	}

	if len(result.Records) == 0 {
		t.Fatalf("expected Pattern node with id %s to exist in Neo4j, but none found", patternID)
	}
}

// AssertConceptNodesExist asserts that at least one Concept node with a MENTIONED_IN
// edge to the Pattern node exists.
// Calls t.Fatal if no concept nodes are found.
func AssertConceptNodesExist(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()

	result, err := neo4j.ExecuteQuery(
		context.Background(),
		driver,
		"MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $id}) RETURN c",
		map[string]any{"id": patternID.String()},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithReadersRouting(),
	)
	if err != nil {
		t.Fatalf("failed to query concept nodes for pattern %s: %v", patternID, err)
	}

	if len(result.Records) == 0 {
		t.Fatalf("expected at least one Concept node MENTIONED_IN Pattern %s, but none found", patternID)
	}
}

// CleanupPatternGraph deletes the pattern node and all connected concept nodes/edges.
func CleanupPatternGraph(t *testing.T, driver neo4j.DriverWithContext, patternID uuid.UUID) {
	t.Helper()

	_, err := neo4j.ExecuteQuery(
		context.Background(),
		driver,
		"MATCH (p:Pattern {id: $id}) OPTIONAL MATCH (c:Concept)-[:MENTIONED_IN]->(p) DETACH DELETE p, c",
		map[string]any{"id": patternID.String()},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithWritersRouting(),
	)
	if err != nil {
		t.Fatalf("failed to cleanup pattern graph for %s: %v", patternID, err)
	}
}
