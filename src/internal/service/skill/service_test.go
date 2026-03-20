package skill_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/repository"
	skillrepo "github.com/twistingmercury/mnemonic/internal/repository/skill"
	"github.com/twistingmercury/mnemonic/internal/service"
	skillsvc "github.com/twistingmercury/mnemonic/internal/service/skill"
)

// ---------- Mock Repository ----------

type mockSkillRepo struct {
	mock.Mock
}

func (m *mockSkillRepo) Create(ctx context.Context, skill *skillrepo.Skill) error {
	args := m.Called(ctx, skill)
	if args.Error(0) == nil {
		// Simulate database populating ID and timestamps.
		skill.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
		skill.CreatedAt = time.Now()
		skill.UpdatedAt = time.Now()
	}
	return args.Error(0)
}

func (m *mockSkillRepo) Get(ctx context.Context, id uuid.UUID) (*skillrepo.Skill, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillRepo) GetByName(ctx context.Context, name string) (*skillrepo.Skill, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillRepo) Update(ctx context.Context, skill *skillrepo.Skill) error {
	args := m.Called(ctx, skill)
	if args.Error(0) == nil {
		skill.UpdatedAt = time.Now()
	}
	return args.Error(0)
}

func (m *mockSkillRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockSkillRepo) DeleteByName(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockSkillRepo) List(ctx context.Context, opts repository.ListOptions) ([]*skillrepo.Skill, int64, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*skillrepo.Skill), args.Get(1).(int64), args.Error(2)
}

func (m *mockSkillRepo) Exists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func (m *mockSkillRepo) GetManifest(ctx context.Context) ([]skillrepo.ManifestEntry, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]skillrepo.ManifestEntry), args.Error(1)
}

// ---------- Helpers ----------

func newTestService(repo *mockSkillRepo) skillsvc.Service {
	logger := zerolog.Nop()
	return skillsvc.New(repo, logger)
}

func sampleCreateInput() skillsvc.CreateInput {
	license := "MIT"
	compat := "claude-code >=1.0"
	return skillsvc.CreateInput{
		Name:          "test-skill",
		Description:   "A test skill",
		Content:       "You are testing.",
		Tags:          []string{"test", "demo"},
		License:       &license,
		Compatibility: &compat,
		Metadata:      map[string]string{"author": "tester"},
		AllowedTools:  []string{"Bash", "Read"},
		Version:       "1.0.0",
	}
}

func sampleUpdateInput() skillsvc.UpdateInput {
	license := "Apache-2.0"
	return skillsvc.UpdateInput{
		Description:  "Updated skill",
		Content:      "You are testing v2.",
		Tags:         []string{"test"},
		License:      &license,
		AllowedTools: []string{"Bash"},
		Version:      "2.0.0",
	}
}

func existingSkill() *skillrepo.Skill {
	return &skillrepo.Skill{
		ID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:       "test-skill",
		Definition: json.RawMessage(`{"description":"old","content":"old content","version":"1.0.0"}`),
		CRC64:      "999",
		CreatedAt:  time.Now().Add(-time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
	}
}

// ---------- Create ----------

func TestService_Create(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("Create", mock.Anything, mock.AnythingOfType("*skill.Skill")).Return(nil)

		result, err := svc.Create(context.Background(), sampleCreateInput())

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-skill", result.Name)
		assert.NotEmpty(t, result.CRC64)
		assert.False(t, result.ID == uuid.Nil)

		// Verify definition JSON contains expected fields.
		var def map[string]any
		require.NoError(t, json.Unmarshal(result.Definition, &def))
		assert.Equal(t, "A test skill", def["description"])
		assert.Equal(t, "You are testing.", def["content"])
		assert.Equal(t, "1.0.0", def["version"])
		assert.Equal(t, "MIT", def["license"])

		repo.AssertExpectations(t)
	})

	t.Run("conflict returns service.ErrConflict", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("Create", mock.Anything, mock.AnythingOfType("*skill.Skill")).Return(skillrepo.ErrExists)

		result, err := svc.Create(context.Background(), sampleCreateInput())

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrConflict)
		repo.AssertExpectations(t)
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("Create", mock.Anything, mock.AnythingOfType("*skill.Skill")).Return(errors.New("connection refused"))

		result, err := svc.Create(context.Background(), sampleCreateInput())

		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, service.ErrConflict)
		repo.AssertExpectations(t)
	})

	t.Run("CRC64 is deterministic for same input", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		var crc1, crc2 string
		repo.On("Create", mock.Anything, mock.AnythingOfType("*skill.Skill")).
			Run(func(args mock.Arguments) {
				s := args.Get(1).(*skillrepo.Skill)
				if crc1 == "" {
					crc1 = s.CRC64
				} else {
					crc2 = s.CRC64
				}
			}).Return(nil)

		input := sampleCreateInput()
		_, _ = svc.Create(context.Background(), input)
		_, _ = svc.Create(context.Background(), input)

		assert.NotEmpty(t, crc1)
		assert.Equal(t, crc1, crc2)
		repo.AssertExpectations(t)
	})
}

// ---------- GetByName ----------

func TestService_GetByName(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		expected := existingSkill()
		repo.On("GetByName", mock.Anything, "test-skill").Return(expected, nil)

		result, err := svc.GetByName(context.Background(), "test-skill")

		require.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.Name, result.Name)
		repo.AssertExpectations(t)
	})

	t.Run("not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		result, err := svc.GetByName(context.Background(), "nonexistent")

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("GetByName", mock.Anything, "any").Return(nil, errors.New("db error"))

		result, err := svc.GetByName(context.Background(), "any")

		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})
}

// ---------- GetByID ----------

func TestService_GetByID(t *testing.T) {
	t.Parallel()

	testID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		expected := existingSkill()
		expected.ID = testID
		repo.On("Get", mock.Anything, testID).Return(expected, nil)

		result, err := svc.GetByID(context.Background(), testID)

		require.NoError(t, err)
		assert.Equal(t, testID, result.ID)
		repo.AssertExpectations(t)
	})

	t.Run("not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("Get", mock.Anything, testID).Return(nil, skillrepo.ErrNotFound)

		result, err := svc.GetByID(context.Background(), testID)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("Get", mock.Anything, testID).Return(nil, errors.New("timeout"))

		result, err := svc.GetByID(context.Background(), testID)

		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})
}

// ---------- Update ----------

func TestService_Update(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		existing := existingSkill()
		repo.On("GetByName", mock.Anything, "test-skill").Return(existing, nil)
		repo.On("Update", mock.Anything, mock.AnythingOfType("*skill.Skill")).Return(nil)

		result, err := svc.Update(context.Background(), "test-skill", sampleUpdateInput())

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, existing.ID, result.ID)

		// Verify definition was updated.
		var def map[string]any
		require.NoError(t, json.Unmarshal(result.Definition, &def))
		assert.Equal(t, "Updated skill", def["description"])
		assert.Equal(t, "2.0.0", def["version"])

		// Verify CRC64 was recomputed (should differ from original).
		assert.NotEqual(t, "999", result.CRC64)
		assert.NotEmpty(t, result.CRC64)

		repo.AssertExpectations(t)
	})

	t.Run("not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		result, err := svc.Update(context.Background(), "nonexistent", sampleUpdateInput())

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})

	t.Run("update repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		existing := existingSkill()
		repo.On("GetByName", mock.Anything, "test-skill").Return(existing, nil)
		repo.On("Update", mock.Anything, mock.AnythingOfType("*skill.Skill")).Return(errors.New("disk full"))

		result, err := svc.Update(context.Background(), "test-skill", sampleUpdateInput())

		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disk full")
		repo.AssertExpectations(t)
	})
}

// ---------- Delete ----------

func TestService_Delete(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		existing := existingSkill()
		repo.On("GetByName", mock.Anything, "test-skill").Return(existing, nil)
		repo.On("Delete", mock.Anything, existing.ID).Return(nil)

		err := svc.Delete(context.Background(), "test-skill")

		assert.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		err := svc.Delete(context.Background(), "nonexistent")

		assert.ErrorIs(t, err, service.ErrNotFound)
		repo.AssertExpectations(t)
	})

	t.Run("delete repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		existing := existingSkill()
		repo.On("GetByName", mock.Anything, "test-skill").Return(existing, nil)
		repo.On("Delete", mock.Anything, existing.ID).Return(errors.New("foreign key"))

		err := svc.Delete(context.Background(), "test-skill")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "foreign key")
		repo.AssertExpectations(t)
	})
}

// ---------- List ----------

func TestService_List(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		skills := []*skillrepo.Skill{existingSkill()}
		repo.On("List", mock.Anything, repository.ListOptions{Offset: 0, Limit: 10}).
			Return(skills, int64(1), nil)

		results, total, err := svc.List(context.Background(), skillsvc.ListOptions{Offset: 0, Limit: 10})

		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, int64(1), total)
		repo.AssertExpectations(t)
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("List", mock.Anything, repository.ListOptions{}).
			Return([]*skillrepo.Skill{}, int64(0), nil)

		results, total, err := svc.List(context.Background(), skillsvc.ListOptions{})

		require.NoError(t, err)
		assert.Empty(t, results)
		assert.Equal(t, int64(0), total)
		repo.AssertExpectations(t)
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("List", mock.Anything, repository.ListOptions{}).
			Return(nil, int64(0), errors.New("timeout"))

		results, total, err := svc.List(context.Background(), skillsvc.ListOptions{})

		assert.Nil(t, results)
		assert.Equal(t, int64(0), total)
		assert.Error(t, err)
		repo.AssertExpectations(t)
	})
}

// ---------- GetManifest ----------

func TestService_GetManifest(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		entries := []skillrepo.ManifestEntry{
			{Name: "skill-a", CRC64: "111"},
			{Name: "skill-b", CRC64: "222"},
		}
		repo.On("GetManifest", mock.Anything).Return(entries, nil)

		result, err := svc.GetManifest(context.Background())

		require.NoError(t, err)
		assert.Equal(t, entries, result)
		repo.AssertExpectations(t)
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		repo := new(mockSkillRepo)
		svc := newTestService(repo)

		repo.On("GetManifest", mock.Anything).Return(nil, errors.New("query failed"))

		result, err := svc.GetManifest(context.Background())

		assert.Nil(t, result)
		assert.Error(t, err)
		repo.AssertExpectations(t)
	})
}
