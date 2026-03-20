package skill

import "errors"

// Common repository errors for skill operations.
var (
	// ErrExists is returned when attempting to create a skill with a name that already exists.
	ErrExists = errors.New("skill already exists")

	// ErrNotFound is returned when a skill with the specified identifier cannot be found.
	ErrNotFound = errors.New("skill not found")
)
