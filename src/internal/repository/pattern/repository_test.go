package pattern_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic-enricher/internal/repository"
	"github.com/twistingmercury/mnemonic-enricher/internal/repository/pattern"
)

// testPattern returns a sample pattern for testing.
func testPattern() *pattern.Pattern {
	desc := "A pattern for testing"
	return &pattern.Pattern{
		ID:               uuid.New(),
		Name:             "test-pattern",
		Description:      &desc,
		Content:          "This is test content for the pattern.",
		Tags:             []string{"test", "example"},
		EntityType:       "go-pattern",
		Language:         "go",
		Domain:           "backend",
		EnrichmentStatus: "pending",
	}
}

func TestRepository_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pattern   *pattern.Pattern
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:    "successful creation",
			pattern: testPattern(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("INSERT INTO patterns").
					WithArgs(
						pgxmock.AnyArg(), // id
						"test-pattern",
						pgxmock.AnyArg(), // description
						"This is test content for the pattern.",
						pgxmock.AnyArg(), // tags JSON
						"go-pattern",     // entity_type
						"go",             // language
						"backend",        // domain
						pgxmock.AnyArg(), // version (nil)
						pgxmock.AnyArg(), // related_patterns JSON
						"pending",
						pgxmock.AnyArg(), // created_at
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: nil,
		},
		{
			name:    "duplicate name returns ErrNameExists",
			pattern: testPattern(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("INSERT INTO patterns").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnError(&pgconn.PgError{Code: "23505"})
			},
			wantErr: pattern.ErrNameExists,
		},
		{
			name: "nil description is valid",
			pattern: &pattern.Pattern{
				Name:    "no-desc-pattern",
				Content: "Content without description",
				Tags:    []string{},
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("INSERT INTO patterns").
					WithArgs(
						pgxmock.AnyArg(),
						"no-desc-pattern",
						pgxmock.AnyArg(), // description (nil *string)
						"Content without description",
						pgxmock.AnyArg(), // tags JSON
						pgxmock.AnyArg(), // entity_type
						pgxmock.AnyArg(), // language
						pgxmock.AnyArg(), // domain
						pgxmock.AnyArg(), // version
						pgxmock.AnyArg(), // related_patterns JSON
						"pending",
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			err = repo.Create(context.Background(), tt.pattern)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.pattern.CreatedAt.IsZero())
				assert.False(t, tt.pattern.UpdatedAt.IsZero())
				assert.Equal(t, "pending", tt.pattern.EnrichmentStatus)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	now := time.Now()
	patternID := uuid.New()
	tagsJSON, _ := json.Marshal([]string{"tag1", "tag2"})
	relatedPatternsJSON, _ := json.Marshal([]string{})
	desc := "Test description"

	tests := []struct {
		name        string
		patternID   uuid.UUID
		setupMock   func(mock pgxmock.PgxPoolIface)
		wantPattern *pattern.Pattern
		wantErr     error
	}{
		{
			name:      "successful retrieval",
			patternID: patternID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at",
				}).AddRow(
					patternID, "test-pattern", &desc, "Test content", tagsJSON,
					"go-pattern", "go", "backend", nil, relatedPatternsJSON,
					"enriched", nil, &now, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(patternID).
					WillReturnRows(rows)
			},
			wantPattern: &pattern.Pattern{
				ID:               patternID,
				Name:             "test-pattern",
				Description:      &desc,
				Content:          "Test content",
				Tags:             []string{"tag1", "tag2"},
				EntityType:       "go-pattern",
				Language:         "go",
				Domain:           "backend",
				EnrichmentStatus: "enriched",
				EnrichedAt:       &now,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			wantErr: nil,
		},
		{
			name:      "successful retrieval with pending status",
			patternID: patternID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at",
				}).AddRow(
					patternID, "test-pattern", &desc, "Test content", tagsJSON,
					"go-pattern", "go", "backend", nil, relatedPatternsJSON,
					"pending", nil, nil, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(patternID).
					WillReturnRows(rows)
			},
			wantPattern: &pattern.Pattern{
				ID:               patternID,
				Name:             "test-pattern",
				Description:      &desc,
				Content:          "Test content",
				Tags:             []string{"tag1", "tag2"},
				EntityType:       "go-pattern",
				Language:         "go",
				Domain:           "backend",
				EnrichmentStatus: "pending",
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantPattern: nil,
			wantErr:     pattern.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			p, err := repo.Get(context.Background(), tt.patternID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantPattern.ID, p.ID)
				assert.Equal(t, tt.wantPattern.Name, p.Name)
				assert.Equal(t, tt.wantPattern.EnrichmentStatus, p.EnrichmentStatus)
				assert.Equal(t, tt.wantPattern.Tags, p.Tags)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_GetByName(t *testing.T) {
	t.Parallel()

	now := time.Now()
	patternID := uuid.New()
	tagsJSON, _ := json.Marshal([]string{"tag1"})
	relatedPatternsJSON, _ := json.Marshal([]string{})
	desc := "Test description"

	tests := []struct {
		name        string
		patternName string
		setupMock   func(mock pgxmock.PgxPoolIface)
		wantErr     error
	}{
		{
			name:        "successful retrieval",
			patternName: "test-pattern",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at",
				}).AddRow(
					patternID, "test-pattern", &desc, "Content", tagsJSON,
					"go-pattern", "go", "backend", nil, relatedPatternsJSON,
					"pending", nil, nil, now, now,
				)
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs("test-pattern").
					WillReturnRows(rows)
			},
			wantErr: nil,
		},
		{
			name:        "not found returns ErrNotFound",
			patternName: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs("nonexistent").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: pattern.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			p, err := repo.GetByName(context.Background(), tt.patternName)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.patternName, p.Name)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pattern   *pattern.Pattern
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:    "successful update",
			pattern: testPattern(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(), // id
						"test-pattern",
						pgxmock.AnyArg(), // description
						pgxmock.AnyArg(), // content
						pgxmock.AnyArg(), // tags JSON
						"go-pattern",     // entity_type
						"go",             // language
						"backend",        // domain
						pgxmock.AnyArg(), // version (nil)
						pgxmock.AnyArg(), // related_patterns JSON
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:    "not found returns ErrNotFound",
			pattern: testPattern(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: pattern.ErrNotFound,
		},
		{
			name:    "duplicate name returns ErrNameExists",
			pattern: testPattern(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnError(&pgconn.PgError{Code: "23505"})
			},
			wantErr: pattern.ErrNameExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			err = repo.Update(context.Background(), tt.pattern)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.False(t, tt.pattern.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Update_ResetsEnrichmentStatus(t *testing.T) {
	t.Parallel()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Create a pattern that was previously enriched
	enrichedAt := time.Now().Add(-time.Hour)
	errMsg := "previous error"
	p := &pattern.Pattern{
		ID:               uuid.New(),
		Name:             "enriched-pattern",
		Description:      ptrString("Description"),
		Content:          "Original content",
		Tags:             []string{"tag1"},
		EntityType:       "go-pattern",
		Language:         "go",
		Domain:           "backend",
		EnrichmentStatus: "enriched",
		EnrichmentError:  &errMsg,
		EnrichedAt:       &enrichedAt,
	}

	// The UPDATE query should reset enrichment fields
	mock.ExpectExec("UPDATE patterns SET").
		WithArgs(
			p.ID,
			p.Name,
			p.Description,
			p.Content,
			pgxmock.AnyArg(), // tags JSON
			"go-pattern",     // entity_type
			"go",             // language
			"backend",        // domain
			pgxmock.AnyArg(), // version (nil)
			pgxmock.AnyArg(), // related_patterns JSON
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := pattern.NewRepository(mock)
	err = repo.Update(context.Background(), p)

	require.NoError(t, err)

	// Verify the pattern struct was updated to reflect reset enrichment fields
	assert.Equal(t, "pending", p.EnrichmentStatus, "enrichment_status should be reset to pending")
	assert.Nil(t, p.EnrichmentError, "enrichment_error should be cleared")
	assert.Nil(t, p.EnrichedAt, "enriched_at should be cleared")
	assert.False(t, p.UpdatedAt.IsZero(), "updated_at should be set")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ptrString is a helper to create a pointer to a string.
func ptrString(s string) *string {
	return &s
}

func TestRepository_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		patternID uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "successful deletion",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM patterns").
					WithArgs(pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "not found returns ErrNotFound",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM patterns").
					WithArgs(pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: pattern.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			err = repo.Delete(context.Background(), tt.patternID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_List(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tagsJSON, _ := json.Marshal([]string{"tag1"})
	relatedPatternsJSON, _ := json.Marshal([]string{})
	desc := "Description"

	tests := []struct {
		name      string
		filter    pattern.Filter
		opts      repository.ListOptions
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantTotal int64
		wantErr   error
		wantNames []string
	}{
		{
			name:   "list all patterns without filter",
			filter: pattern.Filter{},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(uuid.New(), "pattern-a", &desc, "Content A", tagsJSON,
						"go-pattern", "go", "backend", nil, relatedPatternsJSON,
						"pending", nil, nil, now, now, int64(2)).
					AddRow(uuid.New(), "pattern-b", &desc, "Content B", tagsJSON,
						"go-pattern", "go", "backend", nil, relatedPatternsJSON,
						"enriched", nil, &now, now, now, int64(2))
				mock.ExpectQuery("SELECT .* FROM patterns ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantTotal: 2,
			wantNames: []string{"pattern-a", "pattern-b"},
		},
		{
			name:   "list with enrichment status filter",
			filter: pattern.Filter{EnrichmentStatus: "enriched"},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(uuid.New(), "enriched-pattern", &desc, "Content", tagsJSON,
						"go-pattern", "go", "backend", nil, relatedPatternsJSON,
						"enriched", nil, &now, now, now, int64(1))
				mock.ExpectQuery("SELECT .* FROM patterns.*WHERE enrichment_status").
					WithArgs("enriched").
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
			wantNames: []string{"enriched-pattern"},
		},
		{
			name:   "list with pagination",
			filter: pattern.Filter{},
			opts:   repository.ListOptions{Limit: 1, Offset: 1},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(uuid.New(), "pattern-b", &desc, "Content", tagsJSON,
						"go-pattern", "go", "backend", nil, relatedPatternsJSON,
						"pending", nil, nil, now, now, int64(3))
				mock.ExpectQuery("SELECT .* FROM patterns.*LIMIT.*OFFSET").
					WithArgs(1, 1).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 3,
			wantNames: []string{"pattern-b"},
		},
		{
			name:   "empty list returns empty slice",
			filter: pattern.Filter{},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at", "total_count",
				})
				mock.ExpectQuery("SELECT .* FROM patterns ORDER BY name").
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantTotal: 0,
			wantNames: []string{},
		},
		{
			name:   "list with search query filter",
			filter: pattern.Filter{SearchQuery: "authentication"},
			opts:   repository.ListOptions{},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "name", "description", "content", "tags",
					"entity_type", "language", "domain", "version", "related_patterns",
					"enrichment_status", "enrichment_error", "enriched_at",
					"created_at", "updated_at", "total_count",
				}).
					AddRow(uuid.New(), "auth-pattern", &desc, "Content about auth", tagsJSON,
						"go-pattern", "go", "backend", nil, relatedPatternsJSON,
						"pending", nil, nil, now, now, int64(1))
				// Query should include to_tsvector and plainto_tsquery for full-text search
				mock.ExpectQuery("SELECT .* FROM patterns.*to_tsvector.*plainto_tsquery").
					WithArgs("authentication").
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
			wantNames: []string{"auth-pattern"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			patterns, total, err := repo.List(context.Background(), tt.filter, tt.opts)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTotal, total)
				assert.Len(t, patterns, tt.wantCount)

				for i, expectedName := range tt.wantNames {
					assert.Equal(t, expectedName, patterns[i].Name)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_UpdateEnrichmentStatus(t *testing.T) {
	t.Parallel()

	errMsg := "embedding generation failed"

	tests := []struct {
		name      string
		patternID uuid.UUID
		status    string
		errMsg    *string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "update to enriched status",
			patternID: uuid.New(),
			status:    "enriched",
			errMsg:    nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(), // id
						"enriched",
						pgxmock.AnyArg(), // error message (nil *string)
						pgxmock.AnyArg(), // enriched_at (should be set)
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "update to failed status with error",
			patternID: uuid.New(),
			status:    "failed",
			errMsg:    &errMsg,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(), // id
						"failed",
						pgxmock.AnyArg(), // error message
						pgxmock.AnyArg(), // enriched_at (should be nil for failed)
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "pattern not found",
			patternID: uuid.New(),
			status:    "enriched",
			errMsg:    nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: pattern.ErrNotFound,
		},
		{
			name:      "invalid status returns error",
			patternID: uuid.New(),
			status:    "invalid_status",
			errMsg:    nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// No database call expected - validation fails first
			},
			wantErr: nil, // We'll check for error message content instead
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			err = repo.UpdateEnrichmentStatus(context.Background(), tt.patternID, tt.status, tt.errMsg)

			if tt.name == "invalid status returns error" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid enrichment status")
			} else if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_Exists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		patternID  uuid.UUID
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantExists bool
		wantErr    error
	}{
		{
			name:      "pattern exists",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
			},
			wantExists: true,
		},
		{
			name:      "pattern does not exist",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
			},
			wantExists: false,
		},
		{
			name:      "database error",
			patternID: uuid.New(),
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT EXISTS").
					WithArgs(pgxmock.AnyArg()).
					WillReturnError(errors.New("connection failed"))
			},
			wantExists: false,
			wantErr:    errors.New("connection failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			exists, err := repo.Exists(context.Background(), tt.patternID)

			if tt.wantErr != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantExists, exists)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPattern_IsValidEnrichmentStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   bool
	}{
		{"pending", true},
		{"enriched", true},
		{"failed", true},
		{"invalid", false},
		{"PENDING", false}, // case-sensitive
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, repository.IsValidEnrichmentStatus(tt.status))
		})
	}
}

func TestRepository_ContextCancellation(t *testing.T) {
	t.Parallel()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	// Setup mock to return context.Canceled error
	mock.ExpectQuery("SELECT .* FROM patterns").
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(context.Canceled)

	repo := pattern.NewRepository(mock)
	p, err := repo.Get(ctx, uuid.New())

	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, p)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_Create_GeneratesUUID(t *testing.T) {
	t.Parallel()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Pattern with zero UUID should get one generated
	p := &pattern.Pattern{
		Name:    "test-pattern",
		Content: "Test content",
		Tags:    []string{},
	}

	mock.ExpectExec("INSERT INTO patterns").
		WithArgs(
			pgxmock.AnyArg(), // generated UUID
			"test-pattern",
			pgxmock.AnyArg(), // nil description (*string)
			"Test content",
			pgxmock.AnyArg(), // tags JSON
			pgxmock.AnyArg(), // entity_type
			pgxmock.AnyArg(), // language
			pgxmock.AnyArg(), // domain
			pgxmock.AnyArg(), // version
			pgxmock.AnyArg(), // related_patterns JSON
			"pending",
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	repo := pattern.NewRepository(mock)
	err = repo.Create(context.Background(), p)

	assert.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, p.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_Create_NilTagsHandled(t *testing.T) {
	t.Parallel()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Pattern with nil Tags should be converted to empty slice
	p := &pattern.Pattern{
		Name:    "pattern-with-nil-tags",
		Content: "Content",
		Tags:    nil, // Explicitly nil
	}

	mock.ExpectExec("INSERT INTO patterns").
		WithArgs(
			pgxmock.AnyArg(), // id
			"pattern-with-nil-tags",
			pgxmock.AnyArg(), // description
			"Content",
			[]byte("[]"),     // tags should be marshaled as empty array, not "null"
			pgxmock.AnyArg(), // entity_type
			pgxmock.AnyArg(), // language
			pgxmock.AnyArg(), // domain
			pgxmock.AnyArg(), // version
			pgxmock.AnyArg(), // related_patterns JSON
			"pending",
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	repo := pattern.NewRepository(mock)
	err = repo.Create(context.Background(), p)

	require.NoError(t, err)
	// The pattern's Tags should be set to empty slice
	assert.NotNil(t, p.Tags, "Tags should not be nil after Create")
	assert.Empty(t, p.Tags, "Tags should be empty slice")
	assert.NoError(t, mock.ExpectationsWereMet())
}
