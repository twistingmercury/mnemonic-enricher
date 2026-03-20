// Package database provides factory functions for creating database connections
// used by the mnemonic server. It handles PostgreSQL pool creation and Neo4j
// driver initialization with configuration-driven settings.
package database

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jcfg "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/twistingmercury/mnemonic/internal/config"
)

// NewPostgresPool creates a pgxpool.Pool configured from the provided PostgresConfig.
// The caller is responsible for calling pool.Close() when done.
func NewPostgresPool(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parsing postgres DSN: %w", err)
	}

	poolCfg.MaxConns = safeIntToInt32(cfg.MaxOpenConns)
	poolCfg.MinConns = safeIntToInt32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return pool, nil
}

// NewNeo4jDriver creates a neo4j.DriverWithContext configured from the provided Neo4jConfig.
// The caller is responsible for calling driver.Close(ctx) when done.
func NewNeo4jDriver(ctx context.Context, cfg config.Neo4jConfig) (neo4j.DriverWithContext, error) {
	username, password := cfg.Credentials()

	driver, err := neo4j.NewDriverWithContext(
		cfg.URI,
		neo4j.BasicAuth(username, password, ""),
		func(driverCfg *neo4jcfg.Config) {
			driverCfg.MaxConnectionPoolSize = cfg.MaxConnectionPoolSize
			driverCfg.ConnectionAcquisitionTimeout = cfg.ConnectionAcquisitionTimeout
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating neo4j driver: %w", err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("verifying neo4j connectivity: %w", err)
	}

	return driver, nil
}

// safeIntToInt32 converts an int to int32 with clamping to prevent overflow.
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) // #nosec G115 -- bounds checked above
}
