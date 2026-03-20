package skill

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/twistingmercury/mnemonic/internal/repository"
)

// Repository defines data access operations for skills.
// Skills are stored as JSONB documents with name as the unique lookup key.
type Repository interface {
	// Create stores a new skill. The application computes crc64 from the
	// serialized definition before calling this method.
	// Returns ErrExists if name already exists.
	Create(ctx context.Context, skill *Skill) error

	// Get retrieves a skill by ID. Returns ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*Skill, error)

	// GetByName retrieves a skill by name. Returns ErrNotFound if not found.
	GetByName(ctx context.Context, name string) (*Skill, error)

	// Update modifies an existing skill. The application computes crc64
	// from the serialized definition and sets updated_at before calling
	// this method. Returns ErrNotFound if not found.
	Update(ctx context.Context, skill *Skill) error

	// Delete removes a skill by ID. Returns ErrNotFound if not found.
	Delete(ctx context.Context, id uuid.UUID) error

	// DeleteByName removes a skill by name. Returns ErrNotFound if not found.
	DeleteByName(ctx context.Context, name string) error

	// List retrieves all skills with optional pagination.
	// Returns the skills, total count, and any error.
	List(ctx context.Context, opts repository.ListOptions) ([]*Skill, int64, error)

	// Exists checks if a skill with the given name exists.
	Exists(ctx context.Context, name string) (bool, error)

	// GetManifest returns name and crc64 for all skills (used by sync protocol).
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

// Create stores a new skill in the database.
// The database generates the UUID and sets created_at/updated_at via defaults.
func (r *pgxRepository) Create(ctx context.Context, skill *Skill) error {
	query := `
		INSERT INTO skills (name, definition, crc64)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		skill.Name,
		skill.Definition,
		skill.CRC64,
	).Scan(&skill.ID, &skill.CreatedAt, &skill.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == repository.PgErrCodeUniqueViolation {
			return ErrExists
		}
		return err
	}

	return nil
}

// Get retrieves a skill by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Skill, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at
		FROM skills
		WHERE id = $1
	`

	var s Skill
	err := r.db.QueryRow(ctx, query, id).Scan(
		&s.ID,
		&s.Name,
		&s.Definition,
		&s.CRC64,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &s, nil
}

// GetByName retrieves a skill by name from the database.
func (r *pgxRepository) GetByName(ctx context.Context, name string) (*Skill, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at
		FROM skills
		WHERE name = $1
	`

	var s Skill
	err := r.db.QueryRow(ctx, query, name).Scan(
		&s.ID,
		&s.Name,
		&s.Definition,
		&s.CRC64,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &s, nil
}

// Update modifies an existing skill in the database.
// The application must set skill.UpdatedAt before calling this method.
func (r *pgxRepository) Update(ctx context.Context, skill *Skill) error {
	now := time.Now()
	query := `
		UPDATE skills
		SET definition = $2, crc64 = $3, updated_at = $4
		WHERE name = $1
	`

	result, err := r.db.Exec(ctx, query,
		skill.Name,
		skill.Definition,
		skill.CRC64,
		now,
	)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	skill.UpdatedAt = now
	return nil
}

// Delete removes a skill by ID from the database.
func (r *pgxRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM skills WHERE id = $1`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteByName removes a skill by name from the database.
func (r *pgxRepository) DeleteByName(ctx context.Context, name string) error {
	query := `DELETE FROM skills WHERE name = $1`

	result, err := r.db.Exec(ctx, query, name)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List retrieves all skills with optional pagination.
func (r *pgxRepository) List(ctx context.Context, opts repository.ListOptions) ([]*Skill, int64, error) {
	query := `
		SELECT id, name, definition, crc64, created_at, updated_at,
		       COUNT(*) OVER() AS total_count
		FROM skills
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

	skills := make([]*Skill, 0)
	var totalCount int64

	for rows.Next() {
		var s Skill
		err := rows.Scan(
			&s.ID,
			&s.Name,
			&s.Definition,
			&s.CRC64,
			&s.CreatedAt,
			&s.UpdatedAt,
			&totalCount,
		)
		if err != nil {
			return nil, 0, err
		}
		skills = append(skills, &s)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return skills, totalCount, nil
}

// Exists checks if a skill with the given name exists.
func (r *pgxRepository) Exists(ctx context.Context, name string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM skills WHERE name = $1)`

	var exists bool
	err := r.db.QueryRow(ctx, query, name).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// GetManifest returns name and crc64 for all skills, ordered by name.
// Used by the sync protocol to determine which skills have changed.
func (r *pgxRepository) GetManifest(ctx context.Context) ([]ManifestEntry, error) {
	query := `SELECT name, crc64 FROM skills ORDER BY name ASC`

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
