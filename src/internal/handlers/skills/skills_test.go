package skills_test

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
	"github.com/twistingmercury/mnemonic/internal/handlers/skills"
	skillrepo "github.com/twistingmercury/mnemonic/internal/repository/skill"
	"github.com/twistingmercury/mnemonic/internal/service"
	skillsvc "github.com/twistingmercury/mnemonic/internal/service/skill"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Mock SkillService ---

type mockSkillService struct {
	mock.Mock
}

func (m *mockSkillService) Create(ctx context.Context, input skillsvc.CreateInput) (*skillrepo.Skill, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillService) GetByName(ctx context.Context, name string) (*skillrepo.Skill, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillService) GetByID(ctx context.Context, id uuid.UUID) (*skillrepo.Skill, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillService) Update(ctx context.Context, name string, input skillsvc.UpdateInput) (*skillrepo.Skill, error) {
	args := m.Called(ctx, name, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*skillrepo.Skill), args.Error(1)
}

func (m *mockSkillService) Delete(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockSkillService) List(ctx context.Context, opts skillsvc.ListOptions) ([]*skillrepo.Skill, int64, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Get(1).(int64), args.Error(2)
	}
	return args.Get(0).([]*skillrepo.Skill), args.Get(1).(int64), args.Error(2)
}

func (m *mockSkillService) GetManifest(ctx context.Context) ([]skillrepo.ManifestEntry, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]skillrepo.ManifestEntry), args.Error(1)
}

// --- Helpers ---

func newTestRouter(svc skillsvc.Service) *gin.Engine {
	router := gin.New()
	h := skills.New(svc)
	v1 := router.Group("/v1/api")
	h.RegisterRoutes(v1)
	return router
}

func makeSkill(name string) *skillrepo.Skill {
	def, _ := json.Marshal(map[string]any{
		"description": "Test skill",
		"content":     "# Skill Content",
		"tags":        []string{"sync"},
		"version":     "1.0.0",
	})
	return &skillrepo.Skill{
		ID:         uuid.New(),
		Name:       name,
		Definition: def,
		CRC64:      "987654321",
		CreatedAt:  time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
	}
}

// --- Tests ---

func TestSkillCreate_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	skill := makeSkill("code-review")
	svc.On("Create", mock.Anything, mock.AnythingOfType("skill.CreateInput")).Return(skill, nil)

	body := `{
		"name": "code-review",
		"description": "Test skill",
		"content": "# Skill Content",
		"version": "1.0.0"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/v1/api/skills/code-review")
}

func TestSkillCreate_Conflict(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	svc.On("Create", mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("%w: skill %q", service.ErrConflict, "code-review"))

	body := `{
		"name": "code-review",
		"description": "desc",
		"content": "content",
		"version": "1.0.0"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSkillCreate_BadRequest(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	body := `{"name": "test"}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSkillGet_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	skill := makeSkill("code-review")
	svc.On("GetByName", mock.Anything, "code-review").Return(skill, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills/code-review", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "code-review", resp["name"])
}

func TestSkillGet_NotFound(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	svc.On("GetByName", mock.Anything, "unknown").
		Return(nil, fmt.Errorf("%w: skill %q", service.ErrNotFound, "unknown"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills/unknown", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillList_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	s1 := makeSkill("skill-a")
	s2 := makeSkill("skill-b")
	svc.On("List", mock.Anything, mock.AnythingOfType("skill.ListOptions")).
		Return([]*skillrepo.Skill{s1, s2}, int64(2), nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/api/skills?limit=100", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]any)
	assert.Len(t, data, 2)
}

func TestSkillUpdate_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	skill := makeSkill("code-review")
	svc.On("Update", mock.Anything, "code-review", mock.AnythingOfType("skill.UpdateInput")).Return(skill, nil)

	body := `{
		"description": "Updated skill",
		"content": "Updated content",
		"version": "2.0.0"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/skills/code-review", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestSkillUpdate_NameMismatch(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	body := `{
		"name": "different-name",
		"description": "Updated",
		"content": "Updated",
		"version": "2.0.0"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/api/skills/code-review", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSkillDelete_Success(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	svc.On("Delete", mock.Anything, "code-review").Return(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/skills/code-review", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestSkillDelete_NotFound(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	svc.On("Delete", mock.Anything, "unknown").
		Return(fmt.Errorf("%w: skill %q", service.ErrNotFound, "unknown"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/api/skills/unknown", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSkillCreate_MissingDescription(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	body := `{
		"name": "code-review",
		"content": "# Skill Content",
		"version": "1.0.0"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	fieldErrs := resp["errors"].([]any)
	found := false
	for _, fe := range fieldErrs {
		m := fe.(map[string]any)
		if m["field"] == "description" && m["code"] == "REQUIRED" {
			found = true
		}
	}
	assert.True(t, found, "expected field error {field:description, code:REQUIRED}")
}

func TestSkillCreate_MissingVersion(t *testing.T) {
	t.Parallel()
	svc := new(mockSkillService)
	router := newTestRouter(svc)

	body := `{
		"name": "code-review",
		"description": "Test skill",
		"content": "# Skill Content"
	}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/api/skills", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	fieldErrs := resp["errors"].([]any)
	found := false
	for _, fe := range fieldErrs {
		m := fe.(map[string]any)
		if m["field"] == "version" && m["code"] == "REQUIRED" {
			found = true
		}
	}
	assert.True(t, found, "expected field error {field:version, code:REQUIRED}")
}
