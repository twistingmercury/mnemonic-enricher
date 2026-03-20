package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/db"
)

// CypherRunner abstracts the ability to run Cypher queries.
// This interface is satisfied by neo4j.ManagedTransaction and can be
// implemented by test mocks without needing unexported methods.
type CypherRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) (ResultCollector, error)
}

// ResultCollector abstracts the ability to collect results from a Cypher query.
// This interface wraps the subset of neo4j.ResultWithContext that this package uses.
type ResultCollector interface {
	Collect(ctx context.Context) ([]*db.Record, error)
	Single(ctx context.Context) (*db.Record, error)
}

// SessionExecutor abstracts Neo4j session operations for testability.
type SessionExecutor interface {
	ExecuteRead(ctx context.Context, work func(runner CypherRunner) (any, error)) (any, error)
	ExecuteWrite(ctx context.Context, work func(runner CypherRunner) (any, error)) (any, error)
	Close(ctx context.Context) error
}

// SessionFactory creates SessionExecutor instances.
// This allows the repository to be tested without a real Neo4j driver.
type SessionFactory func(ctx context.Context) SessionExecutor

// Repository defines data access operations for the Neo4j knowledge graph.
type Repository interface {
	// SyncAgent creates or updates an Agent node in the graph.
	SyncAgent(ctx context.Context, agentName string) error

	// DeleteAgent removes an Agent node and all its relationships from the graph.
	DeleteAgent(ctx context.Context, agentName string) error

	// SyncPattern creates or updates a Pattern node in the graph.
	SyncPattern(ctx context.Context, pattern *Pattern) error

	// DeletePattern removes a Pattern node and all its relationships from the graph.
	DeletePattern(ctx context.Context, patternID uuid.UUID) error

	// SyncConcepts replaces all MENTIONED_IN relationships for a pattern with the provided concepts.
	SyncConcepts(ctx context.Context, patternID uuid.UUID, concepts []Concept) error

	// SetPatternAgentRelevance replaces all RELEVANT_FOR relationships for a pattern.
	SetPatternAgentRelevance(ctx context.Context, patternID uuid.UUID, associations []AgentAssociation) error

	// ComputeRelatedToEdges deletes existing RELATED_TO edges for the given pattern
	// and recomputes them based on shared concepts. Only edges with similarity >= minSimilarity
	// are created. The similarity is computed as: shared_concepts / max(total_concepts_a, total_concepts_b).
	// Called by EnrichmentService.ProcessJob after concept extraction.
	ComputeRelatedToEdges(ctx context.Context, patternID uuid.UUID, minSimilarity float64) error

	// GetPatternConcepts returns all concepts linked to a pattern via MENTIONED_IN relationships.
	GetPatternConcepts(ctx context.Context, patternID uuid.UUID) ([]Concept, error)

	// FindRelatedPatterns finds patterns related to the given pattern using pre-computed RELATED_TO edges.
	// Results include similarity scores and shared concept names.
	FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]RelatedPattern, error)

	// FindPatternsByAgent finds patterns relevant to the specified agent, ordered by relevance.
	FindPatternsByAgent(ctx context.Context, agentName string, limit int) ([]PatternRelevance, error)

	// CleanupOrphanedConcepts removes concept nodes with no MENTIONED_IN relationships.
	CleanupOrphanedConcepts(ctx context.Context) (int64, error)

	// HealthCheck verifies connectivity to the Neo4j database.
	HealthCheck(ctx context.Context) error
}

// neo4jRepository is a Neo4j implementation of Repository.
type neo4jRepository struct {
	driver        neo4j.DriverWithContext
	database      string
	factory       SessionFactory
	healthCheckFn func(ctx context.Context) error
}

// NewRepository creates a new Neo4j-backed Repository.
func NewRepository(driver neo4j.DriverWithContext, database string) Repository {
	r := &neo4jRepository{
		driver:        driver,
		database:      database,
		healthCheckFn: driver.VerifyConnectivity,
	}
	r.factory = r.defaultSessionFactory
	return r
}

// newRepositoryWithFactory creates a new Repository with a custom SessionFactory
// and an optional health check function. This is used for unit testing with mocked sessions.
func newRepositoryWithFactory(factory SessionFactory, healthCheckFn func(ctx context.Context) error) Repository {
	if healthCheckFn == nil {
		healthCheckFn = func(_ context.Context) error { return nil }
	}
	return &neo4jRepository{
		factory:       factory,
		healthCheckFn: healthCheckFn,
	}
}

// neo4jSessionAdapter wraps a neo4j.SessionWithContext to satisfy SessionExecutor.
type neo4jSessionAdapter struct {
	session neo4j.SessionWithContext
}

// neo4jCypherRunnerAdapter wraps a neo4j.ManagedTransaction to satisfy CypherRunner.
type neo4jCypherRunnerAdapter struct {
	tx neo4j.ManagedTransaction
}

// neo4jResultAdapter wraps a neo4j.ResultWithContext to satisfy ResultCollector.
type neo4jResultAdapter struct {
	result neo4j.ResultWithContext
}

func (a *neo4jResultAdapter) Collect(ctx context.Context) ([]*db.Record, error) {
	return a.result.Collect(ctx)
}

func (a *neo4jResultAdapter) Single(ctx context.Context) (*db.Record, error) {
	return a.result.Single(ctx)
}

func (a *neo4jCypherRunnerAdapter) Run(ctx context.Context, cypher string, params map[string]any) (ResultCollector, error) {
	result, err := a.tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return &neo4jResultAdapter{result: result}, nil
}

func (a *neo4jSessionAdapter) ExecuteRead(ctx context.Context, work func(runner CypherRunner) (any, error)) (any, error) {
	return a.session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return work(&neo4jCypherRunnerAdapter{tx: tx})
	})
}

func (a *neo4jSessionAdapter) ExecuteWrite(ctx context.Context, work func(runner CypherRunner) (any, error)) (any, error) {
	return a.session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return work(&neo4jCypherRunnerAdapter{tx: tx})
	})
}

func (a *neo4jSessionAdapter) Close(ctx context.Context) error {
	return a.session.Close(ctx)
}

// defaultSessionFactory creates a real Neo4j session wrapped in an adapter.
func (r *neo4jRepository) defaultSessionFactory(ctx context.Context) SessionExecutor {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.database,
	})
	return &neo4jSessionAdapter{session: session}
}

// SyncAgent creates or updates an Agent node in the graph.
func (r *neo4jRepository) SyncAgent(ctx context.Context, agentName string) (err error) {
	if strings.TrimSpace(agentName) == "" {
		return errors.New("agentName must not be empty")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		_, err := runner.Run(ctx,
			"MERGE (a:Agent {name: $name}) SET a.updatedAt = datetime()",
			map[string]any{"name": agentName},
		)
		return nil, err
	})

	if err != nil {
		return fmt.Errorf("syncing agent %q: %w", agentName, err)
	}
	return nil
}

// DeleteAgent removes an Agent node and all its relationships from the graph.
func (r *neo4jRepository) DeleteAgent(ctx context.Context, agentName string) (err error) {
	if strings.TrimSpace(agentName) == "" {
		return errors.New("agentName must not be empty")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		_, err := runner.Run(ctx,
			"MATCH (a:Agent {name: $name}) DETACH DELETE a",
			map[string]any{"name": agentName},
		)
		return nil, err
	})

	if err != nil {
		return fmt.Errorf("deleting agent %q: %w", agentName, err)
	}
	return nil
}

// SyncPattern creates or updates a Pattern node in the graph.
func (r *neo4jRepository) SyncPattern(ctx context.Context, pattern *Pattern) (err error) {
	if pattern == nil {
		return errors.New("pattern must not be nil")
	}
	if strings.TrimSpace(pattern.Name) == "" {
		return errors.New("pattern name must not be empty")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	var desc any
	if pattern.Description != nil {
		desc = *pattern.Description
	}

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		_, err := runner.Run(ctx,
			`MERGE (p:Pattern {id: $patternId})
			 SET p.name = $patternName,
			     p.description = $patternDescription,
			     p.updatedAt = datetime()`,
			map[string]any{
				"patternId":          pattern.ID.String(),
				"patternName":        pattern.Name,
				"patternDescription": desc,
			},
		)
		return nil, err
	})

	if err != nil {
		return fmt.Errorf("syncing pattern %s: %w", pattern.ID, err)
	}
	return nil
}

// DeletePattern removes a Pattern node and all its relationships from the graph.
func (r *neo4jRepository) DeletePattern(ctx context.Context, patternID uuid.UUID) (err error) {
	if patternID == uuid.Nil {
		return errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		_, err := runner.Run(ctx,
			"MATCH (p:Pattern {id: $patternId}) DETACH DELETE p",
			map[string]any{"patternId": patternID.String()},
		)
		return nil, err
	})

	if err != nil {
		return fmt.Errorf("deleting pattern %s: %w", patternID, err)
	}
	return nil
}

// SyncConcepts replaces all MENTIONED_IN relationships for a pattern with the provided concepts.
func (r *neo4jRepository) SyncConcepts(ctx context.Context, patternID uuid.UUID, concepts []Concept) (err error) {
	if patternID == uuid.Nil {
		return errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	// Extract concept names for the diff-based delete query.
	conceptNames := make([]string, len(concepts))
	for i, c := range concepts {
		conceptNames[i] = c.Name
	}

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		// Step 1: Remove MENTIONED_IN relationships only for concepts NOT in the new list.
		// When conceptNames is empty, WHERE NOT c.name IN [] matches everything,
		// so all existing relationships are removed.
		_, err := runner.Run(ctx,
			`MATCH (c:Concept)-[r:MENTIONED_IN]->(p:Pattern {id: $patternId})
			 WHERE NOT c.name IN $conceptNames
			 DELETE r`,
			map[string]any{
				"patternId":    patternID.String(),
				"conceptNames": conceptNames,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("removing old MENTIONED_IN relationships: %w", err)
		}

		if len(concepts) == 0 {
			return nil, nil
		}

		// Step 2: MERGE concepts and relationships (idempotent — no-op if unchanged).
		conceptMaps := make([]map[string]any, len(concepts))
		for i, c := range concepts {
			conceptMaps[i] = map[string]any{
				"name": c.Name,
				"type": c.Type,
			}
		}

		_, err = runner.Run(ctx,
			`UNWIND $concepts AS concept
			 MERGE (c:Concept {name: concept.name})
			 ON CREATE SET c.type = concept.type, c.createdAt = datetime()
			 ON MATCH SET c.type = concept.type
			 WITH c
			 MATCH (p:Pattern {id: $patternId})
			 MERGE (c)-[:MENTIONED_IN]->(p)`,
			map[string]any{
				"patternId": patternID.String(),
				"concepts":  conceptMaps,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("creating concepts and relationships: %w", err)
		}

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("syncing concepts for pattern %s: %w", patternID, err)
	}
	return nil
}

// SetPatternAgentRelevance replaces all RELEVANT_FOR relationships for a pattern.
func (r *neo4jRepository) SetPatternAgentRelevance(ctx context.Context, patternID uuid.UUID, associations []AgentAssociation) (err error) {
	if patternID == uuid.Nil {
		return errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	// Extract agent names for the diff-based delete query.
	agentNames := make([]string, len(associations))
	for i, a := range associations {
		agentNames[i] = a.AgentName
	}

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		// Step 1: Remove RELEVANT_FOR relationships only for agents NOT in the new list.
		// When agentNames is empty, WHERE NOT a.name IN [] matches everything,
		// so all existing relationships are removed.
		_, err := runner.Run(ctx,
			`MATCH (p:Pattern {id: $patternId})-[r:RELEVANT_FOR]->(a:Agent)
			 WHERE NOT a.name IN $agentNames
			 DELETE r`,
			map[string]any{
				"patternId":  patternID.String(),
				"agentNames": agentNames,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("removing old RELEVANT_FOR relationships: %w", err)
		}

		if len(associations) == 0 {
			return nil, nil
		}

		// Step 2: MERGE relationships and update relevance (preserves existing, updates in place).
		assocMaps := make([]map[string]any, len(associations))
		for i, a := range associations {
			assocMaps[i] = map[string]any{
				"agentName": a.AgentName,
				"relevance": a.Relevance,
			}
		}

		_, err = runner.Run(ctx,
			`UNWIND $associations AS assoc
			 MATCH (p:Pattern {id: $patternId})
			 MATCH (a:Agent {name: assoc.agentName})
			 MERGE (p)-[r:RELEVANT_FOR]->(a)
			 SET r.relevance = assoc.relevance`,
			map[string]any{
				"patternId":    patternID.String(),
				"associations": assocMaps,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("creating RELEVANT_FOR relationships: %w", err)
		}

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("setting agent relevance for pattern %s: %w", patternID, err)
	}
	return nil
}

// ComputeRelatedToEdges deletes existing RELATED_TO edges for the given pattern
// and recomputes them based on shared concepts.
func (r *neo4jRepository) ComputeRelatedToEdges(ctx context.Context, patternID uuid.UUID, minSimilarity float64) (err error) {
	if patternID == uuid.Nil {
		return errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	_, err = session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		// Step 1: Delete existing RELATED_TO edges for this pattern.
		_, err := runner.Run(ctx,
			`MATCH (p:Pattern {id: $patternId})-[r:RELATED_TO]-()
			 DELETE r`,
			map[string]any{"patternId": patternID.String()},
		)
		if err != nil {
			return nil, fmt.Errorf("deleting existing RELATED_TO edges: %w", err)
		}

		// Step 2: Find patterns sharing concepts, compute similarity, create edges.
		_, err = runner.Run(ctx,
			`MATCH (p1:Pattern {id: $patternId})<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2:Pattern)
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
			 CREATE (p1)-[:RELATED_TO {similarity: similarity, updatedAt: datetime()}]->(p2)`,
			map[string]any{
				"patternId":     patternID.String(),
				"minSimilarity": minSimilarity,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("computing RELATED_TO edges: %w", err)
		}

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("computing related-to edges for pattern %s: %w", patternID, err)
	}
	return nil
}

// GetPatternConcepts returns all concepts linked to a pattern via MENTIONED_IN relationships.
func (r *neo4jRepository) GetPatternConcepts(ctx context.Context, patternID uuid.UUID) (_ []Concept, err error) {
	if patternID == uuid.Nil {
		return nil, errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	result, err := session.ExecuteRead(ctx, func(runner CypherRunner) (any, error) {
		res, err := runner.Run(ctx,
			`MATCH (c:Concept)-[:MENTIONED_IN]->(p:Pattern {id: $patternId})
			 RETURN c.name AS name, c.type AS type
			 ORDER BY c.name`,
			map[string]any{"patternId": patternID.String()},
		)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		concepts := make([]Concept, 0, len(records))
		for _, record := range records {
			nameVal, ok := record.Get("name")
			if !ok {
				return nil, fmt.Errorf("missing 'name' field in record")
			}
			nameStr, ok := nameVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'name': %T", nameVal)
			}

			typeVal, ok := record.Get("type")
			if !ok {
				return nil, fmt.Errorf("missing 'type' field in record")
			}
			typeStr, ok := typeVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'type': %T", typeVal)
			}

			concepts = append(concepts, Concept{
				Name: nameStr,
				Type: typeStr,
			})
		}

		return concepts, nil
	})

	if err != nil {
		return nil, fmt.Errorf("getting concepts for pattern %s: %w", patternID, err)
	}

	return result.([]Concept), nil
}

// FindRelatedPatterns finds patterns related to the given pattern using pre-computed RELATED_TO edges.
// Results include similarity scores and shared concept names.
func (r *neo4jRepository) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) (_ []RelatedPattern, err error) {
	if patternID == uuid.Nil {
		return nil, errors.New("patternID must not be nil UUID")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	result, err := session.ExecuteRead(ctx, func(runner CypherRunner) (any, error) {
		res, err := runner.Run(ctx,
			`MATCH (p1:Pattern {id: $patternId})-[r:RELATED_TO]-(p2:Pattern)
			 WITH p1, p2, r.similarity AS similarity
			 OPTIONAL MATCH (p1)<-[:MENTIONED_IN]-(c:Concept)-[:MENTIONED_IN]->(p2)
			 WITH p2, similarity, collect(c.name) AS conceptNames, count(c) AS sharedConcepts
			 ORDER BY similarity DESC
			 LIMIT $limit
			 RETURN p2.id AS id, p2.name AS name, sharedConcepts, similarity, conceptNames`,
			map[string]any{
				"patternId": patternID.String(),
				"limit":     limit,
			},
		)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		patterns := make([]RelatedPattern, 0, len(records))
		for _, record := range records {
			idVal, ok := record.Get("id")
			if !ok {
				return nil, fmt.Errorf("missing 'id' field in record")
			}
			idStr, ok := idVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'id': %T", idVal)
			}

			nameVal, ok := record.Get("name")
			if !ok {
				return nil, fmt.Errorf("missing 'name' field in record")
			}
			nameStr, ok := nameVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'name': %T", nameVal)
			}

			sharedVal, ok := record.Get("sharedConcepts")
			if !ok {
				return nil, fmt.Errorf("missing 'sharedConcepts' field in record")
			}
			sharedInt, ok := sharedVal.(int64)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'sharedConcepts': %T", sharedVal)
			}

			similarityVal, ok := record.Get("similarity")
			if !ok {
				return nil, fmt.Errorf("missing 'similarity' field in record")
			}
			similarityFloat, ok := similarityVal.(float64)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'similarity': %T", similarityVal)
			}

			conceptNamesVal, ok := record.Get("conceptNames")
			if !ok {
				return nil, fmt.Errorf("missing 'conceptNames' field in record")
			}
			conceptNamesRaw, ok := conceptNamesVal.([]any)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'conceptNames': %T", conceptNamesVal)
			}
			conceptNames := make([]string, 0, len(conceptNamesRaw))
			for _, v := range conceptNamesRaw {
				s, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("unexpected type for concept name element: %T", v)
				}
				conceptNames = append(conceptNames, s)
			}

			parsedID, err := uuid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("parsing pattern ID %q: %w", idStr, err)
			}

			patterns = append(patterns, RelatedPattern{
				ID:             parsedID,
				Name:           nameStr,
				SharedConcepts: int(sharedInt),
				Similarity:     similarityFloat,
				ConceptNames:   conceptNames,
			})
		}

		return patterns, nil
	})

	if err != nil {
		return nil, fmt.Errorf("finding related patterns for %s: %w", patternID, err)
	}

	return result.([]RelatedPattern), nil
}

// FindPatternsByAgent finds patterns relevant to the specified agent, ordered by relevance.
func (r *neo4jRepository) FindPatternsByAgent(ctx context.Context, agentName string, limit int) (_ []PatternRelevance, err error) {
	if strings.TrimSpace(agentName) == "" {
		return nil, errors.New("agentName must not be empty")
	}

	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	result, err := session.ExecuteRead(ctx, func(runner CypherRunner) (any, error) {
		res, err := runner.Run(ctx,
			`MATCH (p:Pattern)-[r:RELEVANT_FOR]->(a:Agent {name: $agentName})
			 RETURN p.id AS id, p.name AS name, r.relevance AS relevance
			 ORDER BY r.relevance DESC
			 LIMIT $limit`,
			map[string]any{
				"agentName": agentName,
				"limit":     limit,
			},
		)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		patterns := make([]PatternRelevance, 0, len(records))
		for _, record := range records {
			idVal, ok := record.Get("id")
			if !ok {
				return nil, fmt.Errorf("missing 'id' field in record")
			}
			idStr, ok := idVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'id': %T", idVal)
			}

			nameVal, ok := record.Get("name")
			if !ok {
				return nil, fmt.Errorf("missing 'name' field in record")
			}
			nameStr, ok := nameVal.(string)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'name': %T", nameVal)
			}

			relevanceVal, ok := record.Get("relevance")
			if !ok {
				return nil, fmt.Errorf("missing 'relevance' field in record")
			}
			relevanceFloat, ok := relevanceVal.(float64)
			if !ok {
				return nil, fmt.Errorf("unexpected type for 'relevance': %T", relevanceVal)
			}

			parsedID, err := uuid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("parsing pattern ID %q: %w", idStr, err)
			}

			patterns = append(patterns, PatternRelevance{
				ID:        parsedID,
				Name:      nameStr,
				Relevance: relevanceFloat,
			})
		}

		return patterns, nil
	})

	if err != nil {
		return nil, fmt.Errorf("finding patterns for agent %q: %w", agentName, err)
	}

	return result.([]PatternRelevance), nil
}

// CleanupOrphanedConcepts removes concept nodes with no MENTIONED_IN relationships.
func (r *neo4jRepository) CleanupOrphanedConcepts(ctx context.Context) (_ int64, err error) {
	session := r.factory(ctx)
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("closing session: %w", closeErr)
		}
	}()

	result, err := session.ExecuteWrite(ctx, func(runner CypherRunner) (any, error) {
		res, err := runner.Run(ctx,
			`MATCH (c:Concept)
			 WHERE NOT (c)-[:MENTIONED_IN]->()
			 DELETE c
			 RETURN count(c) AS deletedCount`,
			nil,
		)
		if err != nil {
			return nil, err
		}

		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}

		deletedVal, ok := record.Get("deletedCount")
		if !ok {
			return nil, fmt.Errorf("missing 'deletedCount' field in record")
		}
		deletedCount, ok := deletedVal.(int64)
		if !ok {
			return nil, fmt.Errorf("unexpected type for 'deletedCount': %T", deletedVal)
		}
		return deletedCount, nil
	})

	if err != nil {
		return 0, fmt.Errorf("cleaning up orphaned concepts: %w", err)
	}

	return result.(int64), nil
}

// HealthCheck verifies connectivity to the Neo4j database.
func (r *neo4jRepository) HealthCheck(ctx context.Context) error {
	return r.healthCheckFn(ctx)
}
