package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsValidEnrichmentStatus reports whether status is one of "pending",
// "enriched" or "failed".
func IsValidEnrichmentStatus(status string) bool {
	switch status {
	case "pending", "enriched", "failed":
		return true
	}
	return false
}

// DBTX is the subset of *pgxpool.Pool (and pgxmock) the repositories use.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
