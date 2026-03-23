package skill

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Skill represents a stored skill document.
type Skill struct {
	ID         uuid.UUID       `json:"id"`
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
	CRC64      string          `json:"crc64"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ManifestEntry holds the name and checksum for a skill.
// Used by the sync protocol to detect changes without fetching full definitions.
type ManifestEntry struct {
	Name  string `json:"name"`
	CRC64 string `json:"crc64"`
}

// Common repository errors for skill operations.
var (
	// ErrExists is returned when attempting to create a skill with a name that already exists.
	ErrExists = errors.New("skill already exists")

	// ErrNotFound is returned when a skill with the specified ID or name cannot be found.
	ErrNotFound = errors.New("skill not found")
)
