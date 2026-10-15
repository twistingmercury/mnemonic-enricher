package enrichmentjob

import "errors"

// ErrNotFound is returned when an enrichment job with the specified ID cannot be found.
var ErrNotFound = errors.New("enrichment job not found")
