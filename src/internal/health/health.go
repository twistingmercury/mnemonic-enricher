package health

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twistingmercury/heartbeat"
)

// checkTimeout is the per-check timeout used when probing dependencies.
const checkTimeout = 3 * time.Second

// Pinger is satisfied by *pgxpool.Pool and any type that supports Ping.
type Pinger interface {
	Ping(ctx context.Context) error
}

// ConnectivityVerifier is satisfied by neo4j.DriverWithContext and any type
// that supports VerifyConnectivity.
type ConnectivityVerifier interface {
	VerifyConnectivity(ctx context.Context) error
}

// Dependencies holds the external connections that health checks probe.
type Dependencies struct {
	PGPool      Pinger
	Neo4jDriver ConnectivityVerifier
}

var (
	healthDeps  *Dependencies
	descriptors []heartbeat.DependencyDescriptor
)

// Initialize configures the health check dependency descriptors using the
// provided database connections. It must be called before Descriptors or
// CheckHealth are used.
func Initialize(deps Dependencies) error {
	if deps.PGPool == nil {
		return errors.New("health: PostgreSQL pool is nil")
	}
	if deps.Neo4jDriver == nil {
		return errors.New("health: Neo4j driver is nil")
	}

	healthDeps = &deps

	descriptors = []heartbeat.DependencyDescriptor{
		{
			Name:        "PostgreSQL",
			Type:        "database",
			Timeout:     checkTimeout,
			HandlerFunc: checkPostgreSQLHealth,
		},
		{
			Name:        "Neo4j",
			Type:        "database",
			Timeout:     checkTimeout,
			HandlerFunc: checkNeo4jHealth,
		},
		{
			Name:        "OpenAI embedding model",
			Type:        "external_api",
			Timeout:     checkTimeout,
			HandlerFunc: checkEmbeddingModel,
		},
		{
			Name:        "OpenAI extraction model",
			Type:        "external_api",
			Timeout:     checkTimeout,
			HandlerFunc: checkExtractionModel,
		},
	}

	return nil
}

// Descriptors returns the registered dependency descriptors for use with the
// heartbeat Handler. It must be called after Initialize.
func Descriptors() []heartbeat.DependencyDescriptor {
	return descriptors
}

// CheckHealth runs all dependency checks and returns a joined error for any
// that are not healthy. This is intended for CLI health probes.
func CheckHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, depResults := heartbeat.CheckDependencies(ctx, descriptors)

	var errs error
	for _, ds := range depResults {
		if ds.Status != heartbeat.StatusOK && ds.Status != heartbeat.StatusNotSet {
			err := fmt.Errorf("failed to verify health for %s: %s", ds.Resource, ds.Message)
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

// checkPostgreSQLHealth pings the PostgreSQL connection pool.
func checkPostgreSQLHealth() (status heartbeat.StatusResult) {
	if healthDeps == nil || healthDeps.PGPool == nil {
		status.Status = heartbeat.StatusCritical
		status.Message = "PostgreSQL pool not initialized"
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	start := time.Now()
	if err := healthDeps.PGPool.Ping(ctx); err != nil {
		status.Status = heartbeat.StatusCritical
		status.Message = fmt.Sprintf("ping failed: %v", err)
		status.RequestDuration = float64(time.Since(start).Microseconds()) / 1000
		return status
	}

	status.Status = heartbeat.StatusOK
	status.Message = "ok"
	status.RequestDuration = float64(time.Since(start).Microseconds()) / 1000
	return status
}

// checkNeo4jHealth verifies Neo4j driver connectivity.
func checkNeo4jHealth() (status heartbeat.StatusResult) {
	if healthDeps == nil || healthDeps.Neo4jDriver == nil {
		status.Status = heartbeat.StatusCritical
		status.Message = "Neo4j driver not initialized"
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	start := time.Now()
	if err := healthDeps.Neo4jDriver.VerifyConnectivity(ctx); err != nil {
		status.Status = heartbeat.StatusCritical
		status.Message = fmt.Sprintf("connectivity check failed: %v", err)
		status.RequestDuration = float64(time.Since(start).Microseconds()) / 1000
		return status
	}

	status.Status = heartbeat.StatusOK
	status.Message = "ok"
	status.RequestDuration = float64(time.Since(start).Microseconds()) / 1000
	return status
}

// checkEmbeddingModel is a placeholder. OpenAI does not expose a lightweight
// ping endpoint, so we report StatusNotSet until a practical check is available.
func checkEmbeddingModel() (status heartbeat.StatusResult) {
	status.Status = heartbeat.StatusNotSet
	status.Message = "no ping endpoint available"
	return status
}

// checkExtractionModel is a placeholder. OpenAI does not expose a lightweight
// ping endpoint, so we report StatusNotSet until a practical check is available.
func checkExtractionModel() (status heartbeat.StatusResult) {
	status.Status = heartbeat.StatusNotSet
	status.Message = "no ping endpoint available"
	return status
}
