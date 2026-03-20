package skillfiles_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/handlers/skillfiles"
	skillfilerepo "github.com/twistingmercury/mnemonic/internal/repository/skillfile"
	"github.com/twistingmercury/mnemonic/internal/service"
	skillfilesvc "github.com/twistingmercury/mnemonic/internal/service/skillfile"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Mock SkillFileService ---

type mockSkillFileService struct {
	mock.Mock
}

func (m *mockSkillFileService) Create(ctx context.Context, skillName string, fileType string, input skillfilesvc.CreateInput) (*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillName, fileType, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileService) Get(ctx context.Context, skillName string, fileType string, filename string) (*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillName, fileType, filename)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileService) Update(ctx context.Context, skillName string, fileType string, filename string, input skillfilesvc.UpdateInput) (*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillName, fileType, filename, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillfilerepo.SkillFile), args.Error(1)
}

func (m *mockSkillFileService) Delete(ctx context.Context, skillName string, fileType string, filename string) error {
	args := m.Called(ctx, skillName, fileType, filename)
	return args.Error(0)
}

func (m *mockSkillFileService) ListBySkill(ctx context.Context, skillName string, fileType *string) ([]*skillfilerepo.SkillFile, error) {
	args := m.Called(ctx, skillName, fileType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*skillfilerepo.SkillFile), args.Error(1)
}

// --- Helpers ---

func newTestRouter(svc skillfilesvc.Service) *gin.Engine {
	router := gin.New()
	h := skillfiles.New(svc)
	v1 := router.Group("/v1/api")
	h.RegisterRoutes(v1)
	return router
}

func makeSkillFile(fileType, filename string) *skillfilerepo.SkillFile {
	return &skillfilerepo.SkillFile{
		ID:        uuid.New(),
		SkillID:   uuid.New(),
		Path:      fileType + "/" + filename,
		Content:   "#!/usr/bin/env python3\nprint('hello')\n",
		CRC64:     "111222333",
		CreatedAt: time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC),
	}
}

// --- Tests ---

func TestCreateScript_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	file := makeSkillFile("scripts", "extract.py")
	svc.On("ListBySkill", mock.Anything, "my-skill", mock.AnythingOfType("*string")).Return([]*skillfilerepo.SkillFile{}, nil)
	svc.On("Create", mock.Anything, "my-skill", "scripts", mock.AnythingOfType("skillfile.CreateInput")).Return(file, nil)

	body := `{
		"filename": "extract.py",
		"content_type": "text/x-python",
		"content": "#!/usr/bin/env python3\nprint('hello')\n",
		"encoding": "utf-8"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/my-skill/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/v1/api/skills/my-skill/scripts/extract.py")
}

func TestCreateScript_SkillNotFound(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("ListBySkill", mock.Anything, "unknown-skill", mock.AnythingOfType("*string")).Return([]*skillfilerepo.SkillFile{}, nil)
	svc.On("Create", mock.Anything, "unknown-skill", "scripts", mock.Anything).
		Return(nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, "unknown-skill"))

	body := `{
		"filename": "extract.py",
		"content_type": "text/x-python",
		"content": "content"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/unknown-skill/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateScript_Conflict(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("ListBySkill", mock.Anything, "my-skill", mock.AnythingOfType("*string")).Return([]*skillfilerepo.SkillFile{}, nil)
	svc.On("Create", mock.Anything, "my-skill", "scripts", mock.Anything).
		Return(nil, fmt.Errorf("%w: file %q in skill %q", service.ErrConflict, "scripts/extract.py", "my-skill"))

	body := `{
		"filename": "extract.py",
		"content_type": "text/x-python",
		"content": "content"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/my-skill/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateScript_ListBySkillDBError(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("ListBySkill", mock.Anything, "my-skill", mock.AnythingOfType("*string")).
		Return(nil, fmt.Errorf("database connection lost"))

	body := `{
		"filename": "extract.py",
		"content_type": "text/x-python",
		"content": "content"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/my-skill/scripts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// A DB error on the file count check must return an error response, not 201.
	assert.NotEqual(t, http.StatusCreated, w.Code)
	assert.GreaterOrEqual(t, w.Code, 400)
	svc.AssertNotCalled(t, "Create")
}

func TestListScripts_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	f1 := makeSkillFile("scripts", "extract.py")
	f2 := makeSkillFile("scripts", "build.sh")
	ft := "scripts"
	svc.On("ListBySkill", mock.Anything, "my-skill", &ft).
		Return([]*skillfilerepo.SkillFile{f1, f2}, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills/my-skill/scripts", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]any)
	assert.Len(t, data, 2)
}

func TestGetScript_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	file := makeSkillFile("scripts", "extract.py")
	svc.On("Get", mock.Anything, "my-skill", "scripts", "extract.py").Return(file, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills/my-skill/scripts/extract.py", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "extract.py", resp["filename"])
	assert.NotEmpty(t, resp["content"])
}

func TestGetScript_NotFound(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("Get", mock.Anything, "my-skill", "scripts", "missing.py").
		Return(nil, fmt.Errorf("%w: file %q in skill %q", service.ErrNotFound, "scripts/missing.py", "my-skill"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills/my-skill/scripts/missing.py", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateScript_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	file := makeSkillFile("scripts", "extract.py")
	svc.On("Update", mock.Anything, "my-skill", "scripts", "extract.py", mock.AnythingOfType("skillfile.UpdateInput")).Return(file, nil)

	body := `{
		"content_type": "text/x-python",
		"content": "updated content"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/skills/my-skill/scripts/extract.py", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteScript_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("Delete", mock.Anything, "my-skill", "scripts", "extract.py").Return(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/skills/my-skill/scripts/extract.py", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteScript_NotFound(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	svc.On("Delete", mock.Anything, "my-skill", "scripts", "missing.py").
		Return(fmt.Errorf("%w: file %q in skill %q", service.ErrNotFound, "scripts/missing.py", "my-skill"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/skills/my-skill/scripts/missing.py", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Test reference and asset endpoints use the same handler factory,
// so a single test per type verifies the routing works.

func TestCreateReference_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	file := makeSkillFile("references", "REFERENCE.md")
	svc.On("ListBySkill", mock.Anything, "my-skill", mock.AnythingOfType("*string")).Return([]*skillfilerepo.SkillFile{}, nil)
	svc.On("Create", mock.Anything, "my-skill", "references", mock.AnythingOfType("skillfile.CreateInput")).Return(file, nil)

	body := `{
		"filename": "REFERENCE.md",
		"content_type": "text/markdown",
		"content": "# Reference"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/my-skill/references", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateAsset_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillFileService)
	router := newTestRouter(svc)

	file := makeSkillFile("assets", "template.json")
	svc.On("ListBySkill", mock.Anything, "my-skill", mock.AnythingOfType("*string")).Return([]*skillfilerepo.SkillFile{}, nil)
	svc.On("Create", mock.Anything, "my-skill", "assets", mock.AnythingOfType("skillfile.CreateInput")).Return(file, nil)

	body := `{
		"filename": "template.json",
		"content_type": "application/json",
		"content": "{\"key\": \"value\"}"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills/my-skill/assets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
}
