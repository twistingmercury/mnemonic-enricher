// Package skills provides HTTP handlers for skill CRUD operations.
// It registers Gin routes for creating, reading, updating, and deleting
// skill definitions via the REST API.
//
// Documentation:
//   - API: docs/api/openapi/mnemonic-v1.yaml (Skill Endpoints)
//   - Design: docs/design/service-layer.md (SkillService)
package skills

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/mnemonic/internal/handlers"
	skillrepo "github.com/twistingmercury/mnemonic/internal/repository/skill"
	skillsvc "github.com/twistingmercury/mnemonic/internal/service/skill"
)

var skillNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Handler provides HTTP handlers for skill CRUD operations.
type Handler struct {
	svc skillsvc.Service
}

// New creates a new skill Handler backed by the given service.
func New(svc skillsvc.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes binds skill endpoints to the given router group.
// The group should be mounted at /v1/api.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/skills", h.Create)
	rg.GET("/skills", h.List)
	rg.GET("/skills/:name", h.Get)
	rg.PUT("/skills/:name", h.Update)
	rg.DELETE("/skills/:name", h.Delete)
}

// --- Request/Response Types ---

// skillCreateRequest maps the OpenAPI SkillCreate schema.
// @Description Request body for creating a skill
type skillCreateRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags"`
	License       *string           `json:"license"`
	Compatibility *string           `json:"compatibility"`
	Metadata      map[string]string `json:"metadata"`
	AllowedTools  []string          `json:"allowed_tools"`
	Version       string            `json:"version"`
}

// skillUpdateRequest maps the OpenAPI SkillUpdate schema.
// @Description Request body for updating a skill
type skillUpdateRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags"`
	License       *string           `json:"license"`
	Compatibility *string           `json:"compatibility"`
	Metadata      map[string]string `json:"metadata"`
	AllowedTools  []string          `json:"allowed_tools"`
	Version       string            `json:"version"`
}

// fileCounts holds the file counts for a skill.
// @Description File counts for a skill
type fileCounts struct {
	ScriptsCount    int `json:"scripts_count"`
	ReferencesCount int `json:"references_count"`
	AssetsCount     int `json:"assets_count"`
}

// skillResponse maps a skill repository object to the OpenAPI Skill schema.
// @Description Skill resource representation returned by create, get, and update operations
type skillResponse struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags"`
	License       *string           `json:"license"`
	Compatibility *string           `json:"compatibility"`
	Metadata      map[string]string `json:"metadata"`
	AllowedTools  []string          `json:"allowed_tools"`
	Version       string            `json:"version"`
	Files         fileCounts        `json:"files"`
	CRC64         string            `json:"crc64"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// skillListResponse maps the OpenAPI SkillList schema.
// @Description Paginated list of skills
type skillListResponse struct {
	Data       []skillResponse     `json:"data"`
	Pagination handlers.Pagination `json:"pagination"`
}

// skillDefinition mirrors the JSONB definition structure for unmarshaling.
type skillDefinition struct {
	Description   string            `json:"description"`
	Content       string            `json:"content"`
	Tags          []string          `json:"tags,omitempty"`
	License       *string           `json:"license,omitempty"`
	Compatibility *string           `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  []string          `json:"allowed_tools,omitempty"`
	Version       string            `json:"version"`
}

func toSkillResponse(s *skillrepo.Skill) skillResponse {
	var def skillDefinition
	_ = json.Unmarshal(s.Definition, &def)

	tags := def.Tags
	if tags == nil {
		tags = []string{}
	}
	allowedTools := def.AllowedTools
	if allowedTools == nil {
		allowedTools = []string{}
	}

	return skillResponse{
		ID:            s.ID.String(),
		Name:          s.Name,
		Description:   def.Description,
		Content:       def.Content,
		Tags:          tags,
		License:       def.License,
		Compatibility: def.Compatibility,
		Metadata:      def.Metadata,
		AllowedTools:  allowedTools,
		Version:       def.Version,
		Files: fileCounts{
			ScriptsCount:    0,
			ReferencesCount: 0,
			AssetsCount:     0,
		},
		CRC64:     s.CRC64,
		CreatedAt: s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// --- Handlers ---

// Create handles POST /v1/api/skills.
//
// @Summary      Create a skill
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Param        body  body      skillCreateRequest  true  "Skill to create"
// @Success      201   {object}  skillResponse
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      409   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /skills [post]
func (h *Handler) Create(c *gin.Context) {
	var req skillCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	var fieldErrs []handlers.FieldError

	// Required field checks.
	if req.Name == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "name", Code: "REQUIRED", Message: "name is required"})
	}
	if req.Content == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "content", Code: "REQUIRED", Message: "content is required"})
	}
	if req.Description == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "description", Code: "REQUIRED", Message: "description is required"})
	}
	if req.Version == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "version", Code: "REQUIRED", Message: "version is required"})
	}

	// Name format validation.
	if req.Name != "" && (len(req.Name) > 64 || !skillNameRe.MatchString(req.Name)) {
		fieldErrs = append(fieldErrs, handlers.FieldError{
			Field:   "name",
			Code:    "INVALID_FORMAT",
			Message: "name must start with a lowercase letter, contain only lowercase letters, digits, and hyphens, with no leading, trailing, or consecutive hyphens, and be at most 64 characters",
		})
	}

	// Content size validation (max 512 KB).
	if len(req.Content) > 524288 {
		fieldErrs = append(fieldErrs, handlers.FieldError{
			Field:   "content",
			Code:    "MAX_SIZE",
			Message: "content must be 512 KB or fewer",
		})
	}

	// Description length validation (max 1024 characters).
	if utf8.RuneCountInString(req.Description) > 1024 {
		fieldErrs = append(fieldErrs, handlers.FieldError{
			Field:   "description",
			Code:    "MAX_LENGTH",
			Message: "description must be 1024 characters or fewer",
		})
	}

	// Tags, license, compatibility, allowed_tools, version limits.
	if len(req.Tags) > 20 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "tags", Code: "MAX_ITEMS", Message: "tags must contain 20 or fewer items"})
	}
	if req.License != nil && utf8.RuneCountInString(*req.License) > 255 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "license", Code: "MAX_LENGTH", Message: "license must be 255 characters or fewer"})
	}
	if req.Compatibility != nil && utf8.RuneCountInString(*req.Compatibility) > 500 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "compatibility", Code: "MAX_LENGTH", Message: "compatibility must be 500 characters or fewer"})
	}
	if len(req.AllowedTools) > 50 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "allowed_tools", Code: "MAX_ITEMS", Message: "allowed_tools must contain 50 or fewer items"})
	}
	if req.Version != "" && utf8.RuneCountInString(req.Version) > 50 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "version", Code: "MAX_LENGTH", Message: "version must be 50 characters or fewer"})
	}

	if len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.AllowedTools == nil {
		req.AllowedTools = []string{}
	}

	skill, err := h.svc.Create(c.Request.Context(), skillsvc.CreateInput{
		Name:          req.Name,
		Description:   req.Description,
		Content:       req.Content,
		Tags:          req.Tags,
		License:       req.License,
		Compatibility: req.Compatibility,
		Metadata:      req.Metadata,
		AllowedTools:  req.AllowedTools,
		Version:       req.Version,
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	resp := toSkillResponse(skill)
	c.Header("Location", fmt.Sprintf("/v1/api/skills/%s", skill.Name))
	c.JSON(http.StatusCreated, resp)
}

// List handles GET /v1/api/skills.
//
// @Summary      List skills
// @Tags         Skills
// @Produce      json
// @Param        limit   query     int     false  "Max results (1–200, default 100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Param        tags    query     string  false  "Comma-separated tag filter"
// @Success      200     {object}  skillListResponse
// @Failure      400     {object}  handlers.ProblemDetail
// @Failure      500     {object}  handlers.ProblemDetail
// @Router       /skills [get]
func (h *Handler) List(c *gin.Context) {
	limit, ok := handlers.ParseIntQueryStrict(c, "limit", 100, 1, 200)
	if !ok {
		handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
			{Field: "limit", Code: "INVALID_VALUE", Message: "limit must be an integer between 1 and 200"},
		})
		return
	}
	offset, ok := handlers.DecodeCursorStrict(c.Query("cursor"))
	if !ok {
		handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
			{Field: "cursor", Code: "INVALID_VALUE", Message: "cursor is not a valid pagination token"},
		})
		return
	}

	// The tags filter is documented in the OpenAPI spec but the skill service
	// List method does not currently support tag filtering. For MVP we fetch
	// all and let the caller filter. The tags query param is accepted but
	// not applied to the query.
	_ = c.Query("tags")

	skills, _, err := h.svc.List(c.Request.Context(), skillsvc.ListOptions{
		Offset: offset,
		Limit:  limit + 1,
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	hasMore := len(skills) > limit
	if hasMore {
		skills = skills[:limit]
	}

	// Post-filter by tags if provided.
	if rawTags := c.Query("tags"); rawTags != "" {
		requestedTags := strings.Split(rawTags, ",")
		skills = filterSkillsByTags(skills, requestedTags)
	}

	data := make([]skillResponse, len(skills))
	for i, s := range skills {
		data[i] = toSkillResponse(s)
	}

	var cursorPtr *string
	if raw := c.Query("cursor"); raw != "" {
		cursorPtr = &raw
	}

	var nextCursor *string
	if hasMore {
		nc := handlers.EncodeCursor(offset + limit)
		nextCursor = &nc
	}

	c.JSON(http.StatusOK, skillListResponse{
		Data: data,
		Pagination: handlers.Pagination{
			Limit:      limit,
			Cursor:     cursorPtr,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	})
}

// Get handles GET /v1/api/skills/:name.
//
// @Summary      Get a skill by name
// @Tags         Skills
// @Produce      json
// @Param        name  path      string  true  "Skill name"
// @Success      200   {object}  skillResponse
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /skills/{name} [get]
func (h *Handler) Get(c *gin.Context) {
	name := c.Param("name")

	skill, err := h.svc.GetByName(c.Request.Context(), name)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, toSkillResponse(skill))
}

// Update handles PUT /v1/api/skills/:name.
//
// @Summary      Update a skill
// @Tags         Skills
// @Accept       json
// @Param        name  path      string              true  "Skill name"
// @Param        body  body      skillUpdateRequest  true  "Skill fields to update"
// @Success      204   "No Content"
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /skills/{name} [put]
func (h *Handler) Update(c *gin.Context) {
	name := c.Param("name")

	var req skillUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	// If name is provided in body, it must match the path parameter.
	if req.Name != "" && req.Name != name {
		handlers.RespondValidationError(c, "Name in body must match path parameter", []handlers.FieldError{
			{Field: "name", Code: "INVALID_VALUE", Message: "name in body must match path parameter or be omitted"},
		})
		return
	}

	var fieldErrs []handlers.FieldError

	// Required field checks.
	if req.Description == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "description", Code: "REQUIRED", Message: "description is required"})
	}
	if req.Content == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "content", Code: "REQUIRED", Message: "content is required"})
	}
	if req.Version == "" {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "version", Code: "REQUIRED", Message: "version is required"})
	}

	// Content size validation (max 512 KB).
	if len(req.Content) > 524288 {
		fieldErrs = append(fieldErrs, handlers.FieldError{
			Field:   "content",
			Code:    "MAX_SIZE",
			Message: "content must be 512 KB or fewer",
		})
	}

	// Description length validation (max 1024 characters).
	if utf8.RuneCountInString(req.Description) > 1024 {
		fieldErrs = append(fieldErrs, handlers.FieldError{
			Field:   "description",
			Code:    "MAX_LENGTH",
			Message: "description must be 1024 characters or fewer",
		})
	}

	// Tags, license, compatibility, allowed_tools, version limits.
	if len(req.Tags) > 20 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "tags", Code: "MAX_ITEMS", Message: "tags must contain 20 or fewer items"})
	}
	if req.License != nil && utf8.RuneCountInString(*req.License) > 255 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "license", Code: "MAX_LENGTH", Message: "license must be 255 characters or fewer"})
	}
	if req.Compatibility != nil && utf8.RuneCountInString(*req.Compatibility) > 500 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "compatibility", Code: "MAX_LENGTH", Message: "compatibility must be 500 characters or fewer"})
	}
	if len(req.AllowedTools) > 50 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "allowed_tools", Code: "MAX_ITEMS", Message: "allowed_tools must contain 50 or fewer items"})
	}
	if req.Version != "" && utf8.RuneCountInString(req.Version) > 50 {
		fieldErrs = append(fieldErrs, handlers.FieldError{Field: "version", Code: "MAX_LENGTH", Message: "version must be 50 characters or fewer"})
	}

	if len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.AllowedTools == nil {
		req.AllowedTools = []string{}
	}

	if _, err := h.svc.Update(c.Request.Context(), name, skillsvc.UpdateInput{
		Description:   req.Description,
		Content:       req.Content,
		Tags:          req.Tags,
		License:       req.License,
		Compatibility: req.Compatibility,
		Metadata:      req.Metadata,
		AllowedTools:  req.AllowedTools,
		Version:       req.Version,
	}); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Delete handles DELETE /v1/api/skills/:name.
//
// @Summary      Delete a skill
// @Tags         Skills
// @Param        name  path  string  true  "Skill name"
// @Success      204   "No Content"
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /skills/{name} [delete]
func (h *Handler) Delete(c *gin.Context) {
	name := c.Param("name")

	if err := h.svc.Delete(c.Request.Context(), name); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// filterSkillsByTags filters skills in-memory by checking if their definition
// tags contain any of the requested tags.
func filterSkillsByTags(skills []*skillrepo.Skill, requestedTags []string) []*skillrepo.Skill {
	if len(requestedTags) == 0 {
		return skills
	}
	tagSet := make(map[string]bool, len(requestedTags))
	for _, t := range requestedTags {
		tagSet[strings.TrimSpace(t)] = true
	}

	var filtered []*skillrepo.Skill
	for _, s := range skills {
		var def skillDefinition
		if err := json.Unmarshal(s.Definition, &def); err != nil {
			continue
		}
		// Build skill tag set.
		skillTags := make(map[string]bool, len(def.Tags))
		for _, t := range def.Tags {
			skillTags[t] = true
		}
		// AND logic: skill must have ALL requested tags.
		hasAll := true
		for tag := range tagSet {
			if !skillTags[tag] {
				hasAll = false
				break
			}
		}
		if hasAll {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
