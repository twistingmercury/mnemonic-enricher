// Package agent provides the business logic layer for agent lifecycle management.
// It coordinates between the PostgreSQL agent repository and the Neo4j graph
// repository, handling CRC64 computation, definition marshaling, and best-effort
// graph synchronization.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc64"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/repository"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	graphrepo "github.com/twistingmercury/mnemonic/internal/repository/graph"
	"github.com/twistingmercury/mnemonic/internal/service"
)

// Service defines the operations for managing agent lifecycle.
// Name-to-UUID resolution is handled internally; callers use agent names.
type Service interface {
	// Create stores a new agent in Postgres and syncs to Neo4j (best-effort).
	// The service computes crc64 from the definition before storage.
	Create(ctx context.Context, input CreateInput) (*agentrepo.Agent, error)

	// Get retrieves an agent by name. Returns service.ErrNotFound if not found.
	Get(ctx context.Context, name string) (*agentrepo.Agent, error)

	// Update replaces an agent definition. Computes new crc64, sets updated_at,
	// then best-effort syncs to Neo4j.
	Update(ctx context.Context, name string, input UpdateInput) (*agentrepo.Agent, error)

	// Delete removes an agent from Postgres (CASCADE removes associations)
	// and best-effort deletes from Neo4j.
	Delete(ctx context.Context, name string) error

	// List retrieves agents with pagination.
	List(ctx context.Context, opts ListOptions) ([]*agentrepo.Agent, int64, error)

	// GetManifest returns sync manifest entries for all agents.
	GetManifest(ctx context.Context) ([]agentrepo.ManifestEntry, error)
}

// CreateInput contains fields for creating an agent.
// The service marshals these into the repository's definition JSONB column.
type CreateInput struct {
	Name         string
	Description  string
	SystemPrompt string
	Model        string
	AllowedTools []string
	Version      string
}

// UpdateInput contains fields for updating an agent.
type UpdateInput struct {
	Description  string
	SystemPrompt string
	Model        string
	AllowedTools []string
	Version      string
}

// ListOptions for service-layer pagination.
type ListOptions struct {
	Offset int
	Limit  int
}

// agentService implements the Service interface.
type agentService struct {
	repo      agentrepo.Repository
	graphRepo graphrepo.Repository
	logger    zerolog.Logger
}

// New creates a new agent Service backed by the given repositories.
func New(repo agentrepo.Repository, graphRepo graphrepo.Repository, logger zerolog.Logger) Service {
	return &agentService{
		repo:      repo,
		graphRepo: graphRepo,
		logger:    logger,
	}
}

// Create stores a new agent and syncs to Neo4j best-effort.
func (s *agentService) Create(ctx context.Context, input CreateInput) (*agentrepo.Agent, error) {
	//nolint:gocritic
	def := agentDefinition{
		Description:  input.Description,
		SystemPrompt: input.SystemPrompt,
		Model:        input.Model,
		AllowedTools: input.AllowedTools,
		Version:      input.Version,
	}

	definition, crc, err := marshalDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	agent := agentrepo.Agent{
		Name:       input.Name,
		Definition: definition,
		CRC64:      crc,
	}

	if err := s.repo.Create(ctx, &agent); err != nil {
		if errors.Is(err, agentrepo.ErrExists) {
			return nil, fmt.Errorf("%w: agent %q", service.ErrConflict, input.Name)
		}
		return nil, fmt.Errorf("create agent: %w", err)
	}

	s.syncNeo4j(fmt.Sprintf("agent:%s", input.Name), func() error {
		return s.graphRepo.SyncAgent(ctx, input.Name)
	})

	return &agent, nil
}

// Get retrieves an agent by name.
func (s *agentService) Get(ctx context.Context, name string) (*agentrepo.Agent, error) {
	agent, err := s.repo.Get(ctx, name)
	if err != nil {
		if errors.Is(err, agentrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent %q", service.ErrNotFound, name)
		}
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return agent, nil
}

// Update replaces an agent definition with new values.
func (s *agentService) Update(ctx context.Context, name string, input UpdateInput) (*agentrepo.Agent, error) {
	agent, err := s.repo.Get(ctx, name)
	if err != nil {
		if errors.Is(err, agentrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: agent %q", service.ErrNotFound, name)
		}
		return nil, fmt.Errorf("update agent: %w", err)
	}

	def := agentDefinition(input)

	definition, crc, err := marshalDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}

	agent.Definition = definition
	agent.CRC64 = crc

	if err := s.repo.Update(ctx, agent); err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}

	s.syncNeo4j(fmt.Sprintf("agent:%s", name), func() error {
		return s.graphRepo.SyncAgent(ctx, name)
	})

	return agent, nil
}

// Delete removes an agent from Postgres and best-effort from Neo4j.
func (s *agentService) Delete(ctx context.Context, name string) error {
	if err := s.repo.Delete(ctx, name); err != nil {
		if errors.Is(err, agentrepo.ErrNotFound) {
			return fmt.Errorf("%w: agent %q", service.ErrNotFound, name)
		}
		return fmt.Errorf("delete agent: %w", err)
	}

	s.syncNeo4j(fmt.Sprintf("agent:%s", name), func() error {
		return s.graphRepo.DeleteAgent(ctx, name)
	})

	return nil
}

// List retrieves agents with pagination.
func (s *agentService) List(ctx context.Context, opts ListOptions) ([]*agentrepo.Agent, int64, error) {
	agents, total, err := s.repo.List(ctx, repository.ListOptions{
		Offset: opts.Offset,
		Limit:  opts.Limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list agents: %w", err)
	}
	return agents, total, nil
}

// GetManifest returns sync manifest entries for all agents.
func (s *agentService) GetManifest(ctx context.Context) ([]agentrepo.ManifestEntry, error) {
	entries, err := s.repo.GetManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("get agent manifest: %w", err)
	}
	return entries, nil
}

// syncNeo4j runs fn as a best-effort Neo4j operation.
// Errors are logged but not returned to the caller.
func (s *agentService) syncNeo4j(entityDesc string, fn func() error) {
	if err := fn(); err != nil {
		s.logger.Warn().
			Err(err).
			Str("entity", entityDesc).
			Msg("neo4j sync failed")
	}
}

// agentDefinition is the JSON structure stored in the definition JSONB column.
type agentDefinition struct {
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Version      string   `json:"version"`
}

// crc64Table is the pre-computed CRC-64/ISO lookup table, allocated once at package init.
var crc64Table = crc64.MakeTable(crc64.ISO)

// computeCRC64 returns the CRC-64 checksum of data as a decimal string.
func computeCRC64(data []byte) string {
	return strconv.FormatUint(crc64.Checksum(data, crc64Table), 10)
}

// marshalDefinition marshals v to JSON and computes its CRC-64 checksum.
func marshalDefinition(v any) (json.RawMessage, string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, "", fmt.Errorf("marshal definition: %w", err)
	}
	return json.RawMessage(data), computeCRC64(data), nil
}
