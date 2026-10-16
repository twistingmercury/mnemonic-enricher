package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/twistingmercury/mnemonic-enricher/internal/metrics"
)

var tracer = otel.Tracer("mnemonic/neo4j")

// Repository defines the knowledge-graph write the enrichment pipeline needs.
// Reading the graph and deleting patterns is the API's job.
type Repository interface {
	// SyncPattern writes a pattern's node, replaces its MENTIONED_IN concepts,
	// and recomputes its RELATED_TO edges, all in one write transaction. Only
	// edges with similarity >= minSimilarity are created, where similarity is
	// shared_concepts / max(total_concepts_a, total_concepts_b).
	SyncPattern(ctx context.Context, pattern Pattern, concepts []Concept, minSimilarity float64) error
}

// neo4jRepository is a Neo4j implementation of Repository.
type neo4jRepository struct {
	driver    neo4j.DriverWithContext
	database  string
	dbMetrics *metrics.Database
}

// NewRepository creates a new Neo4j-backed Repository. Each operation gets a
// client span and a latency and error recording in dbMetrics.
func NewRepository(driver neo4j.DriverWithContext, database string, dbMetrics *metrics.Database) Repository {
	return &neo4jRepository{driver: driver, database: database, dbMetrics: dbMetrics}
}

// instrument starts a client span for one Neo4j operation. The returned func
// ends the span and records the operation's latency and outcome; defer it
// before the session close so close errors are counted too.
func (r *neo4jRepository) instrument(ctx context.Context, op string) (context.Context, func(error)) {
	start := time.Now()
	ctx, span := tracer.Start(ctx, "neo4j "+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "neo4j"),
			attribute.String("db.namespace", r.database),
			attribute.String("db.operation.name", op),
		),
	)
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		r.dbMetrics.Record(ctx, "neo4j", op, time.Since(start), err)
	}
}

const mergePatternCypher = `MERGE (p:Pattern {id: $patternId})
	 SET p.name = $patternName,
	     p.description = $patternDescription,
	     p.updatedAt = datetime()`

// Removes MENTIONED_IN relationships only for concepts NOT in the new list.
// When conceptNames is empty, WHERE NOT c.name IN [] matches everything, so
// all existing relationships are removed.
const pruneConceptsCypher = `MATCH (c:Concept)-[r:MENTIONED_IN]->(p:Pattern {id: $patternId})
	 WHERE NOT c.name IN $conceptNames
	 DELETE r`

// MERGEs concepts and relationships (idempotent: a no-op if unchanged).
const mergeConceptsCypher = `UNWIND $concepts AS concept
	 MERGE (c:Concept {name: concept.name})
	 ON CREATE SET c.type = concept.type, c.createdAt = datetime()
	 ON MATCH SET c.type = concept.type
	 WITH c
	 MATCH (p:Pattern {id: $patternId})
	 MERGE (c)-[:MENTIONED_IN]->(p)`

const deleteRelatedCypher = `MATCH (p:Pattern {id: $patternId})-[r:RELATED_TO]-()
	 DELETE r`

const createRelatedCypher = `MATCH (p1:Pattern {id: $patternId})<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2:Pattern)
	 WHERE p1 <> p2
	 WITH p1, p2, count(DISTINCT c) AS sharedCount
	 OPTIONAL MATCH (c1:Concept)-[:MENTIONED_IN]->(p1)
	 WITH p1, p2, sharedCount, count(DISTINCT c1) AS totalA
	 OPTIONAL MATCH (c2:Concept)-[:MENTIONED_IN]->(p2)
	 WITH p1, p2, sharedCount, totalA, count(DISTINCT c2) AS totalB
	 WITH p1, p2, sharedCount,
	      CASE WHEN totalA > totalB THEN totalA ELSE totalB END AS maxTotal
	 WITH p1, p2, sharedCount,
	      CASE WHEN maxTotal = 0 THEN 0.0
	           ELSE toFloat(sharedCount) / toFloat(maxTotal)
	      END AS similarity
	 WHERE similarity >= $minSimilarity
	 CREATE (p1)-[:RELATED_TO {similarity: similarity, updatedAt: datetime()}]->(p2)`

// SyncPattern runs the pattern, concept, and RELATED_TO writes in one session
// and write transaction, instead of a session and transaction per step, so a
// failure part way leaves the graph as it was.
func (r *neo4jRepository) SyncPattern(ctx context.Context, pattern Pattern, concepts []Concept, minSimilarity float64) (err error) {
	if pattern.ID == uuid.Nil {
		return errors.New("pattern ID must not be nil UUID")
	}
	if strings.TrimSpace(pattern.Name) == "" {
		return errors.New("pattern name must not be empty")
	}

	ctx, done := r.instrument(ctx, "SyncPattern")
	defer func() { done(err) }()

	session := r.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: r.database, AccessMode: neo4j.AccessModeWrite})
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	var desc any
	if pattern.Description != nil {
		desc = *pattern.Description
	}
	conceptNames := make([]string, len(concepts))
	conceptMaps := make([]map[string]any, len(concepts))
	for i, c := range concepts {
		conceptNames[i] = c.Name
		conceptMaps[i] = map[string]any{"name": c.Name, "type": c.Type}
	}
	id := pattern.ID.String()

	steps := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{"merging pattern", mergePatternCypher, map[string]any{"patternId": id, "patternName": pattern.Name, "patternDescription": desc}},
		{"removing old MENTIONED_IN relationships", pruneConceptsCypher, map[string]any{"patternId": id, "conceptNames": conceptNames}},
		{"creating concepts and relationships", mergeConceptsCypher, map[string]any{"patternId": id, "concepts": conceptMaps}},
		{"deleting existing RELATED_TO edges", deleteRelatedCypher, map[string]any{"patternId": id}},
		{"computing RELATED_TO edges", createRelatedCypher, map[string]any{"patternId": id, "minSimilarity": minSimilarity}},
	}

	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		for _, s := range steps {
			if s.cypher == mergeConceptsCypher && len(concepts) == 0 {
				continue
			}
			res, err := tx.Run(ctx, s.cypher, s.params)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", s.name, err)
			}
			if _, err := res.Consume(ctx); err != nil {
				return nil, fmt.Errorf("%s: %w", s.name, err)
			}
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("syncing pattern %s: %w", pattern.ID, err)
	}
	return nil
}
