package skillfile

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/twistingmercury/mnemonic/internal/repository"
)

// Repository defines data access operations for skill child files.
// Skill files are keyed by (skill_id, path).
type Repository interface {
	// Create stores a new skill file. Returns ErrExists if the
	// (skill_id, path) combination already exists.
	Create(ctx context.Context, file *SkillFile) error

	// Get retrieves a skill file by ID. Returns ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*SkillFile, error)

	// GetByPath retrieves a skill file by skill ID and path.
	// Returns ErrNotFound if not found.
	GetByPath(ctx context.Context, skillID uuid.UUID, path string) (*SkillFile, error)

	// Update modifies an existing skill file. Returns ErrNotFound if not found.
	Update(ctx context.Context, file *SkillFile) error

	// Delete removes a skill file by ID. Returns ErrNotFound if not found.
	Delete(ctx context.Context, id uuid.UUID) error

	// ListBySkill retrieves all files for a given skill.
	ListBySkill(ctx context.Context, skillID uuid.UUID) ([]*SkillFile, error)

	// GetManifest returns path and crc64 for all files of a skill
	// (used by sync protocol).
	GetManifest(ctx context.Context, skillID uuid.UUID) ([]ManifestEntry, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Create stores a new skill file in the database.
// The database generates the UUID and sets created_at/updated_at via defaults.
func (r *pgxRepository) Create(ctx context.Context, file *SkillFile) error {
	query := `
		INSERT INTO skill_files (skill_id, path, content, crc64)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		file.SkillID,
		file.Path,
		file.Content,
		file.CRC64,
	).Scan(&file.ID, &file.CreatedAt, &file.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == repository.PgErrCodeUniqueViolation {
			return ErrExists
		}
		return err
	}

	return nil
}

// Get retrieves a skill file by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*SkillFile, error) {
	query := `
		SELECT id, skill_id, path, content, crc64, created_at, updated_at
		FROM skill_files
		WHERE id = $1
	`

	var f SkillFile
	err := r.db.QueryRow(ctx, query, id).Scan(
		&f.ID,
		&f.SkillID,
		&f.Path,
		&f.Content,
		&f.CRC64,
		&f.CreatedAt,
		&f.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &f, nil
}

// GetByPath retrieves a skill file by skill ID and path from the database.
func (r *pgxRepository) GetByPath(ctx context.Context, skillID uuid.UUID, path string) (*SkillFile, error) {
	query := `
		SELECT id, skill_id, path, content, crc64, created_at, updated_at
		FROM skill_files
		WHERE skill_id = $1 AND path = $2
	`

	var f SkillFile
	err := r.db.QueryRow(ctx, query, skillID, path).Scan(
		&f.ID,
		&f.SkillID,
		&f.Path,
		&f.Content,
		&f.CRC64,
		&f.CreatedAt,
		&f.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &f, nil
}

// Update modifies an existing skill file in the database.
// The application must set file.UpdatedAt before calling this method.
func (r *pgxRepository) Update(ctx context.Context, file *SkillFile) error {
	now := time.Now()
	query := `
		UPDATE skill_files
		SET content = $2, crc64 = $3, updated_at = $4
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query,
		file.ID,
		file.Content,
		file.CRC64,
		now,
	)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	file.UpdatedAt = now
	return nil
}

// Delete removes a skill file by ID from the database.
func (r *pgxRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM skill_files WHERE id = $1`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ListBySkill retrieves all files for a given skill, ordered by path.
func (r *pgxRepository) ListBySkill(ctx context.Context, skillID uuid.UUID) ([]*SkillFile, error) {
	query := `
		SELECT id, skill_id, path, content, crc64, created_at, updated_at
		FROM skill_files
		WHERE skill_id = $1
		ORDER BY path ASC
	`

	rows, err := r.db.Query(ctx, query, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]*SkillFile, 0)
	for rows.Next() {
		var f SkillFile
		err := rows.Scan(
			&f.ID,
			&f.SkillID,
			&f.Path,
			&f.Content,
			&f.CRC64,
			&f.CreatedAt,
			&f.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}

// GetManifest returns path and crc64 for all files of a skill, ordered by path.
// Used by the sync protocol to determine which skill files have changed.
func (r *pgxRepository) GetManifest(ctx context.Context, skillID uuid.UUID) ([]ManifestEntry, error) {
	query := `SELECT path, crc64 FROM skill_files WHERE skill_id = $1 ORDER BY path ASC`

	rows, err := r.db.Query(ctx, query, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]ManifestEntry, 0)
	for rows.Next() {
		var entry ManifestEntry
		if err := rows.Scan(&entry.Path, &entry.CRC64); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
