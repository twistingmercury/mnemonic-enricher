package agent

import "errors"

// Common repository errors for agent operations.
var (
	// ErrExists is returned when attempting to create an agent with a name that already exists.
	ErrExists = errors.New("agent already exists")

	// ErrNotFound is returned when an agent with the specified identifier cannot be found.
	ErrNotFound = errors.New("agent not found")
)
