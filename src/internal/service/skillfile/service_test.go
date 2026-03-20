package skillfile_test

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
	skillfilerepo "github.com/twistingmercury/mnemonic/internal/repository/skillfile"
	"github.com/twistingmercury/mnemonic/internal/service"
	skillfilesvc "github.com/twistingmercury/mnemonic/internal/service/skillfile"
)

// ---------- Mock Repositories ----------

type mockSkillFileRepo struct {
	mock.Mock
}

func (m *mockSkillFileRepo) Create(ctx context.Context, file *skillfilerepo.SkillFile) error {
	args := m.Called(ctx, file)
	if args.Error(0) == nil {
		file.ID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
		file.CreatedAt = time.Now()
		file.UpdatedAt = time.Now()
	}
	return args.Error(0)
}

func (m *mockSkillFileRepo) Get(ctx context.Context, id uuid.UUID) (*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileRepo) GetByPath(ctx context.Context, skillID uuid.UUID, path string) (*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillID, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileRepo) Update(ctx context.Context, file *skillfilerepo.SkillFile) error {
	args := m.Called(ctx, file)
	if args.Error(0) == nil {
		file.UpdatedAt = time.Now()
	}
	return args.Error(0)
}

func (m *mockSkillFileRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockSkillFileRepo) ListBySkill(ctx context.Context, skillID uuid.UUID) ([]*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileRepo) GetManifest(ctx context.Context, skillID uuid.UUID) ([]skillfilerepo.ManifestEntry, error) {
	args := m.Called(ctx, skillID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]skillfilerepo.ManifestEntry), args.Error(1)
}

type mockSkillRepo struct {
	mock.Mock
}

func (m *mockSkillRepo) Create(ctx context.Context, skill *skillrepo.Skill) error {
	args := m.Called(ctx, skill)
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

var (
	testSkillID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	testFileID  = uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
)

func newTestService(fileRepo *mockSkillFileRepo, skillRepo *mockSkillRepo) skillfilesvc.Service {
	logger := zerolog.Nop()
	return skillfilesvc.New(fileRepo, skillRepo, logger)
}

func resolvedSkill() *skillrepo.Skill {
	return &skillrepo.Skill{
		ID:         testSkillID,
		Name:       "my-skill",
		Definition: json.RawMessage(`{"description":"test","version":"1.0.0"}`),
		CRC64:      "555",
		CreatedAt:  time.Now().Add(-time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
	}
}

func existingFile() *skillfilerepo.SkillFile {
	return &skillfilerepo.SkillFile{
		ID:        testFileID,
		SkillID:   testSkillID,
		Path:      "scripts/build.sh",
		Content:   "#!/bin/bash\necho build",
		CRC64:     "777",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
}

// ---------- Create ----------

func TestService_Create(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("Create", mock.Anything, mock.AnythingOfType("*skillfile.SkillFile")).
			Run(func(args mock.Arguments) {
				f := args.Get(1).(*skillfilerepo.SkillFile)
				// Verify path construction: "scripts" + "/" + "build.sh" = "scripts/build.sh"
				assert.Equal(t, "scripts/build.sh", f.Path)
				assert.Equal(t, testSkillID, f.SkillID)
				assert.Equal(t, "#!/bin/bash\necho build", f.Content)
				assert.NotEmpty(t, f.CRC64)
			}).Return(nil)

		input := skillfilesvc.CreateInput{
			Filename:    "build.sh",
			ContentType: "text/x-shellscript",
			Content:     "#!/bin/bash\necho build",
			Encoding:    "utf-8",
		}

		result, err := svc.Create(context.Background(), "my-skill", "scripts", input)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "scripts/build.sh", result.Path)
		assert.False(t, result.ID == uuid.Nil)

		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("skill not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		input := skillfilesvc.CreateInput{
			Filename: "build.sh",
			Content:  "content",
			Encoding: "utf-8",
		}

		result, err := svc.Create(context.Background(), "nonexistent", "scripts", input)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertNotCalled(t, "Create")
	})

	t.Run("file already exists returns service.ErrConflict", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("Create", mock.Anything, mock.AnythingOfType("*skillfile.SkillFile")).
			Return(skillfilerepo.ErrExists)

		input := skillfilesvc.CreateInput{
			Filename: "build.sh",
			Content:  "content",
			Encoding: "utf-8",
		}

		result, err := svc.Create(context.Background(), "my-skill", "scripts", input)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrConflict)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})
}

// ---------- Get ----------

func TestService_Get(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/build.sh").Return(existingFile(), nil)

		result, err := svc.Get(context.Background(), "my-skill", "scripts", "build.sh")

		require.NoError(t, err)
		assert.Equal(t, testFileID, result.ID)
		assert.Equal(t, "scripts/build.sh", result.Path)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("skill not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		result, err := svc.Get(context.Background(), "nonexistent", "scripts", "build.sh")

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertNotCalled(t, "GetByPath")
	})

	t.Run("file not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/missing.sh").
			Return(nil, skillfilerepo.ErrNotFound)

		result, err := svc.Get(context.Background(), "my-skill", "scripts", "missing.sh")

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})
}

// ---------- Update ----------

func TestService_Update(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/build.sh").Return(existingFile(), nil)
		fileRepo.On("Update", mock.Anything, mock.AnythingOfType("*skillfile.SkillFile")).
			Run(func(args mock.Arguments) {
				f := args.Get(1).(*skillfilerepo.SkillFile)
				assert.Equal(t, "#!/bin/bash\necho updated", f.Content)
				assert.NotEqual(t, "777", f.CRC64) // CRC64 should be recomputed
			}).Return(nil)

		input := skillfilesvc.UpdateInput{
			ContentType: "text/x-shellscript",
			Content:     "#!/bin/bash\necho updated",
			Encoding:    "utf-8",
		}

		result, err := svc.Update(context.Background(), "my-skill", "scripts", "build.sh", input)

		require.NoError(t, err)
		assert.Equal(t, "#!/bin/bash\necho updated", result.Content)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("skill not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		input := skillfilesvc.UpdateInput{Content: "new content"}
		result, err := svc.Update(context.Background(), "nonexistent", "scripts", "build.sh", input)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
	})

	t.Run("file not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/missing.sh").
			Return(nil, skillfilerepo.ErrNotFound)

		input := skillfilesvc.UpdateInput{Content: "new content"}
		result, err := svc.Update(context.Background(), "my-skill", "scripts", "missing.sh", input)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})
}

// ---------- Delete ----------

func TestService_Delete(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/build.sh").Return(existingFile(), nil)
		fileRepo.On("Delete", mock.Anything, testFileID).Return(nil)

		err := svc.Delete(context.Background(), "my-skill", "scripts", "build.sh")

		assert.NoError(t, err)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("skill not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		err := svc.Delete(context.Background(), "nonexistent", "scripts", "build.sh")

		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertNotCalled(t, "GetByPath")
	})

	t.Run("file not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("GetByPath", mock.Anything, testSkillID, "scripts/missing.sh").
			Return(nil, skillfilerepo.ErrNotFound)

		err := svc.Delete(context.Background(), "my-skill", "scripts", "missing.sh")

		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})
}

// ---------- ListBySkill ----------

func TestService_ListBySkill(t *testing.T) {
	t.Parallel()

	t.Run("happy path without filter", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		files := []*skillfilerepo.SkillFile{
			{ID: uuid.New(), SkillID: testSkillID, Path: "config/settings.yaml"},
			{ID: uuid.New(), SkillID: testSkillID, Path: "scripts/build.sh"},
			{ID: uuid.New(), SkillID: testSkillID, Path: "scripts/test.sh"},
		}

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("ListBySkill", mock.Anything, testSkillID).Return(files, nil)

		result, err := svc.ListBySkill(context.Background(), "my-skill", nil)

		require.NoError(t, err)
		assert.Len(t, result, 3)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("with fileType filter", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		files := []*skillfilerepo.SkillFile{
			{ID: uuid.New(), SkillID: testSkillID, Path: "config/settings.yaml"},
			{ID: uuid.New(), SkillID: testSkillID, Path: "scripts/build.sh"},
			{ID: uuid.New(), SkillID: testSkillID, Path: "scripts/test.sh"},
		}

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("ListBySkill", mock.Anything, testSkillID).Return(files, nil)

		fileType := "scripts"
		result, err := svc.ListBySkill(context.Background(), "my-skill", &fileType)

		require.NoError(t, err)
		assert.Len(t, result, 2)
		for _, f := range result {
			assert.True(t, f.Path == "scripts/build.sh" || f.Path == "scripts/test.sh")
		}
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("with fileType filter no matches", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		files := []*skillfilerepo.SkillFile{
			{ID: uuid.New(), SkillID: testSkillID, Path: "scripts/build.sh"},
		}

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("ListBySkill", mock.Anything, testSkillID).Return(files, nil)

		fileType := "config"
		result, err := svc.ListBySkill(context.Background(), "my-skill", &fileType)

		require.NoError(t, err)
		assert.Empty(t, result)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})

	t.Run("skill not found returns service.ErrNotFound", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "nonexistent").Return(nil, skillrepo.ErrNotFound)

		result, err := svc.ListBySkill(context.Background(), "nonexistent", nil)

		assert.Nil(t, result)
		assert.ErrorIs(t, err, service.ErrNotFound)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertNotCalled(t, "ListBySkill")
	})

	t.Run("repository error propagated", func(t *testing.T) {
		t.Parallel()

		fileRepo := new(mockSkillFileRepo)
		skillRepo := new(mockSkillRepo)
		svc := newTestService(fileRepo, skillRepo)

		skillRepo.On("GetByName", mock.Anything, "my-skill").Return(resolvedSkill(), nil)
		fileRepo.On("ListBySkill", mock.Anything, testSkillID).Return(nil, errors.New("timeout"))

		result, err := svc.ListBySkill(context.Background(), "my-skill", nil)

		assert.Nil(t, result)
		assert.Error(t, err)
		skillRepo.AssertExpectations(t)
		fileRepo.AssertExpectations(t)
	})
}
