package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/mnemonic-enricher/internal/config"
)

// checkTimeout is the per-check timeout used when probing dependencies.
const checkTimeout = 3 * time.Second

// defaultOpenAIBaseURL is the OpenAI API root used when none is configured.
// It is a variable so unit tests can keep checks off the network.
var defaultOpenAIBaseURL = "https://api.openai.com/v1"

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
	OpenAI      config.OpenAIConfig
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

	openAIBaseURL := defaultOpenAIBaseURL
	if deps.OpenAI.BaseURL != "" {
		openAIBaseURL = strings.TrimRight(deps.OpenAI.BaseURL, "/")
	}

	descriptors = []heartbeat.DependencyDescriptor{
		{
			Name:      "PostgreSQL",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: checkPostgreSQLHealth,
		},
		{
			Name:      "Neo4j",
			Type:      "database",
			Timeout:   checkTimeout,
			CheckFunc: checkNeo4jHealth,
		},
		{
			Name:      "OpenAI embedding model",
			Type:      "external_api",
			Timeout:   checkTimeout,
			CheckFunc: openAIModelCheck(openAIBaseURL, deps.OpenAI.APIKey, deps.OpenAI.EmbeddingModel),
		},
		{
			Name:      "OpenAI extraction model",
			Type:      "external_api",
			Timeout:   checkTimeout,
			CheckFunc: openAIModelCheck(openAIBaseURL, deps.OpenAI.APIKey, deps.OpenAI.ExtractionModel),
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
// that are Critical. Warnings, such as OpenAI being unreachable, do not fail
// the probe. This is intended for CLI health probes.
func CheckHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, depResults := heartbeat.CheckDependencies(ctx, descriptors)

	var errs error
	for _, ds := range depResults {
		if ds.Status == heartbeat.StatusCritical {
			err := fmt.Errorf("failed to verify health for %s: %s", ds.Resource, ds.Message)
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

// checkPostgreSQLHealth pings the PostgreSQL connection pool. heartbeat
// cancels ctx when the check's timeout expires and records the duration.
func checkPostgreSQLHealth(ctx context.Context) (status heartbeat.StatusResult) {
	if healthDeps == nil || healthDeps.PGPool == nil {
		status.Status = heartbeat.StatusCritical
		status.Message = "PostgreSQL pool not initialized"
		return status
	}

	if err := healthDeps.PGPool.Ping(ctx); err != nil {
		status.Status = heartbeat.StatusCritical
		status.Message = fmt.Sprintf("ping failed: %v", err)
		return status
	}

	status.Status = heartbeat.StatusOK
	status.Message = "ok"
	return status
}

// checkNeo4jHealth verifies Neo4j driver connectivity. heartbeat cancels ctx
// when the check's timeout expires and records the duration.
func checkNeo4jHealth(ctx context.Context) (status heartbeat.StatusResult) {
	if healthDeps == nil || healthDeps.Neo4jDriver == nil {
		status.Status = heartbeat.StatusCritical
		status.Message = "Neo4j driver not initialized"
		return status
	}

	if err := healthDeps.Neo4jDriver.VerifyConnectivity(ctx); err != nil {
		status.Status = heartbeat.StatusCritical
		status.Message = fmt.Sprintf("connectivity check failed: %v", err)
		return status
	}

	status.Status = heartbeat.StatusOK
	status.Message = "ok"
	return status
}

// openAIModelCheck returns a check that retrieves model from the OpenAI models
// endpoint. The call is free and confirms that the API is reachable, the key
// is valid and the key can use the model. Failures are reported as Warning so
// an OpenAI outage shows in /health without failing the probe.
func openAIModelCheck(baseURL, apiKey, model string) heartbeat.CheckFunc {
	modelURL := baseURL + "/models/" + url.PathEscape(model)

	return func(ctx context.Context) (status heartbeat.StatusResult) {
		status.Resource = model

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
		if err != nil {
			status.Status = heartbeat.StatusWarning
			status.Message = "invalid OpenAI base URL"
			return status
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// The error can carry internal addresses, so it is not echoed.
			status.Status = heartbeat.StatusWarning
			status.Message = "request failed"
			if ctx.Err() != nil {
				status.Message = "request timed out"
			}
			return status
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

		status.StatusCode = resp.StatusCode
		if resp.StatusCode != http.StatusOK {
			status.Status = heartbeat.StatusWarning
			status.Message = fmt.Sprintf("models endpoint returned %s", resp.Status)
			return status
		}

		status.Status = heartbeat.StatusOK
		status.Message = "ok"
		return status
	}
}
