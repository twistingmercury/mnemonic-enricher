package skillfile

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// SkillFile represents a child file belonging to a skill.
type SkillFile struct {
	ID        uuid.UUID `json:"id"`
	SkillID   uuid.UUID `json:"skill_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	CRC64     string    `json:"crc64"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ManifestEntry holds the path and checksum for a skill file.
// Used by the sync protocol to detect changes without fetching full content.
type ManifestEntry struct {
	Path  string `json:"path"`
	CRC64 string `json:"crc64"`
}

// Common repository errors for skill file operations.
var (
	// ErrExists is returned when attempting to create a skill file with a
	// (skill_id, path) combination that already exists.
	ErrExists = errors.New("skill file already exists")

	// ErrNotFound is returned when a skill file with the specified ID or path
	// cannot be found.
	ErrNotFound = errors.New("skill file not found")
)
