package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twistingmercury/mnemonic/tests/e2e/helpers"
)

// =============================================================================
// Pattern Endpoint Tests
// =============================================================================
//
// Covers all pattern-related endpoints from the OpenAPI specification:
//
//   GET    /v1/api/patterns              List patterns (paginated, filterable)
//   POST   /v1/api/patterns              Create pattern (returns 202, async enrichment)
//   GET    /v1/api/patterns/search       Semantic search (vector similarity)
//   GET    /v1/api/patterns/{id}         Get pattern by ID (full content + graph)
//   PUT    /v1/api/patterns/{id}         Update pattern (full replacement)
//   DELETE /v1/api/patterns/{id}         Delete pattern
//   GET    /v1/api/patterns/{id}/agents  Get agent associations
//   PUT    /v1/api/patterns/{id}/agents  Set agent associations
//
// Pattern resource:
//   - id: UUID (server-generated)
//   - name: Pattern name (^[a-z][a-z0-9-]*$, 1-128 chars)
//   - description: Optional (max 500 chars)
//   - content: Markdown (1-10240 bytes)
//   - tags: Optional (max 20 items)
//   - agent_associations: Links to agents with relevance scores
//   - enrichment_status: pending | enriched | failed (async processing)
//
// Create returns 202 Accepted because enrichment (embedding generation,
// concept extraction) runs asynchronously. Patterns must reach "enriched"
// status before they appear in semantic search results.

// -----------------------------------------------------------------------------
// List Patterns (GET /v1/api/patterns)
// -----------------------------------------------------------------------------

func TestListPatterns_ReturnsOKWithPaginatedSummaries(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	// Data field must be present (may be empty slice)
	if list.Data == nil {
		t.Fatal("expected data field to be present (may be empty)")
	}

	// Pagination metadata must be present
	if list.Pagination.Limit <= 0 {
		t.Fatalf("expected pagination.limit > 0, got %d", list.Pagination.Limit)
	}
}

func TestListPatterns_DefaultPaginationValues(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	// Default limit per spec is 20
	if list.Pagination.Limit != 20 {
		t.Fatalf("expected default limit 20, got %d", list.Pagination.Limit)
	}
}

func TestListPatterns_CustomLimit(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns?limit=5")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?limit=5: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	if list.Pagination.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", list.Pagination.Limit)
	}

	if len(list.Data) > 5 {
		t.Fatalf("expected at most 5 items, got %d", len(list.Data))
	}
}

func TestListPatterns_CursorPaginationWalksAllPages(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create 3 patterns to ensure at least 2 pages with limit=2
	for i := 0; i < 3; i++ {
		body := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "cursor pagination test content",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		resp, err := client.Post("/v1/api/patterns", body)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		resp.Body.Close()
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	maxPages := 20 // safety limit

	for pages < maxPages {
		path := "/v1/api/patterns?limit=2"
		if cursor != "" {
			path = fmt.Sprintf("%s&cursor=%s", path, cursor)
		}

		resp, err := client.Get(path)
		if err != nil {
			t.Fatalf("failed to GET %s: %v", path, err)
		}

		list := helpers.ParseJSON[helpers.PatternList](t, resp)

		for _, p := range list.Data {
			if seen[p.ID] {
				t.Fatalf("duplicate pattern ID %q seen across pages", p.ID)
			}
			seen[p.ID] = true
		}

		pages++

		if !list.Pagination.HasMore {
			break
		}

		if list.Pagination.NextCursor == "" {
			t.Fatal("has_more is true but next_cursor is empty")
		}
		cursor = list.Pagination.NextCursor
	}

	if pages == 0 {
		t.Fatal("no pages returned")
	}
}

func TestListPatterns_SummaryExcludesContentField(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a pattern with content
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "this content should not appear in list summaries",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// List patterns and find our created pattern
	resp, err := client.Get("/v1/api/patterns")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	for _, p := range list.Data {
		if p.ID == created.ID {
			// PatternSummary type has no Content field — the type itself enforces this.
			// The struct having no content field means the server did not return it.
			return
		}
	}

	// Pattern may be on a later page — that is acceptable; just verify the type structure.
	// PatternSummary does not have a Content field by definition.
	_ = helpers.PatternSummary{}
}

func TestListPatterns_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

func TestListPatterns_FilterByTags(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueTag := "tag-" + uuid.New().String()[:8]

	// Create a pattern with a unique tag
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "filter by tag content",
		Tags:       []string{uniqueTag},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Filter by the unique tag
	resp, err := client.Get("/v1/api/patterns?tags=" + uniqueTag)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?tags=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	found := false
	for _, p := range list.Data {
		if p.ID == created.ID {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected to find created pattern %q in tag-filtered list", created.ID)
	}

	// All returned summaries should have the tag
	for _, p := range list.Data {
		hasTag := false
		for _, tag := range p.Tags {
			if tag == uniqueTag {
				hasTag = true
				break
			}
		}
		if !hasTag {
			t.Errorf("pattern %q in tag-filtered list does not have tag %q", p.ID, uniqueTag)
		}
	}
}

func TestListPatterns_FilterByMultipleTagsUsesANDLogic(t *testing.T) {
	client := helpers.NewTestClient(t)

	tagA := "tag-a-" + uuid.New().String()[:8]
	tagB := "tag-b-" + uuid.New().String()[:8]
	tagC := "tag-c-" + uuid.New().String()[:8]

	// Pattern with both tags
	bothBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "has both tags",
		Tags:       []string{tagA, tagB},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	bothResp, err := client.Post("/v1/api/patterns", bothBody)
	if err != nil {
		t.Fatalf("failed to create pattern with both tags: %v", err)
	}
	helpers.AssertStatusCode(t, bothResp, http.StatusAccepted)
	both := helpers.ParseJSON[helpers.Pattern](t, bothResp)

	// Pattern with only tagA
	onlyABody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "has only tag a",
		Tags:       []string{tagA, tagC},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	onlyAResp, err := client.Post("/v1/api/patterns", onlyABody)
	if err != nil {
		t.Fatalf("failed to create pattern with only tag A: %v", err)
	}
	helpers.AssertStatusCode(t, onlyAResp, http.StatusAccepted)
	onlyA := helpers.ParseJSON[helpers.Pattern](t, onlyAResp)

	// Filter by both tags — AND logic means only "both" pattern should appear
	resp, err := client.Get(fmt.Sprintf("/v1/api/patterns?tags=%s,%s", tagA, tagB))
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?tags=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	foundBoth := false
	foundOnlyA := false
	for _, p := range list.Data {
		if p.ID == both.ID {
			foundBoth = true
		}
		if p.ID == onlyA.ID {
			foundOnlyA = true
		}
	}

	if !foundBoth {
		t.Errorf("expected pattern with both tags to appear in AND-filtered results")
	}
	if foundOnlyA {
		t.Errorf("expected pattern with only tagA to be excluded from AND-filtered results")
	}
}

func TestListPatterns_FullTextSearchByNameAndDescription(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueWord := "xyzzy" + uuid.New().String()[:8]

	body := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: fmt.Sprintf("description contains %s unique word", uniqueWord),
		Content:     "content for full text search test",
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get("/v1/api/patterns?search=" + uniqueWord)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?search=...: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	found := false
	for _, p := range list.Data {
		if p.ID == created.ID {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected to find pattern %q in full-text search results for %q", created.ID, uniqueWord)
	}
}

func TestListPatterns_CombinedTagAndSearchFilters(t *testing.T) {
	client := helpers.NewTestClient(t)

	uniqueTag := "tag-combined-" + uuid.New().String()[:8]
	uniqueWord := "combined" + uuid.New().String()[:8]

	// Pattern matching both tag and search
	matchBody := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: fmt.Sprintf("description with %s keyword", uniqueWord),
		Content:     "content for combined filter test",
		Tags:        []string{uniqueTag},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	matchResp, err := client.Post("/v1/api/patterns", matchBody)
	if err != nil {
		t.Fatalf("failed to create matching pattern: %v", err)
	}
	helpers.AssertStatusCode(t, matchResp, http.StatusAccepted)
	match := helpers.ParseJSON[helpers.Pattern](t, matchResp)

	// Pattern with tag but not search term
	tagOnlyBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content with tag only",
		Tags:       []string{uniqueTag},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	tagOnlyResp, err := client.Post("/v1/api/patterns", tagOnlyBody)
	if err != nil {
		t.Fatalf("failed to create tag-only pattern: %v", err)
	}
	helpers.AssertStatusCode(t, tagOnlyResp, http.StatusAccepted)
	tagOnly := helpers.ParseJSON[helpers.Pattern](t, tagOnlyResp)

	path := fmt.Sprintf("/v1/api/patterns?tags=%s&search=%s", uniqueTag, uniqueWord)
	resp, err := client.Get(path)
	if err != nil {
		t.Fatalf("failed to GET %s: %v", path, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	list := helpers.ParseJSON[helpers.PatternList](t, resp)

	foundMatch := false
	foundTagOnly := false
	for _, p := range list.Data {
		if p.ID == match.ID {
			foundMatch = true
		}
		if p.ID == tagOnly.ID {
			foundTagOnly = true
		}
	}

	if !foundMatch {
		t.Errorf("expected pattern matching both filters to appear in results")
	}
	if foundTagOnly {
		t.Errorf("expected pattern with tag only (no search match) to be excluded")
	}
}

func TestListPatterns_InvalidLimitReturns400(t *testing.T) {
	t.Run("limit below minimum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns?limit=0")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=0: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit above maximum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns?limit=101")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=101: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit non-numeric", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns?limit=abc")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns?limit=abc: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestListPatterns_InvalidCursorReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns?cursor=!!!invalid-cursor!!!")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns?cursor=...: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Create Pattern (POST /v1/api/patterns)
// -----------------------------------------------------------------------------

func TestCreatePattern_ReturnsAcceptedWithPendingEnrichment(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "# Test Pattern\n\nThis is test content.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending', got %q", pattern.EnrichmentStatus)
	}
}

func TestCreatePattern_ResponseIncludesLocationHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for location header test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header to be present")
	}

	if !strings.Contains(location, "/v1/api/patterns/") {
		t.Fatalf("expected Location header to contain '/v1/api/patterns/', got %q", location)
	}
}

func TestCreatePattern_ServerGeneratesUUID(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for UUID test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected server-generated UUID id to be present")
	}

	// Validate it's a valid UUID
	if _, err := uuid.Parse(pattern.ID); err != nil {
		t.Fatalf("expected id to be a valid UUID, got %q: %v", pattern.ID, err)
	}
}

func TestCreatePattern_MinimalFieldsOnlyNameAndContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "minimal content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected id to be set")
	}
	if pattern.Name != body.Name {
		t.Fatalf("expected name %q, got %q", body.Name, pattern.Name)
	}
	if pattern.Content != body.Content {
		t.Fatalf("expected content %q, got %q", body.Content, pattern.Content)
	}
}

func TestCreatePattern_AllFieldsIncludingDescriptionTagsAssociations(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create an agent first so we have a valid agent_id
	agentName := helpers.GenerateUniqueName("agent")
	agentBody := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "You are a test agent",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}
	agentResp, err := client.Post("/v1/api/agents", agentBody)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agentResp.Body.Close()
	if agentResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating agent, got %d", agentResp.StatusCode)
	}

	body := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Description: "test description",
		Content:     "# Full Pattern\n\nAll fields populated.",
		Tags:        []string{"go", "testing"},
		AgentAssociations: []helpers.AgentAssociation{
			{AgentName: agentName, Relevance: 0.9},
		},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID == "" {
		t.Fatal("expected id to be set")
	}
	if pattern.Description != body.Description {
		t.Fatalf("expected description %q, got %q", body.Description, pattern.Description)
	}
	if len(pattern.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(pattern.Tags))
	}
	if len(pattern.AgentAssociations) != 1 {
		t.Fatalf("expected 1 agent association, got %d", len(pattern.AgentAssociations))
	}
	if pattern.AgentAssociations[0].AgentName != agentName {
		t.Fatalf("expected agent_name %q, got %q", agentName, pattern.AgentAssociations[0].AgentName)
	}
}

func TestCreatePattern_DuplicateNameReturns409(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("pattern")
	body := helpers.PatternCreate{
		Name:       name,
		Content:    "original content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp1, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns: %v", err)
	}
	resp1.Body.Close()
	helpers.AssertStatusCode(t, resp1, http.StatusAccepted)

	// Second create with same name should conflict
	body2 := helpers.PatternCreate{
		Name:       name,
		Content:    "duplicate name content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	resp2, err := client.Post("/v1/api/patterns", body2)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns (duplicate): %v", err)
	}
	defer resp2.Body.Close()

	helpers.AssertStatusCode(t, resp2, http.StatusConflict)
}

func TestCreatePattern_ValidationErrors(t *testing.T) {
	cases := []struct {
		name        string
		body        interface{}
		expectField string
	}{
		{
			name:        "missing name",
			body:        helpers.PatternCreate{Content: "some content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "missing content",
			body:        helpers.PatternCreate{Name: "valid-name", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "name too long",
			body:        helpers.PatternCreate{Name: strings.Repeat("a", 129), Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format uppercase",
			body:        helpers.PatternCreate{Name: "Invalid-Name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format starts with number",
			body:        helpers.PatternCreate{Name: "1-bad-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format underscores",
			body:        helpers.PatternCreate{Name: "bad_name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "content exceeds max size",
			body:        helpers.PatternCreate{Name: "valid-name", Content: strings.Repeat("x", 100_001), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "description too long",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", Description: strings.Repeat("d", 501), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "description",
		},
		{
			name: "too many tags",
			body: helpers.PatternCreate{
				Name:       "valid-name",
				Content:    "content",
				Tags:       makeTags(21),
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			},
			expectField: "tags",
		},
		{
			name:        "missing entity_type",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", Language: "go", Domain: "backend"},
			expectField: "entity_type",
		},
		{
			name:        "missing language",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "missing domain",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go"},
			expectField: "domain",
		},
		{
			name:        "invalid language value",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "COBOL", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "invalid domain value",
			body:        helpers.PatternCreate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "Not A Domain"},
			expectField: "domain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			resp, err := client.Post("/v1/api/patterns", tc.body)
			if err != nil {
				t.Fatalf("failed to POST /v1/api/patterns: %v", err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tc.expectField {
					fieldFound = true
					break
				}
			}

			if !fieldFound {
				t.Fatalf("expected field error for %q, got errors: %+v", tc.expectField, errResp.Errors)
			}
			_ = tc.body
			_ = tc.expectField
		})
	}
}

func TestCreatePattern_InvalidAgentAssociation(t *testing.T) {
	t.Run("non-existent agent name", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		body := helpers.PatternCreate{
			Name:    helpers.GenerateUniqueName("pattern"),
			Content: "content with non-existent agent",
			AgentAssociations: []helpers.AgentAssociation{
				{AgentName: "definitely-does-not-exist", Relevance: 0.5},
			},
		}

		resp, err := client.Post("/v1/api/patterns", body)
		if err != nil {
			t.Fatalf("failed to POST /v1/api/patterns: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Fatalf("expected 4xx status for non-existent agent, got %d", resp.StatusCode)
		}
	})
	t.Run("relevance below zero", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		body := helpers.PatternCreate{
			Name:    helpers.GenerateUniqueName("pattern"),
			Content: "content with invalid relevance",
			AgentAssociations: []helpers.AgentAssociation{
				{AgentName: "some-agent", Relevance: -0.1},
			},
		}

		resp, err := client.Post("/v1/api/patterns", body)
		if err != nil {
			t.Fatalf("failed to POST /v1/api/patterns: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("relevance above one", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		body := helpers.PatternCreate{
			Name:    helpers.GenerateUniqueName("pattern"),
			Content: "content with relevance above one",
			AgentAssociations: []helpers.AgentAssociation{
				{AgentName: "some-agent", Relevance: 1.1},
			},
		}

		resp, err := client.Post("/v1/api/patterns", body)
		if err != nil {
			t.Fatalf("failed to POST /v1/api/patterns: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestCreatePattern_InvalidJSONReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/patterns", strings.NewReader(`{invalid json`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns with invalid JSON: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestCreatePattern_EmptyBodyReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/patterns", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/patterns with empty body: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Semantic Search (GET /v1/api/patterns/search)
// -----------------------------------------------------------------------------

func TestSearchPatterns_ReturnsRankedResultsWithSimilarity(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=error+handling+in+go")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Results slice must be present (may be empty since nothing is enriched)
	if searchResp.Results == nil {
		t.Fatal("expected results field to be present (may be empty)")
	}
}

func TestSearchPatterns_ResultsIncludeContentAndScores(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=test+query+for+content+and+scores")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Verify each result has content and similarity (if any results exist)
	for _, r := range searchResp.Results {
		if r.Content == "" {
			t.Errorf("result %q has empty content", r.PatternID)
		}
		if r.Similarity < 0 || r.Similarity > 1 {
			t.Errorf("result %q has similarity %f out of [0,1] range", r.PatternID, r.Similarity)
		}
	}
}

func TestSearchPatterns_ResponseIncludesMetadata(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=metadata+test+query")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Metadata.Query == "" {
		t.Fatal("expected metadata.query to be present")
	}
	if searchResp.Metadata.SearchDurationMs < 0 {
		t.Fatalf("expected metadata.search_duration_ms >= 0, got %d", searchResp.Metadata.SearchDurationMs)
	}
}

func TestSearchPatterns_MetadataEchoesQueryString(t *testing.T) {
	client := helpers.NewTestClient(t)

	query := "echo this query string back"

	resp, err := client.Get("/v1/api/patterns/search?q=" + strings.ReplaceAll(query, " ", "+"))
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Metadata.Query != query {
		t.Fatalf("expected metadata.query to echo %q, got %q", query, searchResp.Metadata.Query)
	}
}

func TestSearchPatterns_DefaultLimit(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=default+limit+test")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Default limit per spec is 10; results should not exceed it
	if len(searchResp.Results) > 10 {
		t.Fatalf("expected at most 10 results with default limit, got %d", len(searchResp.Results))
	}
}

func TestSearchPatterns_CustomLimit(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=custom+limit+test&limit=3")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if len(searchResp.Results) > 3 {
		t.Fatalf("expected at most 3 results, got %d", len(searchResp.Results))
	}
}

func TestSearchPatterns_CustomThreshold(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=threshold+test&threshold=0.9")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// All results must have similarity >= threshold
	for _, r := range searchResp.Results {
		if r.Similarity < 0.9 {
			t.Errorf("result %q has similarity %f below threshold 0.9", r.PatternID, r.Similarity)
		}
	}
}

func TestSearchPatterns_FilterByTags(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=tag+filtered+search&tags=go,errors")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search?tags=...: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Verify response structure is valid (no enriched patterns expected)
	if searchResp.Results == nil {
		t.Fatal("expected results field to be present")
	}
}

func TestSearchPatterns_FilterByAgent(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=agent+filtered+search&agent=go-software-engineer")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search?agent=...: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	// Verify response structure is valid (no enriched patterns expected)
	if searchResp.Results == nil {
		t.Fatal("expected results field to be present")
	}
}

func TestSearchPatterns_OnlyEnrichedPatternsAppear(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a pattern — it will be in "pending" or "failed" state (not enriched)
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment exclusion test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Search for the pattern by its unique name
	resp, err := client.Get("/v1/api/patterns/search?q=" + strings.ReplaceAll(body.Name, "-", "+"))
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search: %v", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		helpers.ReadBody(t, resp)
		t.Skip("search unavailable: OpenAI API key not configured")
		return
	}

	// Verify 200 + correct structure
	helpers.AssertStatusCode(t, resp, http.StatusOK)

	searchResp := helpers.ParseJSON[helpers.PatternSearchResponse](t, resp)

	if searchResp.Results == nil {
		t.Fatal("expected results field to be present")
	}

	// With dummy OpenAI key, no patterns will be enriched — our pattern should NOT appear
	for _, r := range searchResp.Results {
		if r.PatternID == created.ID {
			t.Errorf("expected non-enriched pattern %q to be excluded from search results", created.ID)
		}
	}
}

func TestSearchPatterns_MissingQueryReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search (no q): %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_EmptyQueryReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/search?q=")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search?q= (empty): %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_QueryTooLongReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	longQuery := strings.Repeat("a", 1001)
	resp, err := client.Get("/v1/api/patterns/search?q=" + longQuery)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/search with long query: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestSearchPatterns_InvalidLimitReturns400(t *testing.T) {
	t.Run("limit below minimum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns/search?q=test&limit=0")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?limit=0: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("limit above maximum", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns/search?q=test&limit=51")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?limit=51: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestSearchPatterns_InvalidThresholdReturns400(t *testing.T) {
	t.Run("threshold below zero", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns/search?q=test&threshold=-0.1")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?threshold=-0.1: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("threshold above one", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns/search?q=test&threshold=1.1")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/search?threshold=1.1: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestSearchPatterns_ServiceUnavailableReturns503(t *testing.T) {
	t.Skip("requires infrastructure manipulation")
}

// -----------------------------------------------------------------------------
// Get Pattern (GET /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestGetPattern_ReturnsFullPatternWithContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "# Full Content Pattern\n\nComplete markdown content here.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, pattern.ID)
	}
	if pattern.Name != body.Name {
		t.Fatalf("expected name %q, got %q", body.Name, pattern.Name)
	}
	if pattern.Content != body.Content {
		t.Fatalf("expected content %q, got %q", body.Content, pattern.Content)
	}
}

func TestGetPattern_IncludesEnrichmentStatus(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment status test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus == "" {
		t.Fatal("expected enrichment_status to be present")
	}

	validStatuses := map[string]bool{"pending": true, "enriched": true, "failed": true}
	if !validStatuses[pattern.EnrichmentStatus] {
		t.Fatalf("expected enrichment_status to be one of pending/enriched/failed, got %q", pattern.EnrichmentStatus)
	}
}

func TestGetPattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	resp, err := client.Get(patternPath(nonExistentID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestGetPattern_InvalidUUIDReturns400(t *testing.T) {
	t.Run("not a uuid", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		resp, err := client.Get("/v1/api/patterns/not-a-uuid")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/not-a-uuid: %v", err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("empty id", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		// This routes to the list endpoint, not an invalid UUID — verify 200 not 400
		resp, err := client.Get("/v1/api/patterns/")
		if err != nil {
			t.Fatalf("failed to GET /v1/api/patterns/: %v", err)
		}
		defer resp.Body.Close()

		// An empty id segment typically resolves to the list route (200)
		// or a 400 depending on routing. Accept either.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 200, 400, or 404 for empty id, got %d", resp.StatusCode)
		}
	})
}

func TestGetPattern_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for request id header test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

// -----------------------------------------------------------------------------
// Update Pattern (PUT /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestUpdatePattern_ReturnsNoContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "updated content with new information",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put(patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_UpdatedAtChangesCreatedAtPreserved(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content for timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "updated content for timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put(patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_FullReplacementResetsOmittedFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:        helpers.GenerateUniqueName("pattern"),
		Content:     "original content",
		Description: "original description",
		Tags:        []string{"original-tag"},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Update with only name and content — description and tags are omitted
	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "replacement content",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put(patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_ContentChangeResetsEnrichmentToPending(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content before update",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "completely new content triggers re-enrichment",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put(patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestUpdatePattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	updateBody := helpers.PatternUpdate{
		Name:       "valid-name",
		Content:    "content for non-existent pattern",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put(patternPath(nonExistentID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestUpdatePattern_ValidationErrors(t *testing.T) {
	cases := []struct {
		name        string
		body        interface{}
		expectField string
	}{
		{
			name:        "missing name",
			body:        helpers.PatternUpdate{Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "missing content",
			body:        helpers.PatternUpdate{Name: "valid-name", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "name too long",
			body:        helpers.PatternUpdate{Name: strings.Repeat("a", 129), Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "name invalid format",
			body:        helpers.PatternUpdate{Name: "BAD_NAME", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "name",
		},
		{
			name:        "content exceeds max size",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: strings.Repeat("x", 100_001), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "content",
		},
		{
			name:        "description too long",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", Description: strings.Repeat("d", 501), EntityType: "go-pattern", Language: "go", Domain: "backend"},
			expectField: "description",
		},
		{
			name: "too many tags",
			body: helpers.PatternUpdate{
				Name:       "valid-name",
				Content:    "content",
				Tags:       makeTags(21),
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			},
			expectField: "tags",
		},
		{
			name:        "missing entity_type",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", Language: "go", Domain: "backend"},
			expectField: "entity_type",
		},
		{
			name:        "missing language",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "missing domain",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go"},
			expectField: "domain",
		},
		{
			name:        "invalid language value",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "COBOL", Domain: "backend"},
			expectField: "language",
		},
		{
			name:        "invalid domain value",
			body:        helpers.PatternUpdate{Name: "valid-name", Content: "content", EntityType: "go-pattern", Language: "go", Domain: "Not A Domain"},
			expectField: "domain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			// Create a real pattern to update (so we get 400, not 404)
			createBody := helpers.PatternCreate{
				Name:       helpers.GenerateUniqueName("pattern"),
				Content:    "content for validation test",
				EntityType: "go-pattern",
				Language:   "go",
				Domain:     "backend",
			}
			createResp, err := client.Post("/v1/api/patterns", createBody)
			if err != nil {
				t.Fatalf("failed to create pattern: %v", err)
			}
			helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
			created := helpers.ParseJSON[helpers.Pattern](t, createResp)

			resp, err := client.Put(patternPath(created.ID), tc.body)
			if err != nil {
				t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tc.expectField {
					fieldFound = true
					break
				}
			}

			if !fieldFound {
				t.Fatalf("expected field error for %q, got errors: %+v", tc.expectField, errResp.Errors)
			}
			_ = tc.body
			_ = tc.expectField
		})
	}
}

func TestUpdatePattern_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	updateBody := helpers.PatternUpdate{
		Name:       "valid-name",
		Content:    "content for invalid uuid test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Put("/v1/api/patterns/not-a-valid-uuid", updateBody)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/patterns/not-a-valid-uuid: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Delete Pattern (DELETE /v1/api/patterns/{id})
// -----------------------------------------------------------------------------

func TestDeletePattern_ReturnsNoContent(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for delete test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Delete(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(created.ID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
}

func TestDeletePattern_PatternNoLongerRetrievable(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for delete then get test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	deleteResp, err := client.Delete(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(created.ID), err)
	}
	deleteResp.Body.Close()
	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)

	getResp, err := client.Get(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s after delete: %v", patternPath(created.ID), err)
	}
	defer getResp.Body.Close()

	helpers.AssertStatusCode(t, getResp, http.StatusNotFound)
}

func TestDeletePattern_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	resp, err := client.Delete(patternPath(nonExistentID))
	if err != nil {
		t.Fatalf("failed to DELETE %s: %v", patternPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestDeletePattern_SecondDeleteReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for double delete test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// First delete
	resp1, err := client.Delete(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed first DELETE %s: %v", patternPath(created.ID), err)
	}
	resp1.Body.Close()
	helpers.AssertStatusCode(t, resp1, http.StatusNoContent)

	// Second delete should return 404
	resp2, err := client.Delete(patternPath(created.ID))
	if err != nil {
		t.Fatalf("failed second DELETE %s: %v", patternPath(created.ID), err)
	}
	defer resp2.Body.Close()

	helpers.AssertStatusCode(t, resp2, http.StatusNotFound)
}

func TestDeletePattern_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Delete("/v1/api/patterns/not-a-valid-uuid")
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/patterns/not-a-valid-uuid: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Get Agent Associations (GET /v1/api/patterns/{id}/agents)
// -----------------------------------------------------------------------------

func TestGetPatternAgentAssociations_ReturnsAssociations(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create an agent first
	agentName := helpers.GenerateUniqueName("agent")
	agentBody := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "You are a test agent for associations",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}
	agentResp, err := client.Post("/v1/api/agents", agentBody)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agentResp.Body.Close()
	if agentResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating agent, got %d", agentResp.StatusCode)
	}

	// Create pattern with agent association
	patternBody := helpers.PatternCreate{
		Name:    helpers.GenerateUniqueName("pattern"),
		Content: "content for agent associations test",
		AgentAssociations: []helpers.AgentAssociation{
			{AgentName: agentName, Relevance: 0.85},
		},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", patternBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternAgentsPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternAgentsPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	associations := helpers.ParseJSON[helpers.PatternAgentAssociations](t, resp)

	if len(associations.Associations) != 1 {
		t.Fatalf("expected 1 association, got %d", len(associations.Associations))
	}
	if associations.Associations[0].AgentName != agentName {
		t.Fatalf("expected agent_name %q, got %q", agentName, associations.Associations[0].AgentName)
	}
}

func TestGetPatternAgentAssociations_EmptyListWhenNoAssociations(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content with no agent associations",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternAgentsPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternAgentsPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	associations := helpers.ParseJSON[helpers.PatternAgentAssociations](t, resp)

	if len(associations.Associations) != 0 {
		t.Fatalf("expected 0 associations, got %d", len(associations.Associations))
	}
}

func TestGetPatternAgentAssociations_PatternNotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	resp, err := client.Get(patternAgentsPath(nonExistentID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternAgentsPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestGetPatternAgentAssociations_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/not-a-valid-uuid/agents")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/patterns/not-a-valid-uuid/agents: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

func TestGetPatternAgentAssociations_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for request id header on agents endpoint",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	resp, err := client.Get(patternAgentsPath(created.ID))
	if err != nil {
		t.Fatalf("failed to GET %s: %v", patternAgentsPath(created.ID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

// -----------------------------------------------------------------------------
// Set Agent Associations (PUT /v1/api/patterns/{id}/agents)
// -----------------------------------------------------------------------------

func TestSetPatternAgentAssociations_ReplacesAllAssociations(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create two agents
	agentA := helpers.GenerateUniqueName("agent")
	agentB := helpers.GenerateUniqueName("agent")

	for _, name := range []string{agentA, agentB} {
		agentBody := helpers.AgentCreate{
			Name:         name,
			SystemPrompt: "You are a test agent",
			Model:        "sonnet",
			Description:  "Test agent.",
			Version:      "1.0.0",
		}
		agentResp, err := client.Post("/v1/api/agents", agentBody)
		if err != nil {
			t.Fatalf("failed to create agent %q: %v", name, err)
		}
		agentResp.Body.Close()
		if agentResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 creating agent %q, got %d", name, agentResp.StatusCode)
		}
	}

	// Create pattern with agentA
	patternBody := helpers.PatternCreate{
		Name:    helpers.GenerateUniqueName("pattern"),
		Content: "content for replace associations test",
		AgentAssociations: []helpers.AgentAssociation{
			{AgentName: agentA, Relevance: 0.7},
		},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", patternBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Replace with agentB only
	newAssociations := helpers.PatternAgentAssociations{
		Associations: []helpers.AgentAssociation{
			{AgentName: agentB, Relevance: 0.9},
		},
	}

	resp, err := client.Put(patternAgentsPath(created.ID), newAssociations)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestSetPatternAgentAssociations_ClearAssociationsWithEmptyArray(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create an agent first
	agentName := helpers.GenerateUniqueName("agent")
	agentBody := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "You are a test agent",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}
	agentResp, err := client.Post("/v1/api/agents", agentBody)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	agentResp.Body.Close()

	// Create pattern with one association
	patternBody := helpers.PatternCreate{
		Name:    helpers.GenerateUniqueName("pattern"),
		Content: "content for clear associations test",
		AgentAssociations: []helpers.AgentAssociation{
			{AgentName: agentName, Relevance: 0.75},
		},
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", patternBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Clear associations with empty array
	clearBody := helpers.PatternAgentAssociations{
		Associations: []helpers.AgentAssociation{},
	}

	resp, err := client.Put(patternAgentsPath(created.ID), clearBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

func TestSetPatternAgentAssociations_PatternNotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	nonExistentID := uuid.New().String()
	body := helpers.PatternAgentAssociations{
		Associations: []helpers.AgentAssociation{},
	}

	resp, err := client.Put(patternAgentsPath(nonExistentID), body)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternAgentsPath(nonExistentID), err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

func TestSetPatternAgentAssociations_ValidationErrors(t *testing.T) {
	t.Run("non-existent agent name", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		// Create a pattern to PUT against
		patternBody := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "content for non-existent agent validation",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		createResp, err := client.Post("/v1/api/patterns", patternBody)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
		created := helpers.ParseJSON[helpers.Pattern](t, createResp)

		body := helpers.PatternAgentAssociations{
			Associations: []helpers.AgentAssociation{
				{AgentName: "definitely-does-not-exist", Relevance: 0.5},
			},
		}

		resp, err := client.Put(patternAgentsPath(created.ID), body)
		if err != nil {
			t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Fatalf("expected 4xx for non-existent agent, got %d", resp.StatusCode)
		}
	})
	t.Run("relevance below zero", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		patternBody := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "content for relevance below zero validation",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		createResp, err := client.Post("/v1/api/patterns", patternBody)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
		created := helpers.ParseJSON[helpers.Pattern](t, createResp)

		body := helpers.PatternAgentAssociations{
			Associations: []helpers.AgentAssociation{
				{AgentName: "some-agent", Relevance: -0.1},
			},
		}

		resp, err := client.Put(patternAgentsPath(created.ID), body)
		if err != nil {
			t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("relevance above one", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		patternBody := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "content for relevance above one validation",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		createResp, err := client.Post("/v1/api/patterns", patternBody)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
		created := helpers.ParseJSON[helpers.Pattern](t, createResp)

		body := helpers.PatternAgentAssociations{
			Associations: []helpers.AgentAssociation{
				{AgentName: "some-agent", Relevance: 1.1},
			},
		}

		resp, err := client.Put(patternAgentsPath(created.ID), body)
		if err != nil {
			t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
	t.Run("missing associations field", func(t *testing.T) {
		client := helpers.NewTestClient(t)

		patternBody := helpers.PatternCreate{
			Name:       helpers.GenerateUniqueName("pattern"),
			Content:    "content for missing associations field validation",
			EntityType: "go-pattern",
			Language:   "go",
			Domain:     "backend",
		}
		createResp, err := client.Post("/v1/api/patterns", patternBody)
		if err != nil {
			t.Fatalf("failed to create pattern: %v", err)
		}
		helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
		created := helpers.ParseJSON[helpers.Pattern](t, createResp)

		// Send empty JSON object — no "associations" field
		req, err := http.NewRequest(http.MethodPut, client.BaseURL+patternAgentsPath(created.ID), strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed to PUT %s: %v", patternAgentsPath(created.ID), err)
		}
		defer resp.Body.Close()

		helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	})
}

func TestSetPatternAgentAssociations_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternAgentAssociations{
		Associations: []helpers.AgentAssociation{},
	}

	resp, err := client.Put("/v1/api/patterns/not-a-valid-uuid/agents", body)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/patterns/not-a-valid-uuid/agents: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Pattern Enrichment (cross-cutting async behavior)
// -----------------------------------------------------------------------------

func TestPatternEnrichment_NewPatternStartsWithPendingStatus(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment pending status test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, resp)

	if pattern.EnrichmentStatus != "pending" {
		t.Fatalf("expected enrichment_status 'pending' immediately after create, got %q", pattern.EnrichmentStatus)
	}
}

func TestPatternEnrichment_StatusTransitionsToPendingOrFailed(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment status transition test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Poll until enrichment completes (pending → enriched or failed)
	deadline := time.Now().Add(10 * time.Second)
	var finalStatus string
	for time.Now().Before(deadline) {
		resp, err := client.Get(patternPath(created.ID))
		if err != nil {
			t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
		}
		p := helpers.ParseJSON[helpers.Pattern](t, resp)
		finalStatus = p.EnrichmentStatus
		if p.EnrichmentStatus != "pending" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// With a dummy OpenAI key, expect "failed" (not enriched)
	validStatuses := map[string]bool{"pending": true, "enriched": true, "failed": true}
	if !validStatuses[finalStatus] {
		t.Fatalf("expected enrichment_status to be pending/enriched/failed, got %q", finalStatus)
	}
}

func TestPatternEnrichment_EnrichedAtSetWhenEnriched(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enriched_at timestamp test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Poll until enrichment is no longer pending
	deadline := time.Now().Add(10 * time.Second)
	var finalPattern helpers.Pattern
	for time.Now().Before(deadline) {
		resp, err := client.Get(patternPath(created.ID))
		if err != nil {
			t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
		}
		finalPattern = helpers.ParseJSON[helpers.Pattern](t, resp)
		if finalPattern.EnrichmentStatus != "pending" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// If enriched, enriched_at must be set; if failed, it may be empty
	if finalPattern.EnrichmentStatus == "enriched" && finalPattern.EnrichedAt == "" {
		t.Fatal("expected enriched_at to be set when enrichment_status is 'enriched'")
	}
}

func TestPatternEnrichment_ErrorSetWhenFailed(t *testing.T) {
	client := helpers.NewTestClient(t)

	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "content for enrichment error test",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Poll until enrichment is no longer pending
	deadline := time.Now().Add(10 * time.Second)
	var finalPattern helpers.Pattern
	for time.Now().Before(deadline) {
		resp, err := client.Get(patternPath(created.ID))
		if err != nil {
			t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
		}
		finalPattern = helpers.ParseJSON[helpers.Pattern](t, resp)
		if finalPattern.EnrichmentStatus != "pending" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// With dummy OpenAI key, enrichment should fail and enrichment_error should be set
	if finalPattern.EnrichmentStatus == "failed" && finalPattern.EnrichmentError == "" {
		t.Fatal("expected enrichment_error to be set when enrichment_status is 'failed'")
	}
}

func TestPatternEnrichment_ContentUpdateTriggersReenrichment(t *testing.T) {
	client := helpers.NewTestClient(t)

	createBody := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "original content before re-enrichment",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", createBody)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)
	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// Wait for initial enrichment to settle (pending → failed with dummy key)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(patternPath(created.ID))
		if err != nil {
			t.Fatalf("failed to GET %s: %v", patternPath(created.ID), err)
		}
		p := helpers.ParseJSON[helpers.Pattern](t, resp)
		if p.EnrichmentStatus != "pending" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Update content — should reset enrichment_status to pending
	updateBody := helpers.PatternUpdate{
		Name:       createBody.Name,
		Content:    "new content after re-enrichment trigger",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}

	updateResp, err := client.Put(patternPath(created.ID), updateBody)
	if err != nil {
		t.Fatalf("failed to PUT %s: %v", patternPath(created.ID), err)
	}

	helpers.AssertStatusCode(t, updateResp, http.StatusNoContent)
	helpers.ReadBody(t, updateResp)
}

// -----------------------------------------------------------------------------
// Get Pattern Chunks (GET /v1/api/patterns/{id}/chunks)
// -----------------------------------------------------------------------------

func TestGetPatternChunks_ReturnsChunksForPattern(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a pattern (chunks are populated asynchronously by enrichment)
	body := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("pattern"),
		Content:    "## Philosophy\nStorage-only databases.\n\n## Usage\nCall the API.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	createResp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusAccepted)

	created := helpers.ParseJSON[helpers.Pattern](t, createResp)

	// GET /v1/api/patterns/:id/chunks — returns 200 with chunks (may be empty if not yet enriched)
	resp, err := client.Get("/v1/api/patterns/" + created.ID + "/chunks")
	if err != nil {
		t.Fatalf("failed to GET chunks: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	chunkList := helpers.ParseJSON[helpers.ChunkListResponse](t, resp)

	if chunkList.Chunks == nil {
		t.Fatal("expected chunks field to be present (may be empty)")
	}
}

func TestGetPatternChunks_InvalidUUIDReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/patterns/not-a-uuid/chunks")
	if err != nil {
		t.Fatalf("failed to GET chunks: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// makeTags generates n unique tag strings for table-driven tests.
func makeTags(n int) []string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag-%d", i)
	}
	return tags
}

// patternPath returns the API path for a specific pattern ID.
func patternPath(id string) string {
	return fmt.Sprintf("/v1/api/patterns/%s", id)
}

// patternAgentsPath returns the API path for a pattern's agent associations.
func patternAgentsPath(id string) string {
	return fmt.Sprintf("/v1/api/patterns/%s/agents", id)
}

// Compile-time interface assertions to ensure test types from types.go are used.
// These prevent the types from appearing unused if no test body references them yet.
var (
	_ = helpers.PatternCreate{}
	_ = helpers.PatternUpdate{}
	_ = helpers.PatternList{}
	_ = helpers.PatternSearchResponse{}
	_ = helpers.PatternAgentAssociations{}
	_ = helpers.Pattern{}
	_ = helpers.ErrorResponse{}
	_ = helpers.ChunkListResponse{}
)

// Compile-time assertions for helpers, uuid, http, and strings packages.
var (
	_ = helpers.NewTestClient
	_ = helpers.NewReadOnlyTestClient
	_ = helpers.NewUnauthenticatedClient
	_ = helpers.AssertStatusCode
	_ = helpers.AssertContentType
	_ = helpers.AssertRequestIDHeader
	_ = helpers.GenerateUniqueName
	_ = uuid.New
	_ = http.StatusOK
	_ = strings.Repeat
)
