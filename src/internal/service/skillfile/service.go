// Package skillfile provides the service layer for skill file lifecycle
// management. Skill files are child files (scripts, references, assets)
// belonging to skill definitions. The service resolves skill names to UUIDs
// via the skill repository, constructs file paths from type and filename
// components, computes CRC64 checksums, and translates repository errors
// to service-level sentinels.
//
// Documentation:
//   - Design: docs/design/service-layer.md (SkillFileService)
//   - Repository: internal/repository/skillfile
package skillfile

import (
	"context"
	"errors"
	"fmt"
	"hash/crc64"
	"strconv"
	"strings"

	skillrepo "github.com/twistingmercury/mnemonic/internal/repository/skill"
	skillfilerepo "github.com/twistingmercury/mnemonic/internal/repository/skillfile"
	"github.com/twistingmercury/mnemonic/internal/service"

	"github.com/rs/zerolog"
)

// Service defines the operations for managing skill files.
// Each method resolves skillName to skillID via the skill repository.
type Service interface {
	// Create stores a new file for a skill. Resolves skillName -> skillID.
	Create(ctx context.Context, skillName string, fileType string, input CreateInput) (*skillfilerepo.SkillFile, error)

	// Get retrieves a file by (skillName, fileType, filename).
	Get(ctx context.Context, skillName string, fileType string, filename string) (*skillfilerepo.SkillFile, error)

	// Update replaces a file's content. Resolves skillName -> skillID.
	Update(ctx context.Context, skillName string, fileType string, filename string, input UpdateInput) (*skillfilerepo.SkillFile, error)

	// Delete removes a file. Resolves skillName -> skillID.
	Delete(ctx context.Context, skillName string, fileType string, filename string) error

	// ListBySkill retrieves all files for a skill, optionally filtered by type.
	ListBySkill(ctx context.Context, skillName string, fileType *string) ([]*skillfilerepo.SkillFile, error)
}

// CreateInput contains fields for creating a skill file.
type CreateInput struct {
	Filename    string
	ContentType string
	Content     string
	Encoding    string // "utf-8" or "base64"
}

// UpdateInput contains fields for updating a skill file.
type UpdateInput struct {
	ContentType string
	Content     string
	Encoding    string
}

// skillFileService implements Service.
type skillFileService struct {
	fileRepo  skillfilerepo.Repository
	skillRepo skillrepo.Repository
	logger    zerolog.Logger
}

// New creates a new skill file service backed by the given repositories.
func New(fileRepo skillfilerepo.Repository, skillRepo skillrepo.Repository, logger zerolog.Logger) Service {
	return &skillFileService{
		fileRepo:  fileRepo,
		skillRepo: skillRepo,
		logger:    logger.With().Str("service", "skillfile").Logger(),
	}
}

// Create resolves the skill, constructs the file path, computes CRC64, and
// stores the file.
func (s *skillFileService) Create(ctx context.Context, skillName string, fileType string, input CreateInput) (*skillfilerepo.SkillFile, error) {
	skill, err := s.resolveSkill(ctx, skillName)
	if err != nil {
		return nil, err
	}

	path := buildPath(fileType, input.Filename)

	file := &skillfilerepo.SkillFile{
		SkillID: skill.ID,
		Path:    path,
		Content: input.Content,
		CRC64:   computeCRC64(input.Content),
	}

	if err := s.fileRepo.Create(ctx, file); err != nil {
		if errors.Is(err, skillfilerepo.ErrExists) {
			return nil, fmt.Errorf("%w: file %q in skill %q", service.ErrConflict, path, skillName)
		}
		return nil, fmt.Errorf("create skill file: %w", err)
	}

	return file, nil
}

// Get resolves the skill, constructs the path, and retrieves the file.
func (s *skillFileService) Get(ctx context.Context, skillName string, fileType string, filename string) (*skillfilerepo.SkillFile, error) {
	skill, err := s.resolveSkill(ctx, skillName)
	if err != nil {
		return nil, err
	}

	path := buildPath(fileType, filename)

	file, err := s.fileRepo.GetByPath(ctx, skill.ID, path)
	if err != nil {
		if errors.Is(err, skillfilerepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: file %q in skill %q", service.ErrNotFound, path, skillName)
		}
		return nil, fmt.Errorf("get skill file: %w", err)
	}

	return file, nil
}

// Update resolves the skill, retrieves the existing file, updates fields,
// computes new CRC64, and persists.
func (s *skillFileService) Update(ctx context.Context, skillName string, fileType string, filename string, input UpdateInput) (*skillfilerepo.SkillFile, error) {
	skill, err := s.resolveSkill(ctx, skillName)
	if err != nil {
		return nil, err
	}

	path := buildPath(fileType, filename)

	existing, err := s.fileRepo.GetByPath(ctx, skill.ID, path)
	if err != nil {
		if errors.Is(err, skillfilerepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: file %q in skill %q", service.ErrNotFound, path, skillName)
		}
		return nil, fmt.Errorf("get skill file for update: %w", err)
	}

	existing.Content = input.Content
	existing.CRC64 = computeCRC64(input.Content)

	if err := s.fileRepo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update skill file: %w", err)
	}

	return existing, nil
}

// Delete resolves the skill, retrieves the file to get its ID, and deletes it.
func (s *skillFileService) Delete(ctx context.Context, skillName string, fileType string, filename string) error {
	skill, err := s.resolveSkill(ctx, skillName)
	if err != nil {
		return err
	}

	path := buildPath(fileType, filename)

	existing, err := s.fileRepo.GetByPath(ctx, skill.ID, path)
	if err != nil {
		if errors.Is(err, skillfilerepo.ErrNotFound) {
			return fmt.Errorf("%w: file %q in skill %q", service.ErrNotFound, path, skillName)
		}
		return fmt.Errorf("get skill file for delete: %w", err)
	}

	if err := s.fileRepo.Delete(ctx, existing.ID); err != nil {
		return fmt.Errorf("delete skill file: %w", err)
	}

	return nil
}

// ListBySkill resolves the skill and retrieves all its files. If fileType is
// non-nil, filters results client-side by path prefix.
func (s *skillFileService) ListBySkill(ctx context.Context, skillName string, fileType *string) ([]*skillfilerepo.SkillFile, error) {
	skill, err := s.resolveSkill(ctx, skillName)
	if err != nil {
		return nil, err
	}

	files, err := s.fileRepo.ListBySkill(ctx, skill.ID)
	if err != nil {
		return nil, fmt.Errorf("list skill files: %w", err)
	}

	if fileType != nil {
		prefix := *fileType + "/"
		filtered := make([]*skillfilerepo.SkillFile, 0, len(files))
		for _, f := range files {
			if strings.HasPrefix(f.Path, prefix) {
				filtered = append(filtered, f)
			}
		}
		return filtered, nil
	}

	return files, nil
}

// resolveSkill looks up a skill by name and translates repository errors.
func (s *skillFileService) resolveSkill(ctx context.Context, skillName string) (*skillrepo.Skill, error) {
	skill, err := s.skillRepo.GetByName(ctx, skillName)
	if err != nil {
		if errors.Is(err, skillrepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, skillName)
		}
		return nil, fmt.Errorf("resolve skill: %w", err)
	}
	return skill, nil
}

// buildPath constructs a file path from the file type and filename.
// Example: buildPath("scripts", "build.sh") returns "scripts/build.sh".
func buildPath(fileType string, filename string) string {
	return fileType + "/" + filename
}

// computeCRC64 computes a CRC-64/ECMA checksum of the given content and
// returns it as a decimal string.
func computeCRC64(content string) string {
	table := crc64.MakeTable(crc64.ECMA)
	checksum := crc64.Checksum([]byte(content), table)
	return strconv.FormatUint(checksum, 10)
}
