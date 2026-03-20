package mcpserver

import "errors"

// ErrPatternNotFound is returned when a pattern ID does not exist.
// Applies to: get_pattern, find_related_patterns.
var ErrPatternNotFound = errors.New("pattern not found")

// ErrInvalidInput is returned when handler-level validation fails
// (constraints the SDK schema validation does not catch, such as
// limit exceeding maximum or threshold out of range).
// Applies to: all 3 tools.
var ErrInvalidInput = errors.New("invalid input")

// ErrServiceUnavailable is returned when a backend database
// (Postgres, Neo4j) is unreachable.
// Applies to: all 3 tools.
var ErrServiceUnavailable = errors.New("service unavailable")
