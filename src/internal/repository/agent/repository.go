package agent

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/twistingmercury/mnemonic/internal/repository"
)

// Repository defines data access operations for agents.
// Agents are stored as JSONB documents with name as the unique lookup key.
type Repository interface {
	// Create stores a new agent. The application computes crc64 from the
	// serialized definition before calling this method.
	// Returns ErrExists if name already exists.
	Create(ctx context.Context, agent *Agent) error

	// Get retrieves an agent by name. Returns ErrNotFound if not found.
	Get(ctx context.Context, name string) (*Agent, error)

	// GetByID retrieves an agent by UUID. Returns ErrNotFound if not found.
	GetByID(ctx context.Context, id uuid.UUID) (*Agent, error)

	// Update modifies an existing agent. The application computes crc64
	// from the serialized definition and sets updated_at before calling
	// this method. Returns ErrNotFound if not found.
	Update(ctx context.Context, agent *Agent) error

	// Delete removes an agent by name. Returns ErrNotFound if not found.
	Delete(ctx context.Context, name string) error

	// DeleteByID removes an agent by UUID. Returns ErrNotFound if not found.
	DeleteByID(ctx context.Context, id uuid.UUID) error

	// List retrieves all agents with optional pagination.
	// Returns the agents, total count, and any error.
	List(ctx context.Context, opts repository.ListOptions) ([]*Agent, int64, error)

	// Exists checks if an agent with the given name exists.
	Exists(ctx context.Context, name string) (bool, error)

	// GetManifest returns name and crc64 for all agents (used by sync protocol).
	GetManifest(ctx context.Context) ([]ManifestEntry, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Create stores a new agent in the database.
// The database generates the UUID and sets created_at/updated_at via defaults.
func (r *pgxRepository) Create(ctx context.Context, agent *Agent) error {
	query := `
		INSERT INTO agents (name, definition, crc64)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		agent.Name,
		agent.Definition,
		agent.CRC64,
	).Scan(&agent.ID, &agent.CreatedAt, &agent.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == repository.PgErrCodeUniqueViolation {
			return ErrExists
		}
		return err
	}

	return nil
}

// Get retrieves an agent by name from the database.
func (r *pgxRepository) Get(ctx context.Context, name string) (*Agent, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at
		FROM agents
		WHERE name = $1
	`

	var agent Agent
	err := r.db.QueryRow(ctx, query, name).Scan(
		&agent.ID,
		&agent.Name,
		&agent.Definition,
		&agent.CRC64,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &agent, nil
}

// GetByID retrieves an agent by UUID from the database.
func (r *pgxRepository) GetByID(ctx context.Context, id uuid.UUID) (*Agent, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at
		FROM agents
		WHERE id = $1
	`

	var agent Agent
	err := r.db.QueryRow(ctx, query, id).Scan(
		&agent.ID,
		&agent.Name,
		&agent.Definition,
		&agent.CRC64,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &agent, nil
}

// Update modifies an existing agent in the database.
// The application must set agent.UpdatedAt before calling this method.
func (r *pgxRepository) Update(ctx context.Context, agent *Agent) error {
	now := time.Now()
	query := `
		UPDATE agents
		SET definition = $2, crc64 = $3, updated_at = $4
		WHERE name = $1
	`

	result, err := r.db.Exec(ctx, query,
		agent.Name,
		agent.Definition,
		agent.CRC64,
		now,
	)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	agent.UpdatedAt = now
	return nil
}

// Delete removes an agent by name from the database.
func (r *pgxRepository) Delete(ctx context.Context, name string) error {
	query := `DELETE FROM agents WHERE name = $1`

	result, err := r.db.Exec(ctx, query, name)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteByID removes an agent by UUID from the database.
func (r *pgxRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM agents WHERE id = $1`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List retrieves all agents with optional pagination.
func (r *pgxRepository) List(ctx context.Context, opts repository.ListOptions) ([]*Agent, int64, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at,
		       COUNT(*) OVER() AS total_count
		FROM agents
		ORDER BY name ASC
	`

	args := make([]any, 0, 2)
	if opts.Limit > 0 {
		query += " LIMIT $1"
		args = append(args, opts.Limit)
		if opts.Offset > 0 {
			query += " OFFSET $2"
			args = append(args, opts.Offset)
		}
	} else if opts.Offset > 0 {
		query += " OFFSET $1"
		args = append(args, opts.Offset)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	agents := make([]*Agent, 0)
	var totalCount int64

	for rows.Next() {
		var a Agent
		err := rows.Scan(
			&a.ID,
			&a.Name,
			&a.Definition,
			&a.CRC64,
			&a.CreatedAt,
			&a.UpdatedAt,
			&totalCount,
		)
		if err != nil {
			return nil, 0, err
		}
		agents = append(agents, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return agents, totalCount, nil
}

// Exists checks if an agent with the given name exists.
func (r *pgxRepository) Exists(ctx context.Context, name string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM agents WHERE name = $1)`

	var exists bool
	err := r.db.QueryRow(ctx, query, name).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// GetManifest returns name and crc64 for all agents, ordered by name.
// Used by the sync protocol to determine which agents have changed.
func (r *pgxRepository) GetManifest(ctx context.Context) ([]ManifestEntry, error) {
	query := `SELECT name, crc64 FROM agents ORDER BY name ASC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]ManifestEntry, 0)
	for rows.Next() {
		var entry ManifestEntry
		if err := rows.Scan(&entry.Name, &entry.CRC64); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
