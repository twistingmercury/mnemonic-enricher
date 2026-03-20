package graph

import "github.com/google/uuid"

// Pattern represents a pattern node in the Neo4j knowledge graph.
type Pattern struct {
	// ID is the unique identifier for the pattern, matching the PostgreSQL pattern UUID.
	ID uuid.UUID

	// Name is the human-readable name of the pattern.
	Name string

	// Description is an optional description of the pattern's purpose.
	Description *string
}

// Concept represents a concept node linked to patterns via MENTIONED_IN relationships.
type Concept struct {
	// Name is the normalized lowercase name of the concept.
	Name string

	// Type classifies the concept: technology, practice, or domain.
	Type string
}

// RelatedPattern represents a pattern discovered through shared concepts.
type RelatedPattern struct {
	// ID is the unique identifier of the related pattern.
	ID uuid.UUID

	// Name is the human-readable name of the related pattern.
	Name string

	// SharedConcepts is the number of concepts shared between the source and this pattern.
	SharedConcepts int

	// Similarity is the pre-computed similarity score from the RELATED_TO edge (0.0-1.0).
	// Computed as: sharedConcepts / max(totalConceptsA, totalConceptsB).
	Similarity float64

	// ConceptNames contains the names of the shared concepts between the two patterns.
	ConceptNames []string
}

// PatternRelevance represents a pattern associated with an agent and its relevance score.
type PatternRelevance struct {
	// ID is the unique identifier of the pattern.
	ID uuid.UUID

	// Name is the human-readable name of the pattern.
	Name string

	// Relevance is the relevance score for the agent (0.0 to 1.0).
	Relevance float64
}

// AgentAssociation represents a pattern-agent relationship with relevance score.
// This type is intentionally duplicated from the pattern package to maintain
// package independence and avoid circular dependencies between repository packages.
type AgentAssociation struct {
	// AgentName is the name of the agent.
	AgentName string

	// Relevance is the relevance score (0.0 to 1.0).
	Relevance float64
}
