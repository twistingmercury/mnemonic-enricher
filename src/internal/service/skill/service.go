// Package skill provides the service layer for skill lifecycle management.
// Skills are Postgres-only (no Neo4j sync). The service marshals flat input
// types into a JSONB definition document, computes CRC64 checksums for change
// detection, and translates repository errors to service-level sentinels.
//
// Documentation:
//   - Design: docs/design/service-layer.md (SkillService)
//   - Repository: internal/repository/skill
package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc64"
	"strconv"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/repository"
	skillrepo "github.com/twistingmercury/mnemonic/internal/repository/skill"
	"github.com/twistingmercury/mnemonic/internal/service"
)

// Service defines the operations for managing skills.
// Skills are Postgres-only (no Neo4j sync).
type Service interface {
	// Create stores a new skill. Computes crc64 from the definition.
	Create(ctx context.Context, input CreateInput) (*skillrepo.Skill, error)

	// GetByName retrieves a skill by name. Returns service.ErrNotFound if not found.
	GetByName(ctx context.Context, name string) (*skillrepo.Skill, error)

	// GetByID retrieves a skill by UUID. Returns service.ErrNotFound if not found.
	GetByID(ctx context.Context, id uuid.UUID) (*skillrepo.Skill, error)

	// Update replaces a skill definition. Computes new crc64, sets updated_at.
	Update(ctx context.Context, name string, input UpdateInput) (*skillrepo.Skill, error)

	// Delete removes a skill by name. CASCADE deletes skill_files.
	Delete(ctx context.Context, name string) error

	// List retrieves skills with pagination.
	List(ctx context.Context, opts ListOptions) ([]*skillrepo.Skill, int64, error)

	// GetManifest returns sync manifest entries for all skills.
	GetManifest(ctx context.Context) ([]skillrepo.ManifestEntry, error)
}

// CreateInput contains fields for creating a skill.
type CreateInput struct {
	Name          string
	Description   string
	Content       string
	Tags          []string
	License       *string
	Compatibility *string
	Metadata      map[string]string
	AllowedTools  []string
	Version       string
}

// UpdateInput contains fields for updating a skill.
type UpdateInput struct {
	Description   string
	Content       string
	Tags          []string
	License       *string
	Compatibility *string
	Metadata      map[string]string
	AllowedTools  []string
	Version       string
}

// ListOptions for service-layer pagination.
type ListOptions struct {
	Offset int
	Limit  int
}

// skillDefinition is the JSON structure stored in the definition JSONB column.
type skillDefinition struct {
	Description   string            `json:"description"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags,omitempty"`
	License       *string           `json:"license,omitempty"`
	Compatibility *string           `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Version       string            `json:"version"`
}

// skillService implements Service.
type skillService struct {
	repo   skillrepo.Repository
	logger zerolog.Logger
}

// New creates a new skill service backed by the given repository.
func New(repo skillrepo.Repository, logger zerolog.Logger) Service {
	return &skillService{
		repo:   repo,
		logger: logger.With().Str("service", "skill").Logger(),
	}
}

// Create marshals the input into a definition, computes CRC64, and stores the skill.
func (s *skillService) Create(ctx context.Context, input CreateInput) (*skillrepo.Skill, error) {
	//nolint:gocritic
	def := skillDefinition{
		Description:   input.Description,
		Content:       input.Content,
		Tags:          input.Tags,
		License:       input.License,
		Compatibility: input.Compatibility,
		Metadata:      input.Metadata,
		AllowedTools:  input.AllowedTools,
		Version:       input.Version,
	}

	defJSON, err := marshalDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("marshal definition: %w", err)
	}

	skill := &skillrepo.Skill{
		Name:       input.Name,
		Definition: defJSON,
		CRC64:      computeCRC64(defJSON),
	}

	if err := s.repo.Create(ctx, skill); err != nil {
		if errors.Is(err, skillrepo.ErrExists) {
			return nil, fmt.Errorf("%w: skill %q", service.ErrConflict, input.Name)
		}
		return nil, fmt.Errorf("create skill: %w", err)
	}

	return skill, nil
}

// GetByName retrieves a skill by name, translating repository errors.
func (s *skillService) GetByName(ctx context.Context, name string) (*skillrepo.Skill, error) {
	skill, err := s.repo.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, name)
		}
		return nil, fmt.Errorf("get skill: %w", err)
	}
	return skill, nil
}

// GetByID retrieves a skill by UUID, translating repository errors.
func (s *skillService) GetByID(ctx context.Context, id uuid.UUID) (*skillrepo.Skill, error) {
	skill, err := s.repo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, id)
		}
		return nil, fmt.Errorf("get skill: %w", err)
	}
	return skill, nil
}

// Update retrieves the existing skill by name, marshals the new definition,
// computes CRC64, and updates the record.
func (s *skillService) Update(ctx context.Context, name string, input UpdateInput) (*skillrepo.Skill, error) {
	existing, err := s.repo.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, name)
		}
		return nil, fmt.Errorf("get skill for update: %w", err)
	}

	def := skillDefinition(input)

	defJSON, err := marshalDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("marshal definition: %w", err)
	}

	existing.Definition = defJSON
	existing.CRC64 = computeCRC64(defJSON)

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}

	return existing, nil
}

// Delete resolves the skill by name and deletes it by ID.
func (s *skillService) Delete(ctx context.Context, name string) error {
	existing, err := s.repo.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) {
			return fmt.Errorf("%w: skill %q", service.ErrNotFound, name)
		}
		return fmt.Errorf("get skill for delete: %w", err)
	}

	if err := s.repo.Delete(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	return nil
}

// List delegates to the repository with mapped pagination options.
func (s *skillService) List(ctx context.Context, opts ListOptions) ([]*skillrepo.Skill, int64, error) {
	return s.repo.List(ctx, repository.ListOptions{
		Offset: opts.Offset,
		Limit:  opts.Limit,
	})
}

// GetManifest delegates to the repository.
func (s *skillService) GetManifest(ctx context.Context) ([]skillrepo.ManifestEntry, error) {
	return s.repo.GetManifest(ctx)
}

// marshalDefinition serializes a skillDefinition to JSON.
func marshalDefinition(def skillDefinition) (json.RawMessage, error) {
	data, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// computeCRC64 computes a CRC-64/ECMA checksum of the given data and returns
// it as a decimal string.
func computeCRC64(data []byte) string {
	table := crc64.MakeTable(crc64.ECMA)
	checksum := crc64.Checksum(data, table)
	return strconv.FormatUint(checksum, 10)
}
