// Package agents provides HTTP handlers for agent CRUD operations.
// It registers Gin routes for creating, reading, updating, and deleting
// agent definitions via the REST API.
//
// Documentation:
//   - API: docs/api/openapi/mnemonic-v1.yaml (Agent Endpoints)
//   - Design: docs/design/service-layer.md (AgentService)
package agents

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/mnemonic/internal/handlers"
	agentrepo "github.com/twistingmercury/mnemonic/internal/repository/agent"
	agentsvc "github.com/twistingmercury/mnemonic/internal/service/agent"
)

// agentNameRe is the compiled pattern for valid agent names.
var agentNameRe = regexp.MustCompile(`^[a-z]([a-z0-9](-[a-z0-9])*)*$`)

// Handler provides HTTP handlers for agent CRUD operations.
type Handler struct {
	svc agentsvc.Service
}

// New creates a new agent Handler backed by the given service.
func New(svc agentsvc.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes binds agent endpoints to the given router group.
// The group should be mounted at /v1/api.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/agents", h.Create)
	rg.GET("/agents", h.List)
	rg.GET("/agents/:name", h.Get)
	rg.PUT("/agents/:name", h.Update)
	rg.DELETE("/agents/:name", h.Delete)
}

// agentCreateRequest maps the OpenAPI AgentCreate schema.
// @Description Request body for creating a new agent
type agentCreateRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Version      string   `json:"version"`
}

// agentUpdateRequest maps the OpenAPI AgentUpdate schema.
// @Description Request body for updating an existing agent
type agentUpdateRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Version      string   `json:"version"`
}

// agentResponse maps an agent repository object to the OpenAPI Agent schema.
// @Description Agent resource returned by create, get, and update operations
type agentResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Model        string   `json:"model"`
	AllowedTools []string `json:"allowed_tools"`
	Version      string   `json:"version"`
	CRC64        string   `json:"crc64"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// agentListResponse maps the OpenAPI AgentList schema.
// @Description Paginated list of agents
type agentListResponse struct {
	Data       []agentResponse     `json:"data"`
	Pagination handlers.Pagination `json:"pagination"`
}

// toAgentResponse converts a repository Agent to the wire-format response.
// It returns an error if the stored JSONB definition cannot be unmarshaled.
func toAgentResponse(a *agentrepo.Agent) (agentResponse, error) {
	// Unmarshal the JSONB definition to extract flat fields.
	var def struct {
		Description  string   `json:"description"`
		SystemPrompt string   `json:"system_prompt"`
		Model        string   `json:"model"`
		AllowedTools []string `json:"allowed_tools"`
		Version      string   `json:"version"`
	}
	if err := json.Unmarshal(a.Definition, &def); err != nil {
		return agentResponse{}, fmt.Errorf("corrupt agent definition for %s: %w", a.ID, err)
	}

	if def.AllowedTools == nil {
		def.AllowedTools = []string{}
	}

	return agentResponse{
		ID:           a.ID.String(),
		Name:         a.Name,
		Description:  def.Description,
		SystemPrompt: def.SystemPrompt,
		Model:        def.Model,
		AllowedTools: def.AllowedTools,
		Version:      def.Version,
		CRC64:        a.CRC64,
		CreatedAt:    a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:    a.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}, nil
}

// validateAgentFields checks field-level constraints for create/update and
// returns a non-nil slice of FieldErrors when any constraint is violated.
func validateAgentFields(name, systemPrompt, model, description, version string) []handlers.FieldError {
	var errs []handlers.FieldError

	// name: required, regex, max 64
	if name == "" {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "REQUIRED", Message: "name is required"})
	} else if utf8.RuneCountInString(name) > 64 {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "MAX_LENGTH", Message: "name must be 64 characters or fewer"})
	} else if !agentNameRe.MatchString(name) {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "INVALID_FORMAT", Message: "name must match ^[a-z]([a-z0-9](-[a-z0-9])*)*$"})
	}

	// system_prompt: required, max 51200
	if systemPrompt == "" {
		errs = append(errs, handlers.FieldError{Field: "system_prompt", Code: "REQUIRED", Message: "system_prompt is required"})
	} else if utf8.RuneCountInString(systemPrompt) > 51200 {
		errs = append(errs, handlers.FieldError{Field: "system_prompt", Code: "MAX_LENGTH", Message: "system_prompt must be 51200 characters or fewer"})
	}

	// model: required
	if model == "" {
		errs = append(errs, handlers.FieldError{Field: "model", Code: "REQUIRED", Message: "model is required"})
	}

	// description: required, max 500
	if description == "" {
		errs = append(errs, handlers.FieldError{Field: "description", Code: "REQUIRED", Message: "description is required"})
	} else if utf8.RuneCountInString(description) > 500 {
		errs = append(errs, handlers.FieldError{Field: "description", Code: "MAX_LENGTH", Message: "description must be 500 characters or fewer"})
	}

	// version: required, max 50
	if version == "" {
		errs = append(errs, handlers.FieldError{Field: "version", Code: "REQUIRED", Message: "version is required"})
	} else if utf8.RuneCountInString(version) > 50 {
		errs = append(errs, handlers.FieldError{Field: "version", Code: "MAX_LENGTH", Message: "version must be 50 characters or fewer"})
	}

	return errs
}

// Create handles POST /v1/api/agents.
//
// @Summary      Create agent
// @Tags         Agents
// @Accept       json
// @Produce      json
// @Param        body  body      agentCreateRequest  true  "Agent to create"
// @Success      201   {object}  agentResponse
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      409   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /agents [post]
func (h *Handler) Create(c *gin.Context) {
	var req agentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	if fieldErrs := validateAgentFields(req.Name, req.SystemPrompt, req.Model, req.Description, req.Version); len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if req.AllowedTools == nil {
		req.AllowedTools = []string{}
	}

	agent, err := h.svc.Create(c.Request.Context(), agentsvc.CreateInput{
		Name:         req.Name,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		AllowedTools: req.AllowedTools,
		Version:      req.Version,
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	resp, err := toAgentResponse(agent)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}
	c.Header("Location", fmt.Sprintf("/v1/api/agents/%s", agent.Name))
	c.JSON(http.StatusCreated, resp)
}

// List handles GET /v1/api/agents.
//
// @Summary      List agents
// @Tags         Agents
// @Produce      json
// @Param        limit   query     int     false  "Max results (1–200, default 100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Success      200     {object}  agentListResponse
// @Failure      400     {object}  handlers.ProblemDetail
// @Failure      500     {object}  handlers.ProblemDetail
// @Router       /agents [get]
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

	agents, _, err := h.svc.List(c.Request.Context(), agentsvc.ListOptions{
		Offset: offset,
		Limit:  limit + 1, // Fetch one extra to detect has_more.
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	hasMore := len(agents) > limit
	if hasMore {
		agents = agents[:limit]
	}

	data := make([]agentResponse, len(agents))
	for i, a := range agents {
		resp, err := toAgentResponse(a)
		if err != nil {
			handlers.RespondError(c, err)
			return
		}
		data[i] = resp
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

	c.JSON(http.StatusOK, agentListResponse{
		Data: data,
		Pagination: handlers.Pagination{
			Limit:      limit,
			Cursor:     cursorPtr,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	})
}

// Get handles GET /v1/api/agents/:name.
//
// @Summary      Get agent
// @Tags         Agents
// @Produce      json
// @Param        name  path      string  true  "Agent name"
// @Success      200   {object}  agentResponse
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /agents/{name} [get]
func (h *Handler) Get(c *gin.Context) {
	name := c.Param("name")

	agent, err := h.svc.Get(c.Request.Context(), name)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	resp, err := toAgentResponse(agent)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Update handles PUT /v1/api/agents/:name.
//
// @Summary      Update agent
// @Tags         Agents
// @Accept       json
// @Param        name  path      string              true  "Agent name"
// @Param        body  body      agentUpdateRequest  true  "Agent fields to update"
// @Success      204   "No Content"
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      409   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /agents/{name} [put]
func (h *Handler) Update(c *gin.Context) {
	name := c.Param("name")

	var req agentUpdateRequest
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

	// Validate body fields (use path name for name validation).
	effectiveName := req.Name
	if effectiveName == "" {
		effectiveName = name
	}
	if fieldErrs := validateAgentFields(effectiveName, req.SystemPrompt, req.Model, req.Description, req.Version); len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if req.AllowedTools == nil {
		req.AllowedTools = []string{}
	}

	if _, err := h.svc.Update(c.Request.Context(), name, agentsvc.UpdateInput{
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		AllowedTools: req.AllowedTools,
		Version:      req.Version,
	}); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Delete handles DELETE /v1/api/agents/:name.
//
// @Summary      Delete agent
// @Tags         Agents
// @Produce      json
// @Param        name  path  string  true  "Agent name"
// @Success      204   "No Content"
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /agents/{name} [delete]
func (h *Handler) Delete(c *gin.Context) {
	name := c.Param("name")

	if err := h.svc.Delete(c.Request.Context(), name); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
