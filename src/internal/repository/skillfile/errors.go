package skillfile

import "errors"

// Common repository errors for skill file operations.
var (
	// ErrExists is returned when attempting to create a skill file with a
	// (skill_id, path) combination that already exists.
	ErrExists = errors.New("skill file already exists")

	// ErrNotFound is returned when a skill file with the specified identifier cannot be found.
	ErrNotFound = errors.New("skill file not found")
)
