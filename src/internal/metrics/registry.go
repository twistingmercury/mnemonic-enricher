package metrics

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Registry holds all metric instruments for the application.
type Registry struct {
	Jobs     *Jobs
	Database *Database
}

// NewRegistry creates all metric instruments using the provided meter.
func NewRegistry(meter metric.Meter) (*Registry, error) {
	jobs, err := NewJobs(meter)
	if err != nil {
		return nil, fmt.Errorf("job metrics: %w", err)
	}

	database, err := NewDatabase(meter)
	if err != nil {
		return nil, fmt.Errorf("database metrics: %w", err)
	}

	return &Registry{
		Jobs:     jobs,
		Database: database,
	}, nil
}
