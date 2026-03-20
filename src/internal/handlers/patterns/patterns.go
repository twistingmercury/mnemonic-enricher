// Package patterns provides HTTP handlers for pattern CRUD, association,
// and search operations. It registers Gin routes for creating, reading,
// updating, deleting, and searching context patterns via the REST API.
//
// Documentation:
//   - API: docs/api/openapi/mnemonic-v1.yaml (Pattern Endpoints)
//   - Design: docs/design/service-layer.md (PatternService, SearchService)
package patterns

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/twistingmercury/mnemonic/internal/config"
	"github.com/twistingmercury/mnemonic/internal/handlers"
	patternrepo "github.com/twistingmercury/mnemonic/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/mnemonic/internal/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/service/search"
)

// kebabCaseRe matches lowercase kebab-case identifiers (e.g., "go-pattern", "go-error-handling").
var kebabCaseRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Handler provides HTTP handlers for pattern CRUD and search operations.
type Handler struct {
	patternSvc       patternsvc.Service
	searchSvc        searchsvc.Service
	allowedLanguages []string
	allowedDomains   []string
}

// New creates a new pattern Handler backed by the given services and vocabulary config.
func New(patternSvc patternsvc.Service, searchSvc searchsvc.Service, vocab config.VocabularyConfig) *Handler {
	langs := make([]string, len(vocab.Languages))
	copy(langs, vocab.Languages)
	doms := make([]string, len(vocab.Domains))
	copy(doms, vocab.Domains)
	return &Handler{
		patternSvc:       patternSvc,
		searchSvc:        searchSvc,
		allowedLanguages: langs,
		allowedDomains:   doms,
	}
}


// RegisterRoutes binds pattern endpoints to the given router group.
// The group should be mounted at /v1/api.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/patterns", h.Create)
	rg.GET("/patterns", h.List)
	rg.GET("/patterns/search", h.Search)
	rg.GET("/patterns/:id", h.Get)
	rg.PUT("/patterns/:id", h.Update)
	rg.DELETE("/patterns/:id", h.Delete)
	rg.GET("/patterns/:id/agents", h.GetAgentAssociations)
	rg.PUT("/patterns/:id/agents", h.SetAgentAssociations)
	rg.GET("/patterns/:id/chunks", h.GetChunks)
}

// --- Request/Response Types ---

// associationRequest maps a single agent association in create/update requests.
// @Description Agent association entry used in pattern create and update requests
type associationRequest struct {
	AgentName string  `json:"agent_name" binding:"required"`
	Relevance float64 `json:"relevance"`
}

// patternCreateRequest maps the OpenAPI PatternCreate schema.
// @Description Request body for creating a new pattern
type patternCreateRequest struct {
	Name              string               `json:"name"`
	Description       *string              `json:"description"`
	Content           string               `json:"content"`
	Tags              []string             `json:"tags"`
	AgentAssociations []associationRequest `json:"agent_associations"`
	EntityType        string               `json:"entity_type"`
	Language          string               `json:"language"`
	Domain            string               `json:"domain"`
	Version           *string              `json:"version"`
	RelatedPatterns   []string             `json:"related_patterns"`
}

// patternUpdateRequest maps the OpenAPI PatternUpdate schema.
// @Description Request body for updating an existing pattern
type patternUpdateRequest struct {
	Name              string               `json:"name"`
	Description       *string              `json:"description"`
	Content           string               `json:"content"`
	Tags              []string             `json:"tags"`
	AgentAssociations []associationRequest `json:"agent_associations"`
	EntityType        string               `json:"entity_type"`
	Language          string               `json:"language"`
	Domain            string               `json:"domain"`
	Version           *string              `json:"version"`
	RelatedPatterns   []string             `json:"related_patterns"`
}

// associationResponse represents a single agent association in a pattern response.
// @Description Agent association returned in pattern responses
type associationResponse struct {
	AgentName string  `json:"agent_name"`
	Relevance float64 `json:"relevance"`
}

// relatedPatternResponse represents a related pattern entry in graph context.
// @Description Related pattern entry returned in graph context
type relatedPatternResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Relationship string  `json:"relationship"`
	Strength     float64 `json:"strength"`
}

// conceptResponse represents a concept node in graph context.
// @Description Concept node returned in graph context
type conceptResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// graphContextResponse contains the graph context for a pattern.
// @Description Graph context containing related patterns and concepts
type graphContextResponse struct {
	RelatedPatterns []relatedPatternResponse `json:"related_patterns"`
	Concepts        []conceptResponse        `json:"concepts"`
}

// chunkSummary represents a single content chunk summary for a pattern.
// @Description Summary of a single content chunk belonging to a pattern
type chunkSummary struct {
	ChunkIndex       int    `json:"chunk_index"`
	SectionTitle     string `json:"section_title"`
	EnrichmentStatus string `json:"enrichment_status"`
}

// chunkListResponse is the response body for listing pattern chunks.
// @Description List of chunk summaries for a pattern
type chunkListResponse struct {
	Chunks []chunkSummary `json:"chunks"`
	Count  int            `json:"count"`
}

// patternResponse maps a pattern repository object to the OpenAPI Pattern schema.
// @Description Full pattern resource returned by create, get, and update operations
type patternResponse struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Description      *string               `json:"description"`
	Content          string                `json:"content"`
	Tags             []string              `json:"tags"`
	EntityType       string                `json:"entity_type"`
	Language         string                `json:"language"`
	Domain           string                `json:"domain"`
	Version          *string               `json:"version"`
	RelatedPatterns  []string              `json:"related_patterns"`
	AgentAssociation []associationResponse `json:"agent_associations,omitempty"`
	EnrichmentStatus string                `json:"enrichment_status"`
	EnrichmentError  *string               `json:"enrichment_error"`
	EnrichedAt       *string               `json:"enriched_at"`
	Graph            *graphContextResponse `json:"graph"`
	Chunks           []chunkSummary        `json:"chunks,omitempty"`
	CreatedAt        string                `json:"created_at"`
	UpdatedAt        string                `json:"updated_at"`
}

// patternSummaryResponse maps the OpenAPI PatternSummary schema.
// @Description Abbreviated pattern resource returned in list responses
type patternSummaryResponse struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	Tags             []string `json:"tags"`
	EnrichmentStatus string   `json:"enrichment_status"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// patternListResponse maps the OpenAPI PatternList schema.
// @Description Paginated list of pattern summaries
type patternListResponse struct {
	Data       []patternSummaryResponse `json:"data"`
	Pagination handlers.Pagination      `json:"pagination"`
}

// associationsRequest is the request body for setting agent associations on a pattern.
// @Description Request body for setting agent associations on a pattern
type associationsRequest struct {
	Associations []associationRequest `json:"associations" binding:"required"`
}

// associationsResponse is the response body for agent association operations.
// @Description Response body containing the current agent associations for a pattern
type associationsResponse struct {
	Associations []associationResponse `json:"associations"`
}

// searchResultResponse represents a single semantic search result.
// @Description Single semantic search result matching a query
type searchResultResponse struct {
	PatternID    string   `json:"pattern_id"`
	PatternName  string   `json:"pattern_name"`
	EntityType   string   `json:"entity_type"`
	Language     string   `json:"language"`
	Domain       string   `json:"domain"`
	Tags         []string `json:"tags"`
	SectionTitle string   `json:"section_title"`
	ChunkIndex   int      `json:"chunk_index"`
	Content      string   `json:"content"`
	Similarity   float64  `json:"similarity"`
}

// searchMetadata contains metadata about a semantic search operation.
// @Description Metadata returned alongside semantic search results
type searchMetadata struct {
	Query            string `json:"query"`
	TotalCandidates  int    `json:"total_candidates"`
	SearchDurationMs int64  `json:"search_duration_ms"`
}

// searchResponse is the response body for semantic pattern search.
// @Description Response body for a semantic pattern search operation
type searchResponse struct {
	Results  []searchResultResponse `json:"results"`
	Metadata searchMetadata         `json:"metadata"`
}

// --- Converters ---

func toPatternResponse(p *patternrepo.Pattern, graph *patternsvc.GraphContext, assocs []associationResponse) patternResponse {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}

	relatedPatterns := p.RelatedPatterns
	if relatedPatterns == nil {
		relatedPatterns = []string{}
	}

	resp := patternResponse{
		ID:               p.ID.String(),
		Name:             p.Name,
		Description:      p.Description,
		Content:          p.Content,
		Tags:             tags,
		EntityType:       p.EntityType,
		Language:         p.Language,
		Domain:           p.Domain,
		Version:          p.Version,
		RelatedPatterns:  relatedPatterns,
		AgentAssociation: assocs,
		EnrichmentStatus: p.EnrichmentStatus,
		EnrichmentError:  p.EnrichmentError,
		CreatedAt:        p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if p.EnrichedAt != nil {
		s := p.EnrichedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.EnrichedAt = &s
	}

	if graph != nil {
		gc := graphContextResponse{
			RelatedPatterns: make([]relatedPatternResponse, len(graph.RelatedPatterns)),
			Concepts:        make([]conceptResponse, len(graph.Concepts)),
		}
		for i, r := range graph.RelatedPatterns {
			gc.RelatedPatterns[i] = relatedPatternResponse{
				ID:           r.ID.String(),
				Name:         r.Name,
				Relationship: r.Relationship,
				Strength:     r.Similarity,
			}
		}
		for i, c := range graph.Concepts {
			gc.Concepts[i] = conceptResponse{
				Name: c.Name,
			}
		}
		resp.Graph = &gc
	}

	return resp
}

func toPatternSummary(p *patternrepo.Pattern) patternSummaryResponse {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	return patternSummaryResponse{
		ID:               p.ID.String(),
		Name:             p.Name,
		Description:      p.Description,
		Tags:             tags,
		EnrichmentStatus: p.EnrichmentStatus,
		CreatedAt:        p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toAssociationResponses(assocs []patternrepo.AgentAssociation, names map[uuid.UUID]string) []associationResponse {
	result := make([]associationResponse, len(assocs))
	for i, a := range assocs {
		result[i] = associationResponse{
			AgentName: names[a.AgentID],
			Relevance: a.Relevance,
		}
	}
	return result
}

func toAssociationInputs(reqs []associationRequest) []patternsvc.AssociationInput {
	inputs := make([]patternsvc.AssociationInput, len(reqs))
	for i, r := range reqs {
		relevance := r.Relevance
		if relevance == 0 {
			relevance = 1.0
		}
		inputs[i] = patternsvc.AssociationInput{
			AgentName: r.AgentName,
			Relevance: relevance,
		}
	}
	return inputs
}

// validatePatternFields checks field-level constraints for create/update and
// returns a non-nil slice of FieldErrors when any constraint is violated.
// When allowedLanguages or allowedDomains are non-empty, the supplied values
// must appear in the respective list.
func (h *Handler) validatePatternFields(name, content string, description *string, tags []string, entityType, language, domain string) []handlers.FieldError {
	var errs []handlers.FieldError

	// name: must match ^[a-z][a-z0-9-]*$, length 1-128
	nameLen := utf8.RuneCountInString(name)
	if nameLen == 0 {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "REQUIRED", Message: "name is required"})
	} else if nameLen > 128 {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "MAX_LENGTH", Message: "name must be 128 characters or fewer"})
	} else if !kebabCaseRe.MatchString(name) {
		errs = append(errs, handlers.FieldError{Field: "name", Code: "INVALID_FORMAT", Message: "name must match ^[a-z][a-z0-9-]*$"})
	}

	// content: must not be empty; upper bound of 100KB guards against DoS
	// (real pattern files top out around 18KB per design doc)
	if len(content) == 0 {
		errs = append(errs, handlers.FieldError{Field: "content", Code: "REQUIRED", Message: "content is required"})
	} else if len(content) > 100_000 {
		errs = append(errs, handlers.FieldError{Field: "content", Code: "TOO_LARGE", Message: "content must be 100000 bytes or fewer"})
	}

	// description: optional, max 500 chars
	if description != nil && utf8.RuneCountInString(*description) > 500 {
		errs = append(errs, handlers.FieldError{Field: "description", Code: "MAX_LENGTH", Message: "description must be 500 characters or fewer"})
	}

	// tags: max 20 items
	if len(tags) > 20 {
		errs = append(errs, handlers.FieldError{Field: "tags", Code: "MAX_ITEMS", Message: "tags must contain 20 items or fewer"})
	}

	// entity_type: required, max 100 chars, must match ^[a-z][a-z0-9-]*$
	entityTypeLen := utf8.RuneCountInString(entityType)
	if entityTypeLen == 0 {
		errs = append(errs, handlers.FieldError{Field: "entity_type", Code: "REQUIRED", Message: "entity_type is required"})
	} else if entityTypeLen > 100 {
		errs = append(errs, handlers.FieldError{Field: "entity_type", Code: "MAX_LENGTH", Message: "entity_type must be 100 characters or fewer"})
	} else if !kebabCaseRe.MatchString(entityType) {
		errs = append(errs, handlers.FieldError{Field: "entity_type", Code: "INVALID_FORMAT", Message: "entity_type must match ^[a-z][a-z0-9-]*$"})
	}

	// language: required, must be kebab-case, max 64 chars, and must be one of the allowed values.
	if language == "" {
		errs = append(errs, handlers.FieldError{Field: "language", Code: "REQUIRED", Message: "language is required"})
	} else if utf8.RuneCountInString(language) > 64 {
		errs = append(errs, handlers.FieldError{Field: "language", Code: "MAX_LENGTH", Message: "language must be 64 characters or fewer"})
	} else if !kebabCaseRe.MatchString(language) {
		errs = append(errs, handlers.FieldError{Field: "language", Code: "INVALID_FORMAT", Message: "language must match ^[a-z][a-z0-9-]*$"})
	} else if !slices.Contains(h.allowedLanguages, language) {
		errs = append(errs, handlers.FieldError{Field: "language", Code: "INVALID_VALUE", Message: "language is not an allowed value"})
	}

	// domain: required, must be kebab-case, max 64 chars, and must be one of the allowed values.
	if domain == "" {
		errs = append(errs, handlers.FieldError{Field: "domain", Code: "REQUIRED", Message: "domain is required"})
	} else if utf8.RuneCountInString(domain) > 64 {
		errs = append(errs, handlers.FieldError{Field: "domain", Code: "MAX_LENGTH", Message: "domain must be 64 characters or fewer"})
	} else if !kebabCaseRe.MatchString(domain) {
		errs = append(errs, handlers.FieldError{Field: "domain", Code: "INVALID_FORMAT", Message: "domain must match ^[a-z][a-z0-9-]*$"})
	} else if !slices.Contains(h.allowedDomains, domain) {
		errs = append(errs, handlers.FieldError{Field: "domain", Code: "INVALID_VALUE", Message: "domain is not an allowed value"})
	}

	return errs
}

// validateAssociationRelevance checks that each association has a relevance value
// in the range [0.0, 1.0] and returns FieldErrors for any that are out of range.
// fieldPrefix is the JSON field name used in error paths (e.g., "agent_associations" or "associations").
func validateAssociationRelevance(assocs []associationRequest, fieldPrefix string) []handlers.FieldError {
	var errs []handlers.FieldError
	for i, assoc := range assocs {
		if assoc.Relevance < 0.0 || assoc.Relevance > 1.0 {
			errs = append(errs, handlers.FieldError{
				Field:   fmt.Sprintf("%s[%d].relevance", fieldPrefix, i),
				Code:    "INVALID_VALUE",
				Message: "relevance must be a number between 0 and 1",
			})
		}
	}
	return errs
}

// --- Handlers ---

// Create handles POST /v1/api/patterns.
//
// @Summary      Create pattern
// @Tags         Patterns
// @Accept       json
// @Produce      json
// @Param        body  body      patternCreateRequest  true  "Pattern to create"
// @Success      202   {object}  patternResponse
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      409   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /patterns [post]
func (h *Handler) Create(c *gin.Context) {
	var req patternCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	if fieldErrs := h.validatePatternFields(req.Name, req.Content, req.Description, req.Tags, req.EntityType, req.Language, req.Domain); len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if assocErrs := validateAssociationRelevance(req.AgentAssociations, "agent_associations"); len(assocErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", assocErrs)
		return
	}

	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.AgentAssociations == nil {
		req.AgentAssociations = []associationRequest{}
	}
	if req.RelatedPatterns == nil {
		req.RelatedPatterns = []string{}
	}

	pattern, err := h.patternSvc.Create(c.Request.Context(), patternsvc.CreateInput{
		Name:              req.Name,
		Description:       req.Description,
		Content:           req.Content,
		Tags:              req.Tags,
		AgentAssociations: toAssociationInputs(req.AgentAssociations),
		EntityType:        req.EntityType,
		Language:          req.Language,
		Domain:            req.Domain,
		Version:           req.Version,
		RelatedPatterns:   req.RelatedPatterns,
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	// Fetch agent associations to include in the response.
	ctx := c.Request.Context()
	pgAssocs, err := h.patternSvc.GetAgentAssociations(ctx, pattern.ID)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}
	agentIDs := make([]uuid.UUID, len(pgAssocs))
	for i, a := range pgAssocs {
		agentIDs[i] = a.AgentID
	}
	names, err := h.patternSvc.ResolveAgentNames(ctx, agentIDs)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	resp := toPatternResponse(pattern, nil, toAssociationResponses(pgAssocs, names))
	c.Header("Location", fmt.Sprintf("/v1/api/patterns/%s", pattern.ID))
	c.JSON(http.StatusAccepted, resp)
}

// List handles GET /v1/api/patterns.
//
// @Summary      List patterns
// @Tags         Patterns
// @Produce      json
// @Param        limit        query     int     false  "Max results (1–100, default 20)"
// @Param        cursor       query     string  false  "Pagination cursor"
// @Param        tags         query     string  false  "Comma-separated tag filter"
// @Param        search       query     string  false  "Keyword search filter"
// @Param        language     query     string  false  "Language filter"
// @Param        domain       query     string  false  "Domain filter"
// @Param        entity_type  query     string  false  "Entity type filter"
// @Success      200          {object}  patternListResponse
// @Failure      400          {object}  handlers.ProblemDetail
// @Failure      500          {object}  handlers.ProblemDetail
// @Router       /patterns [get]
func (h *Handler) List(c *gin.Context) {
	limit, ok := handlers.ParseIntQueryStrict(c, "limit", 20, 1, 100)
	if !ok {
		handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
			{Field: "limit", Code: "INVALID_VALUE", Message: "limit must be an integer between 1 and 100"},
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

	var tags []string
	if raw := c.Query("tags"); raw != "" {
		tags = strings.Split(raw, ",")
	}

	filter := patternrepo.Filter{
		Tags:        tags,
		SearchQuery: c.Query("search"),
		Language:    c.Query("language"),
		Domain:      c.Query("domain"),
		EntityType:  c.Query("entity_type"),
	}

	patterns, _, err := h.patternSvc.List(c.Request.Context(), filter, patternsvc.ListOptions{
		Offset: offset,
		Limit:  limit + 1,
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	hasMore := len(patterns) > limit
	if hasMore {
		patterns = patterns[:limit]
	}

	data := make([]patternSummaryResponse, len(patterns))
	for i, p := range patterns {
		data[i] = toPatternSummary(p)
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

	c.JSON(http.StatusOK, patternListResponse{
		Data: data,
		Pagination: handlers.Pagination{
			Limit:      limit,
			Cursor:     cursorPtr,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	})
}

// Get handles GET /v1/api/patterns/:id.
//
// @Summary      Get pattern
// @Tags         Patterns
// @Produce      json
// @Param        id   path      string  true  "Pattern UUID"
// @Success      200  {object}  patternResponse
// @Failure      400  {object}  handlers.ProblemDetail
// @Failure      404  {object}  handlers.ProblemDetail
// @Failure      500  {object}  handlers.ProblemDetail
// @Router       /patterns/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	ctx := c.Request.Context()

	pattern, graph, err := h.patternSvc.GetWithGraph(ctx, id)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	// Fetch agent associations separately (handler composition per design doc).
	pgAssocs, err := h.patternSvc.GetAgentAssociations(ctx, id)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	agentIDs := make([]uuid.UUID, len(pgAssocs))
	for i, a := range pgAssocs {
		agentIDs[i] = a.AgentID
	}
	names, err := h.patternSvc.ResolveAgentNames(ctx, agentIDs)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	resp := toPatternResponse(pattern, graph, toAssociationResponses(pgAssocs, names))

	// Populate chunks; degrade gracefully if the chunk service fails.
	chunks, chunkErr := h.patternSvc.ListChunks(ctx, id)
	if chunkErr != nil {
		// Non-fatal: log and return the pattern without chunks.
		c.Header("X-Warning", "chunks unavailable")
	} else {
		summaries := make([]chunkSummary, len(chunks))
		for i, ch := range chunks {
			summaries[i] = chunkSummary{
				ChunkIndex:       ch.ChunkIndex,
				SectionTitle:     ch.SectionTitle,
				EnrichmentStatus: ch.EnrichmentStatus,
			}
		}
		resp.Chunks = summaries
	}

	c.JSON(http.StatusOK, resp)
}

// Update handles PUT /v1/api/patterns/:id.
//
// @Summary      Update pattern
// @Tags         Patterns
// @Accept       json
// @Param        id    path      string                true  "Pattern UUID"
// @Param        body  body      patternUpdateRequest  true  "Pattern fields to update"
// @Success      204   "No Content"
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      409   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /patterns/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	var req patternUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	if fieldErrs := h.validatePatternFields(req.Name, req.Content, req.Description, req.Tags, req.EntityType, req.Language, req.Domain); len(fieldErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", fieldErrs)
		return
	}

	if assocErrs := validateAssociationRelevance(req.AgentAssociations, "agent_associations"); len(assocErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", assocErrs)
		return
	}

	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.AgentAssociations == nil {
		req.AgentAssociations = []associationRequest{}
	}
	if req.RelatedPatterns == nil {
		req.RelatedPatterns = []string{}
	}

	if _, err := h.patternSvc.Update(c.Request.Context(), id, patternsvc.UpdateInput{
		Name:              req.Name,
		Description:       req.Description,
		Content:           req.Content,
		Tags:              req.Tags,
		AgentAssociations: toAssociationInputs(req.AgentAssociations),
		EntityType:        req.EntityType,
		Language:          req.Language,
		Domain:            req.Domain,
		Version:           req.Version,
		RelatedPatterns:   req.RelatedPatterns,
	}); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Delete handles DELETE /v1/api/patterns/:id.
//
// @Summary      Delete pattern
// @Tags         Patterns
// @Param        id   path      string  true  "Pattern UUID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  handlers.ProblemDetail
// @Failure      404  {object}  handlers.ProblemDetail
// @Failure      500  {object}  handlers.ProblemDetail
// @Router       /patterns/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	if err := h.patternSvc.Delete(c.Request.Context(), id); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetAgentAssociations handles GET /v1/api/patterns/:id/agents.
//
// @Summary      Get pattern agent associations
// @Tags         Patterns
// @Produce      json
// @Param        id   path      string  true  "Pattern UUID"
// @Success      200  {object}  associationsResponse
// @Failure      400  {object}  handlers.ProblemDetail
// @Failure      404  {object}  handlers.ProblemDetail
// @Failure      500  {object}  handlers.ProblemDetail
// @Router       /patterns/{id}/agents [get]
func (h *Handler) GetAgentAssociations(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	pgAssocs, err := h.patternSvc.GetAgentAssociations(c.Request.Context(), id)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	agentIDs := make([]uuid.UUID, len(pgAssocs))
	for i, a := range pgAssocs {
		agentIDs[i] = a.AgentID
	}
	names, err := h.patternSvc.ResolveAgentNames(c.Request.Context(), agentIDs)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, associationsResponse{Associations: toAssociationResponses(pgAssocs, names)})
}

// SetAgentAssociations handles PUT /v1/api/patterns/:id/agents.
//
// @Summary      Set pattern agent associations
// @Tags         Patterns
// @Accept       json
// @Param        id    path      string               true  "Pattern UUID"
// @Param        body  body      associationsRequest  true  "Agent associations to set"
// @Success      204   "No Content"
// @Failure      400   {object}  handlers.ProblemDetail
// @Failure      404   {object}  handlers.ProblemDetail
// @Failure      500   {object}  handlers.ProblemDetail
// @Router       /patterns/{id}/agents [put]
func (h *Handler) SetAgentAssociations(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	var req associationsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handlers.RespondValidationError(c, "The request body contains invalid fields", nil)
		return
	}

	if assocErrs := validateAssociationRelevance(req.Associations, "associations"); len(assocErrs) > 0 {
		handlers.RespondValidationError(c, "The request body contains invalid fields", assocErrs)
		return
	}

	inputs := toAssociationInputs(req.Associations)
	if err := h.patternSvc.SetAgentAssociations(c.Request.Context(), id, inputs); err != nil {
		handlers.RespondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetChunks handles GET /v1/api/patterns/:id/chunks.
//
// @Summary      Get pattern chunks
// @Tags         Patterns
// @Produce      json
// @Param        id   path      string  true  "Pattern UUID"
// @Success      200  {object}  chunkListResponse
// @Failure      400  {object}  handlers.ProblemDetail
// @Failure      404  {object}  handlers.ProblemDetail
// @Failure      500  {object}  handlers.ProblemDetail
// @Router       /patterns/{id}/chunks [get]
func (h *Handler) GetChunks(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		handlers.RespondValidationError(c, "Invalid pattern ID format", []handlers.FieldError{
			{Field: "id", Code: "INVALID_FORMAT", Message: "id must be a valid UUID"},
		})
		return
	}

	ctx := c.Request.Context()

	// Verify the pattern exists before listing chunks (returns 404 for unknown patterns).
	if _, err := h.patternSvc.Get(ctx, id); err != nil {
		handlers.RespondError(c, err)
		return
	}

	chunks, err := h.patternSvc.ListChunks(ctx, id)
	if err != nil {
		handlers.RespondError(c, err)
		return
	}
	summaries := make([]chunkSummary, len(chunks))
	for i, ch := range chunks {
		summaries[i] = chunkSummary{
			ChunkIndex:       ch.ChunkIndex,
			SectionTitle:     ch.SectionTitle,
			EnrichmentStatus: ch.EnrichmentStatus,
		}
	}
	c.JSON(http.StatusOK, chunkListResponse{Chunks: summaries, Count: len(summaries)})
}

// Search handles GET /v1/api/patterns/search.
//
// @Summary      Search patterns
// @Tags         Patterns
// @Produce      json
// @Param        q          query     string   true   "Search query (required, max 1000 chars)"
// @Param        limit      query     int      false  "Max results (1–50, default 10)"
// @Param        threshold  query     number   false  "Similarity threshold (0–1, default 0.7)"
// @Param        tags       query     string   false  "Comma-separated tag filter"
// @Param        agent      query     string   false  "Agent name filter"
// @Param        language   query     string   false  "Language filter"
// @Param        domain     query     string   false  "Domain filter"
// @Success      200        {object}  searchResponse
// @Failure      400        {object}  handlers.ProblemDetail
// @Failure      500        {object}  handlers.ProblemDetail
// @Router       /patterns/search [get]
func (h *Handler) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		handlers.RespondValidationError(c, "Search query is required", []handlers.FieldError{
			{Field: "q", Code: "REQUIRED", Message: "q query parameter is required"},
		})
		return
	}

	if utf8.RuneCountInString(query) > 1000 {
		handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
			{Field: "q", Code: "MAX_LENGTH", Message: "query must be 1000 characters or fewer"},
		})
		return
	}

	limit := 10
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 50 {
			handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
				{Field: "limit", Code: "INVALID_VALUE", Message: "limit must be an integer between 1 and 50"},
			})
			return
		}
		limit = parsed
	}

	threshold := 0.7
	if rawThreshold := c.Query("threshold"); rawThreshold != "" {
		parsed, err := strconv.ParseFloat(rawThreshold, 64)
		if err != nil || parsed < 0.0 || parsed > 1.0 {
			handlers.RespondValidationError(c, "Invalid query parameter", []handlers.FieldError{
				{Field: "threshold", Code: "INVALID_VALUE", Message: "threshold must be a number between 0 and 1"},
			})
			return
		}
		threshold = parsed
	}

	var tags []string
	if raw := c.Query("tags"); raw != "" {
		tags = strings.Split(raw, ",")
	}

	result, err := h.searchSvc.SearchPatterns(c.Request.Context(), searchsvc.SearchOptions{
		Query:     query,
		Limit:     limit,
		Threshold: threshold,
		Tags:      tags,
		AgentName: c.Query("agent"),
		Language:  c.Query("language"),
		Domain:    c.Query("domain"),
	})
	if err != nil {
		handlers.RespondError(c, err)
		return
	}

	results := make([]searchResultResponse, len(result.Matches))
	for i, m := range result.Matches {
		matchTags := m.Tags
		if matchTags == nil {
			matchTags = []string{}
		}
		results[i] = searchResultResponse{
			PatternID:    m.PatternID.String(),
			PatternName:  m.PatternName,
			EntityType:   m.EntityType,
			Language:     m.Language,
			Domain:       m.Domain,
			Tags:         matchTags,
			SectionTitle: m.SectionTitle,
			ChunkIndex:   m.ChunkIndex,
			Content:      m.Content,
			Similarity:   m.Similarity,
		}
	}

	c.JSON(http.StatusOK, searchResponse{
		Results: results,
		Metadata: searchMetadata{
			Query:            result.Query,
			TotalCandidates:  result.TotalCandidates,
			SearchDurationMs: result.SearchDurationMs,
		},
	})
}
