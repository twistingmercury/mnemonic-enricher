package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/twistingmercury/mnemonic/tests/e2e/helpers"
)

// =============================================================================
// Skill Endpoint Tests (/v1/api/skills, /v1/api/skills/{name})
// =============================================================================
//
// Skills represent reusable instruction sets (SKILL.md files) that can be
// synced to Claude Code's local filesystem. Each skill has:
//   - name: Unique identifier (lowercase letters, numbers, hyphens, max 64 chars)
//   - content: SKILL.md content (max 512KB)
//   - description: Optional short description (max 1024 chars)
//   - tags: Optional categorization tags (max 20)
//   - license: Optional license identifier (max 255 chars)
//   - compatibility: Optional compatibility info (max 500 chars)
//   - metadata: Optional key-value pairs (string values)
//   - allowed_tools: Optional tool names (max 50 items)
//   - version: Optional version string (max 50 chars)
//
// Authorization (MVP):
//   - No authentication required (localhost trusted environment)
//
// Related endpoints:
//   - Skill files: /v1/api/skills/{name}/scripts|references|assets

// -----------------------------------------------------------------------------
// List Skills Tests (GET /v1/api/skills)
// OpenAPI: listSkills
// -----------------------------------------------------------------------------

// TestListSkills_Success verifies listing skills returns paginated results
// with full content included (sync use case requires full content).
//
// Expected behavior:
//   - Returns 200 OK
//   - Response contains data array with Skill objects (including content)
//   - Response contains pagination metadata
//   - Default page size is 100
//   - X-Request-ID header is present in response
func TestListSkills_Success(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a skill so we know at least one exists.
	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Test skill for list success",
		Content:     "# Test Skill\nThis is the content.",
		Version:     "1.0.0",
	}
	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	resp, err := client.Get("/v1/api/skills")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)

	list := helpers.ParseJSON[helpers.SkillList](t, resp)

	if list.Data == nil {
		t.Fatal("expected data array to be non-nil")
	}
	if list.Pagination.Limit <= 0 {
		t.Fatalf("expected pagination.limit > 0, got %d", list.Pagination.Limit)
	}

	// Verify the created skill appears in the list with content.
	found := false
	for _, s := range list.Data {
		if s.Name == name {
			found = true
			if s.Content == "" {
				t.Errorf("expected content to be present for skill %q in list response", name)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected skill %q to appear in list response", name)
	}
}

// TestListSkills_Pagination verifies cursor-based pagination works correctly.
//
// Expected behavior:
//   - limit parameter limits results (1-200, default 100)
//   - cursor parameter enables fetching next page
//   - next_cursor is null when no more pages
//   - has_more indicates if more pages exist
func TestListSkills_Pagination(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create 3 skills to guarantee we have at least enough for a small page.
	for i := 0; i < 3; i++ {
		payload := helpers.SkillCreate{
			Name:        helpers.GenerateUniqueName("skill"),
			Description: fmt.Sprintf("Pagination test skill %d", i),
			Content:     "# Skill Content\nContent body.",
			Version:     "1.0.0",
		}
		resp, err := client.Post("/v1/api/skills", payload)
		if err != nil {
			t.Fatalf("failed to create skill: %v", err)
		}
		helpers.AssertStatusCode(t, resp, http.StatusCreated)
		helpers.ReadBody(t, resp)
	}

	// Request first page with limit=1.
	resp, err := client.Get("/v1/api/skills?limit=1")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?limit=1: %v", err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusOK)

	page1 := helpers.ParseJSON[helpers.SkillList](t, resp)

	if len(page1.Data) > 1 {
		t.Fatalf("expected at most 1 result with limit=1, got %d", len(page1.Data))
	}
	if len(page1.Data) == 0 {
		t.Skip("no skills returned; cannot verify pagination")
	}

	// If there is more than one skill total, has_more should be true.
	if page1.Pagination.HasMore {
		if page1.Pagination.NextCursor == "" {
			t.Fatal("expected next_cursor to be non-empty when has_more is true")
		}

		// Fetch next page using the cursor.
		resp2, err := client.Get("/v1/api/skills?limit=1&cursor=" + page1.Pagination.NextCursor)
		if err != nil {
			t.Fatalf("failed to GET next page: %v", err)
		}
		helpers.AssertStatusCode(t, resp2, http.StatusOK)

		page2 := helpers.ParseJSON[helpers.SkillList](t, resp2)

		if len(page2.Data) == 0 {
			t.Fatal("expected at least one result on second page")
		}

		// Names on page 2 must differ from page 1.
		page1Name := page1.Data[0].Name
		for _, s := range page2.Data {
			if s.Name == page1Name {
				t.Errorf("duplicate skill %q found across pages", page1Name)
			}
		}
	}

	// Exhaust pages and verify the final page has has_more=false and next_cursor empty.
	cursor := page1.Pagination.NextCursor
	hasMore := page1.Pagination.HasMore
	const maxPages = 500
	for i := 0; i < maxPages && hasMore; i++ {
		nextResp, err := client.Get("/v1/api/skills?limit=1&cursor=" + cursor)
		if err != nil {
			t.Fatalf("failed to GET page: %v", err)
		}
		helpers.AssertStatusCode(t, nextResp, http.StatusOK)
		nextPage := helpers.ParseJSON[helpers.SkillList](t, nextResp)
		hasMore = nextPage.Pagination.HasMore
		cursor = nextPage.Pagination.NextCursor
		if !hasMore && cursor != "" {
			t.Errorf("expected next_cursor to be empty when has_more is false")
		}
	}
}

// TestListSkills_PaginationLimitBounds verifies limit parameter boundaries.
//
// Expected behavior:
//   - limit=0 returns 400 Bad Request
//   - limit=201 returns 400 Bad Request
//   - limit=1 returns at most 1 result
//   - limit=200 is accepted
func TestListSkills_PaginationLimitBounds(t *testing.T) {
	client := helpers.NewTestClient(t)

	// limit=0 should return 400.
	resp0, err := client.Get("/v1/api/skills?limit=0")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?limit=0: %v", err)
	}
	helpers.AssertStatusCode(t, resp0, http.StatusBadRequest)
	helpers.ReadBody(t, resp0)

	// limit=201 should return 400.
	resp201, err := client.Get("/v1/api/skills?limit=201")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?limit=201: %v", err)
	}
	helpers.AssertStatusCode(t, resp201, http.StatusBadRequest)
	helpers.ReadBody(t, resp201)

	// limit=1 is accepted and returns at most 1 result.
	resp1, err := client.Get("/v1/api/skills?limit=1")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?limit=1: %v", err)
	}
	helpers.AssertStatusCode(t, resp1, http.StatusOK)
	list1 := helpers.ParseJSON[helpers.SkillList](t, resp1)
	if len(list1.Data) > 1 {
		t.Fatalf("expected at most 1 result with limit=1, got %d", len(list1.Data))
	}

	// limit=200 is accepted.
	resp200, err := client.Get("/v1/api/skills?limit=200")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?limit=200: %v", err)
	}
	helpers.AssertStatusCode(t, resp200, http.StatusOK)
	helpers.ReadBody(t, resp200)
}

// TestListSkills_InvalidCursor verifies invalid cursor handling.
//
// Expected behavior:
//   - Non-base64 cursor returns 400 Bad Request
//   - Expired cursor returns 400 Bad Request
func TestListSkills_InvalidCursor(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Non-base64 cursor (contains characters illegal in base64).
	resp, err := client.Get("/v1/api/skills?cursor=!!!invalid!!!")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?cursor=!!!invalid!!!: %v", err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	helpers.ReadBody(t, resp)

	// A structurally invalid base64 value (valid base64 chars but nonsense payload).
	resp2, err := client.Get("/v1/api/skills?cursor=dGhpcyBpcyBub3QgYSB2YWxpZCBjdXJzb3I=")
	if err != nil {
		t.Fatalf("failed to GET with invalid cursor: %v", err)
	}
	// Either 400 (invalid cursor format) is acceptable.
	if resp2.StatusCode != http.StatusBadRequest && resp2.StatusCode != http.StatusOK {
		body := helpers.ReadBody(t, resp2)
		t.Fatalf("expected 400 or 200 for invalid cursor, got %d: %s", resp2.StatusCode, string(body))
	}
	helpers.ReadBody(t, resp2)
}

// TestListSkills_FilterByTags verifies filtering by comma-separated tags.
//
// Expected behavior:
//   - GET /v1/api/skills?tags=go,testing returns matching skills
//   - Skills must have ALL specified tags (AND logic)
func TestListSkills_FilterByTags(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a skill with both tags.
	bothTagsName := helpers.GenerateUniqueName("skill")
	both := helpers.SkillCreate{
		Name:        bothTagsName,
		Description: "Skill with both tags",
		Content:     "# Both Tags\nContent.",
		Version:     "1.0.0",
		Tags:        []string{"go", "testing"},
	}
	r1, err := client.Post("/v1/api/skills", both)
	if err != nil {
		t.Fatalf("failed to create both-tag skill: %v", err)
	}
	helpers.AssertStatusCode(t, r1, http.StatusCreated)
	helpers.ReadBody(t, r1)

	// Create a skill with only one of the tags.
	oneTagName := helpers.GenerateUniqueName("skill")
	one := helpers.SkillCreate{
		Name:        oneTagName,
		Description: "Skill with one tag",
		Content:     "# One Tag\nContent.",
		Version:     "1.0.0",
		Tags:        []string{"go"},
	}
	r2, err := client.Post("/v1/api/skills", one)
	if err != nil {
		t.Fatalf("failed to create one-tag skill: %v", err)
	}
	helpers.AssertStatusCode(t, r2, http.StatusCreated)
	helpers.ReadBody(t, r2)

	// Create a skill with no matching tags.
	noTagName := helpers.GenerateUniqueName("skill")
	noTag := helpers.SkillCreate{
		Name:        noTagName,
		Description: "Skill with no matching tags",
		Content:     "# No Tags\nContent.",
		Version:     "1.0.0",
		Tags:        []string{"rust"},
	}
	r3, err := client.Post("/v1/api/skills", noTag)
	if err != nil {
		t.Fatalf("failed to create no-tag skill: %v", err)
	}
	helpers.AssertStatusCode(t, r3, http.StatusCreated)
	helpers.ReadBody(t, r3)

	// Filter by both tags (AND logic).
	resp, err := client.Get("/v1/api/skills?tags=go,testing")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?tags=go,testing: %v", err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.SkillList](t, resp)

	// Skill with both tags must appear.
	foundBoth := false
	for _, s := range list.Data {
		if s.Name == bothTagsName {
			foundBoth = true
		}
		// Skill with only one tag must NOT appear (AND filter).
		if s.Name == oneTagName {
			t.Errorf("skill with only tag 'go' should not appear when filtering by 'go,testing'")
		}
		// Skill with no matching tags must NOT appear.
		if s.Name == noTagName {
			t.Errorf("skill with tag 'rust' should not appear when filtering by 'go,testing'")
		}
	}
	if !foundBoth {
		t.Errorf("expected skill %q (with tags go,testing) to appear in filtered results", bothTagsName)
	}
}

// TestListSkills_EmptyResult verifies response when no skills exist.
//
// Expected behavior:
//   - Returns 200 OK
//   - data is empty array
//   - has_more is false
func TestListSkills_EmptyResult(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Use a unique tag that no real skill will have to approximate an empty
	// result without requiring a clean database state.
	uniqueTag := helpers.GenerateUniqueName("emptytag")
	resp, err := client.Get("/v1/api/skills?tags=" + uniqueTag)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills?tags=%s: %v", uniqueTag, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.SkillList](t, resp)

	if list.Data == nil {
		t.Fatal("expected data to be a non-nil array (even if empty)")
	}
	if len(list.Data) != 0 {
		t.Fatalf("expected data to be empty, got %d skills", len(list.Data))
	}
	if list.Pagination.HasMore {
		t.Fatal("expected has_more to be false for empty result")
	}
}

// -----------------------------------------------------------------------------
// Create Skill Tests (POST /v1/api/skills)
// OpenAPI: createSkill
// -----------------------------------------------------------------------------

// TestCreateSkill_Success verifies creating a skill with all required fields.
//
// Expected behavior:
//   - Returns 201 Created
//   - Location header points to /v1/api/skills/{name}
//   - Response body contains the created Skill with timestamps
//   - X-Request-ID header is present
func TestCreateSkill_Success(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	payload := helpers.SkillCreate{
		Name:        name,
		Description: "A skill for testing create success",
		Content:     "# My Skill\nThis is the skill content.",
		Version:     "1.0.0",
	}

	resp, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)
	helpers.AssertRequestIDHeader(t, resp)

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header to be present")
	}
	expectedLocation := "/v1/api/skills/" + name
	if !strings.HasSuffix(location, expectedLocation) {
		t.Errorf("expected Location to end with %q, got %q", expectedLocation, location)
	}

	skill := helpers.ParseJSON[helpers.Skill](t, resp)

	if skill.Name != name {
		t.Errorf("expected name %q, got %q", name, skill.Name)
	}
	if skill.Content == "" {
		t.Error("expected content to be non-empty in created skill response")
	}
	if skill.CreatedAt == "" {
		t.Error("expected created_at to be present")
	}
	if skill.UpdatedAt == "" {
		t.Error("expected updated_at to be present")
	}
}

// TestCreateSkill_AllFields verifies creating a skill with all optional fields.
//
// Expected behavior:
//   - Returns 201 Created
//   - All optional fields (description, tags, license, compatibility,
//     metadata, allowed_tools, version) are persisted and returned
func TestCreateSkill_AllFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	payload := helpers.SkillCreate{
		Name:          name,
		Description:   "Full-featured test skill",
		Content:       "# Full Skill\nAll fields populated.",
		Tags:          []string{"go", "testing", "full"},
		License:       "MIT",
		Compatibility: "Claude Code 1.0+",
		Metadata: map[string]string{
			"author":   "test-team",
			"category": "testing",
		},
		AllowedTools: []string{"Read", "Write", "Bash"},
		Version:      "2.3.1",
	}

	resp, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)

	skill := helpers.ParseJSON[helpers.Skill](t, resp)

	if skill.Name != name {
		t.Errorf("expected name %q, got %q", name, skill.Name)
	}
	if skill.Description != payload.Description {
		t.Errorf("expected description %q, got %q", payload.Description, skill.Description)
	}
	if len(skill.Tags) != len(payload.Tags) {
		t.Errorf("expected %d tags, got %d", len(payload.Tags), len(skill.Tags))
	}
	if skill.License != payload.License {
		t.Errorf("expected license %q, got %q", payload.License, skill.License)
	}
	if skill.Compatibility != payload.Compatibility {
		t.Errorf("expected compatibility %q, got %q", payload.Compatibility, skill.Compatibility)
	}
	if len(skill.AllowedTools) != len(payload.AllowedTools) {
		t.Errorf("expected %d allowed_tools, got %d", len(payload.AllowedTools), len(skill.AllowedTools))
	}
	if skill.Version != payload.Version {
		t.Errorf("expected version %q, got %q", payload.Version, skill.Version)
	}
	if skill.Metadata["author"] != "test-team" {
		t.Errorf("expected metadata.author %q, got %q", "test-team", skill.Metadata["author"])
	}
}

// TestCreateSkill_MinimalFields verifies creating a skill with only required fields.
//
// Expected behavior:
//   - Returns 201 Created
//   - Only name and content are required
//   - Optional fields are absent or default in response
func TestCreateSkill_MinimalFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	// Per OpenAPI spec, SkillCreate requires: name, description, content, version.
	payload := helpers.SkillCreate{
		Name:        name,
		Description: "Minimal required description",
		Content:     "# Minimal Skill\nJust the content.",
		Version:     "1.0.0",
	}

	resp, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)

	skill := helpers.ParseJSON[helpers.Skill](t, resp)

	if skill.Name != name {
		t.Errorf("expected name %q, got %q", name, skill.Name)
	}
	if skill.Content != payload.Content {
		t.Errorf("expected content to match, got %q", skill.Content)
	}
	// Optional fields should be empty/absent.
	if len(skill.Tags) != 0 {
		t.Errorf("expected no tags, got %v", skill.Tags)
	}
	if len(skill.AllowedTools) != 0 {
		t.Errorf("expected no allowed_tools, got %v", skill.AllowedTools)
	}
}

// TestCreateSkill_DuplicateName verifies 409 when skill name already exists.
//
// Expected behavior:
//   - Returns 409 Conflict
//   - Response is RFC 7807 Problem Details format
func TestCreateSkill_DuplicateName(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	payload := helpers.SkillCreate{
		Name:        name,
		Description: "Duplicate name test",
		Content:     "# Skill Content\nBody.",
		Version:     "1.0.0",
	}

	// Create once — should succeed.
	r1, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed first POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, r1, http.StatusCreated)
	helpers.ReadBody(t, r1)

	// Create again with the same name — should conflict.
	r2, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed second POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, r2, http.StatusConflict)

	errResp := helpers.ParseJSON[helpers.ErrorResponse](t, r2)

	if errResp.Status != http.StatusConflict {
		t.Errorf("expected error status 409, got %d", errResp.Status)
	}
	if errResp.Title == "" {
		t.Error("expected non-empty title in error response")
	}
}

// TestCreateSkill_ValidationErrors verifies 400 for invalid input fields.
//
// Expected behavior:
//   - Missing name returns 400 with field error for "name"
//   - Missing content returns 400 with field error for "content"
//   - Name exceeding 64 chars returns 400
//   - Name with uppercase letters returns 400
//   - Name starting with number returns 400
//   - Name with underscores returns 400
//   - Content exceeding 524288 bytes returns 400
//   - Description exceeding 1024 chars returns 400
//   - More than 20 tags returns 400
//   - License exceeding 255 chars returns 400
//   - Compatibility exceeding 500 chars returns 400
//   - More than 50 allowed_tools returns 400
//   - Version exceeding 50 chars returns 400
func TestCreateSkill_ValidationErrors(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Build a 21-element tags slice.
	tooManyTags := make([]string, 21)
	for i := range tooManyTags {
		tooManyTags[i] = fmt.Sprintf("tag%d", i)
	}

	// Build a 51-element allowed_tools slice.
	tooManyTools := make([]string, 51)
	for i := range tooManyTools {
		tooManyTools[i] = fmt.Sprintf("Tool%d", i)
	}

	tests := []struct {
		name        string
		payload     helpers.SkillCreate
		expectField string
	}{
		{
			name: "missing name",
			payload: helpers.SkillCreate{
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "name",
		},
		{
			name: "missing content",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: "desc",
				Version:     "1.0.0",
			},
			expectField: "content",
		},
		{
			name: "name too long",
			payload: helpers.SkillCreate{
				Name:        stringOfLen("a", 65),
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "name",
		},
		{
			name: "name uppercase",
			payload: helpers.SkillCreate{
				Name:        "MySkill",
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "name",
		},
		{
			name: "name starts with number",
			payload: helpers.SkillCreate{
				Name:        "1-skill",
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "name",
		},
		{
			name: "name with underscore",
			payload: helpers.SkillCreate{
				Name:        "my_skill",
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "name",
		},
		{
			name: "content too large",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: "desc",
				Content:     stringOfLen("x", 524289),
				Version:     "1.0.0",
			},
			expectField: "content",
		},
		{
			name: "description too long",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: stringOfLen("d", 1025),
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "description",
		},
		{
			name: "too many tags",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
				Tags:        tooManyTags,
			},
			expectField: "tags",
		},
		{
			name: "license too long",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: "desc",
				Content:     "content",
				Version:     "1.0.0",
				License:     stringOfLen("L", 256),
			},
			expectField: "license",
		},
		{
			name: "compatibility too long",
			payload: helpers.SkillCreate{
				Name:          helpers.GenerateUniqueName("skill"),
				Description:   "desc",
				Content:       "content",
				Version:       "1.0.0",
				Compatibility: stringOfLen("C", 501),
			},
			expectField: "compatibility",
		},
		{
			name: "too many allowed_tools",
			payload: helpers.SkillCreate{
				Name:         helpers.GenerateUniqueName("skill"),
				Description:  "desc",
				Content:      "content",
				Version:      "1.0.0",
				AllowedTools: tooManyTools,
			},
			expectField: "allowed_tools",
		},
		{
			name: "version too long",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill"),
				Description: "desc",
				Content:     "content",
				Version:     stringOfLen("v", 51),
			},
			expectField: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Post("/v1/api/skills", tt.payload)
			if err != nil {
				t.Fatalf("failed to POST /v1/api/skills: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			if errResp.Status != http.StatusBadRequest {
				t.Errorf("expected error status 400, got %d", errResp.Status)
			}

			if tt.expectField != "" {
				found := false
				for _, fe := range errResp.Errors {
					if fe.Field == tt.expectField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for %q, got errors: %v", tt.expectField, errResp.Errors)
				}
			}
		})
	}
}

// TestCreateSkill_InvalidJSON verifies 400 for malformed JSON.
//
// Expected behavior:
//   - Returns 400 Bad Request
//   - Response is RFC 7807 Problem Details format
func TestCreateSkill_InvalidJSON(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/skills", bytes.NewBufferString(`{not valid json`))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills with invalid JSON: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

	errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
	if errResp.Status != http.StatusBadRequest {
		t.Errorf("expected error status 400, got %d", errResp.Status)
	}
	if errResp.Title == "" {
		t.Error("expected non-empty title in error response")
	}
}

// TestCreateSkill_EmptyBody verifies 400 for empty request body.
//
// Expected behavior:
//   - Returns 400 Bad Request
func TestCreateSkill_EmptyBody(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/skills", bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills with empty body: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	helpers.ReadBody(t, resp)
}

// TestCreateSkill_NameFormat validates the name pattern ^[a-z]([a-z0-9](-[a-z0-9])*)*$.
//
// Expected behavior:
//   - Valid names: "my-skill", "a", "abc-123", "skill-v2-beta"
//   - Invalid names: "My-Skill", "123-abc", "-leading-hyphen", "trailing-", "a--b"
func TestCreateSkill_NameFormat(t *testing.T) {
	client := helpers.NewTestClient(t)

	validContent := "# Skill\nContent."
	validDesc := "Test description"
	validVersion := "1.0.0"

	validNames := []struct {
		name    string
		payload helpers.SkillCreate
	}{
		{
			name: "single letter",
			payload: helpers.SkillCreate{
				Name:        "a",
				Description: validDesc,
				Content:     validContent,
				Version:     validVersion,
			},
		},
		{
			name: "hyphenated",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("my-skill"),
				Description: validDesc,
				Content:     validContent,
				Version:     validVersion,
			},
		},
		{
			name: "alphanumeric with hyphen",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("abc-123"),
				Description: validDesc,
				Content:     validContent,
				Version:     validVersion,
			},
		},
		{
			name: "multiple segments",
			payload: helpers.SkillCreate{
				Name:        helpers.GenerateUniqueName("skill-v2-beta"),
				Description: validDesc,
				Content:     validContent,
				Version:     validVersion,
			},
		},
	}

	for _, tt := range validNames {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			resp, err := client.Post("/v1/api/skills", tt.payload)
			if err != nil {
				t.Fatalf("failed to POST: %v", err)
			}
			// 201 = created successfully; 409 = name collision (still a valid name format).
			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
				body := helpers.ReadBody(t, resp)
				t.Errorf("expected 201 or 409 for valid name %q, got %d: %s", tt.payload.Name, resp.StatusCode, string(body))
			} else {
				helpers.ReadBody(t, resp)
			}
		})
	}

	invalidNames := []struct {
		name      string
		skillName string
	}{
		{name: "uppercase", skillName: "My-Skill"},
		{name: "starts with number", skillName: "123-abc"},
		{name: "leading hyphen", skillName: "-leading-hyphen"},
		{name: "trailing hyphen", skillName: "trailing-"},
		{name: "consecutive hyphens", skillName: "a--b"},
	}

	for _, tt := range invalidNames {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			payload := helpers.SkillCreate{
				Name:        tt.skillName,
				Description: validDesc,
				Content:     validContent,
				Version:     validVersion,
			}
			resp, err := client.Post("/v1/api/skills", payload)
			if err != nil {
				t.Fatalf("failed to POST: %v", err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
			helpers.ReadBody(t, resp)
		})
	}
}

// -----------------------------------------------------------------------------
// Get Skill Tests (GET /v1/api/skills/{name})
// OpenAPI: getSkill
// -----------------------------------------------------------------------------

// TestGetSkill_Success verifies retrieving a skill by name.
//
// Expected behavior:
//   - Returns 200 OK
//   - Response body contains the full Skill including content
//   - X-Request-ID header is present
func TestGetSkill_Success(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	payload := helpers.SkillCreate{
		Name:        name,
		Description: "Get skill test",
		Content:     "# Get Skill\nFull content here.",
		Version:     "1.0.0",
	}

	createResp, err := client.Post("/v1/api/skills", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	resp, err := client.Get("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills/%s: %v", name, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)

	skill := helpers.ParseJSON[helpers.Skill](t, resp)

	if skill.Name != name {
		t.Errorf("expected name %q, got %q", name, skill.Name)
	}
	if skill.Content == "" {
		t.Error("expected content to be non-empty")
	}
	if skill.Content != payload.Content {
		t.Errorf("expected content %q, got %q", payload.Content, skill.Content)
	}
}

// TestGetSkill_NotFound verifies 404 for non-existent skill name.
//
// Expected behavior:
//   - Returns 404 Not Found
//   - Response is RFC 7807 Problem Details format
func TestGetSkill_NotFound(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/skills/nonexistent-skill-that-does-not-exist")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills/nonexistent-skill-that-does-not-exist: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)

	errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
	if errResp.Status != http.StatusNotFound {
		t.Errorf("expected error status 404, got %d", errResp.Status)
	}
	if errResp.Title == "" {
		t.Error("expected non-empty title in error response")
	}
}

// TestGetSkill_InvalidNameFormat verifies 400 for invalid name path parameter.
//
// Expected behavior:
//   - Uppercase name returns 400 or 404
//   - Name starting with number returns 400 or 404
func TestGetSkill_InvalidNameFormat(t *testing.T) {
	client := helpers.NewTestClient(t)

	tests := []struct {
		name      string
		skillName string
	}{
		{name: "uppercase name", skillName: "Invalid-Skill-Name"},
		{name: "starts with number", skillName: "1-invalid-skill"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get("/v1/api/skills/" + tt.skillName)
			if err != nil {
				t.Fatalf("failed to GET /v1/api/skills/%s: %v", tt.skillName, err)
			}
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				body := helpers.ReadBody(t, resp)
				t.Errorf("expected 400 or 404 for invalid name %q, got %d: %s", tt.skillName, resp.StatusCode, string(body))
			} else {
				helpers.ReadBody(t, resp)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Update Skill Tests (PUT /v1/api/skills/{name})
// OpenAPI: updateSkill
// -----------------------------------------------------------------------------

// TestUpdateSkill_Success verifies updating an existing skill.
//
// Expected behavior:
//   - Returns 204 No Content
func TestUpdateSkill_Success(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Original description",
		Content:     "# Original Content\nOriginal body.",
		Version:     "1.0.0",
	}

	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	updatePayload := helpers.SkillUpdate{
		Name:        name,
		Description: "Updated description",
		Content:     "# Updated Content\nNew body.",
		Version:     "2.0.0",
	}

	resp, err := client.Put("/v1/api/skills/"+name, updatePayload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/skills/%s: %v", name, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.AssertRequestIDHeader(t, resp)
	helpers.ReadBody(t, resp)
}

// TestUpdateSkill_FullReplacement verifies PUT is full replacement, not PATCH.
//
// Expected behavior:
//   - Omitted optional fields are cleared (not preserved from previous state)
//   - Only name and content are retained if only they are provided
func TestUpdateSkill_FullReplacement(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:          name,
		Description:   "Has optional fields",
		Content:       "# Content\nBody.",
		Version:       "1.0.0",
		Tags:          []string{"go", "testing"},
		License:       "MIT",
		Compatibility: "Claude Code 1.0+",
		AllowedTools:  []string{"Read", "Write"},
	}

	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	// PUT with only required fields — optional fields should be cleared.
	updatePayload := helpers.SkillUpdate{
		Name:        name,
		Description: "Replacement description",
		Content:     "# Replacement\nNew body.",
		Version:     "2.0.0",
	}

	resp, err := client.Put("/v1/api/skills/"+name, updatePayload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/skills/%s: %v", name, err)
	}
	helpers.AssertStatusCode(t, resp, http.StatusNoContent)
	helpers.ReadBody(t, resp)
}

// TestUpdateSkill_NotFound verifies 404 when updating a non-existent skill.
//
// Expected behavior:
//   - Returns 404 Not Found
//   - Response is RFC 7807 Problem Details format
func TestUpdateSkill_NotFound(t *testing.T) {
	client := helpers.NewTestClient(t)

	payload := helpers.SkillUpdate{
		Name:        "does-not-exist-skill",
		Description: "Will not be created",
		Content:     "# Not found\nContent.",
		Version:     "1.0.0",
	}

	resp, err := client.Put("/v1/api/skills/does-not-exist-skill", payload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/skills/does-not-exist-skill: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)

	errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
	if errResp.Status != http.StatusNotFound {
		t.Errorf("expected error status 404, got %d", errResp.Status)
	}
}

// TestUpdateSkill_ValidationErrors verifies 400 for invalid update input.
//
// Expected behavior:
//   - Same validation rules as create (name pattern, content size, etc.)
//   - Name in body must match path parameter (or be omitted)
func TestUpdateSkill_ValidationErrors(t *testing.T) {
	client := helpers.NewTestClient(t)

	// First create a skill to update against.
	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Validation test skill",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}
	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	tests := []struct {
		name        string
		payload     helpers.SkillUpdate
		expectField string
	}{
		{
			name: "missing content",
			payload: helpers.SkillUpdate{
				Name:        name,
				Description: "desc",
				Version:     "1.0.0",
			},
			expectField: "content",
		},
		{
			name: "content too large",
			payload: helpers.SkillUpdate{
				Name:        name,
				Description: "desc",
				Content:     stringOfLen("x", 524289),
				Version:     "1.0.0",
			},
			expectField: "content",
		},
		{
			name: "description too long",
			payload: helpers.SkillUpdate{
				Name:        name,
				Description: stringOfLen("d", 1025),
				Content:     "content",
				Version:     "1.0.0",
			},
			expectField: "description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Put("/v1/api/skills/"+name, tt.payload)
			if err != nil {
				t.Fatalf("failed to PUT /v1/api/skills/%s: %v", name, err)
			}
			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
			if errResp.Status != http.StatusBadRequest {
				t.Errorf("expected error status 400, got %d", errResp.Status)
			}

			if tt.expectField != "" {
				found := false
				for _, fe := range errResp.Errors {
					if fe.Field == tt.expectField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected field error for %q, got errors: %v", tt.expectField, errResp.Errors)
				}
			}
		})
	}
}

// TestUpdateSkill_NameMismatch verifies 400 when body name differs from path name.
//
// Expected behavior:
//   - Returns 400 Bad Request
func TestUpdateSkill_NameMismatch(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create a skill to update.
	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Mismatch test skill",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}
	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	// PUT with a different name in the body.
	payload := helpers.SkillUpdate{
		Name:        "a-different-name",
		Description: "Mismatch body",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}

	resp, err := client.Put("/v1/api/skills/"+name, payload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/skills/%s: %v", name, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	helpers.ReadBody(t, resp)
}

// -----------------------------------------------------------------------------
// Delete Skill Tests (DELETE /v1/api/skills/{name})
// OpenAPI: deleteSkill
// -----------------------------------------------------------------------------

// TestDeleteSkill_Success verifies deleting an existing skill.
//
// Expected behavior:
//   - Returns 204 No Content
//   - Skill is no longer retrievable via GET
//   - Associated files (scripts, references, assets) are cascade-deleted
func TestDeleteSkill_Success(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Delete success test skill",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}

	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	deleteResp, err := client.Delete("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/skills/%s: %v", name, err)
	}
	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)
	helpers.ReadBody(t, deleteResp)

	// Verify skill is no longer accessible.
	getResp, err := client.Get("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills/%s after delete: %v", name, err)
	}
	helpers.AssertStatusCode(t, getResp, http.StatusNotFound)
	helpers.ReadBody(t, getResp)
}

// TestDeleteSkill_NotFound verifies 404 when deleting a non-existent skill.
//
// Expected behavior:
//   - Returns 404 Not Found
//   - Response is RFC 7807 Problem Details format
func TestDeleteSkill_NotFound(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Delete("/v1/api/skills/nonexistent-skill-xyz-999")
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/skills/nonexistent-skill-xyz-999: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)

	errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)
	if errResp.Status != http.StatusNotFound {
		t.Errorf("expected error status 404, got %d", errResp.Status)
	}
	if errResp.Title == "" {
		t.Error("expected non-empty title in error response")
	}
}

// TestDeleteSkill_Idempotent verifies second DELETE returns 404.
//
// Expected behavior:
//   - First DELETE returns 204
//   - Second DELETE of same name returns 404
func TestDeleteSkill_Idempotent(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Idempotent delete test skill",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}

	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	// First DELETE — should return 204.
	del1, err := client.Delete("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed first DELETE /v1/api/skills/%s: %v", name, err)
	}
	helpers.AssertStatusCode(t, del1, http.StatusNoContent)
	helpers.ReadBody(t, del1)

	// Second DELETE — should return 404.
	del2, err := client.Delete("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed second DELETE /v1/api/skills/%s: %v", name, err)
	}
	helpers.AssertStatusCode(t, del2, http.StatusNotFound)
	helpers.ReadBody(t, del2)
}

// TestDeleteSkill_CascadesFiles verifies associated files are deleted when skill is deleted.
//
// Expected behavior:
//   - Create a skill with scripts, references, and assets
//   - DELETE the skill returns 204
//   - GET /v1/api/skills/{name}/scripts returns 404 (skill not found)
func TestDeleteSkill_CascadesFiles(t *testing.T) {
	client := helpers.NewTestClient(t)

	name := helpers.GenerateUniqueName("skill")
	createPayload := helpers.SkillCreate{
		Name:        name,
		Description: "Cascade delete test skill",
		Content:     "# Content\nBody.",
		Version:     "1.0.0",
	}

	createResp, err := client.Post("/v1/api/skills", createPayload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/skills: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	// Upload a script file to the skill.
	scriptPayload := helpers.SkillFileCreate{
		Filename:    "test-script.sh",
		ContentType: "text/x-shellscript",
		Content:     "#!/bin/sh\necho hello",
		Encoding:    "utf-8",
	}
	scriptResp, err := client.Post("/v1/api/skills/"+name+"/scripts", scriptPayload)
	if err != nil {
		t.Fatalf("failed to POST script to skill: %v", err)
	}
	helpers.AssertStatusCode(t, scriptResp, http.StatusCreated)
	helpers.ReadBody(t, scriptResp)

	// Verify the script is accessible.
	listResp, err := client.Get("/v1/api/skills/" + name + "/scripts")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills/%s/scripts: %v", name, err)
	}
	helpers.AssertStatusCode(t, listResp, http.StatusOK)
	helpers.ReadBody(t, listResp)

	// Delete the skill.
	deleteResp, err := client.Delete("/v1/api/skills/" + name)
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/skills/%s: %v", name, err)
	}
	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)
	helpers.ReadBody(t, deleteResp)

	// After deletion, the scripts endpoint should return 404 (skill not found).
	scriptsAfterDelete, err := client.Get("/v1/api/skills/" + name + "/scripts")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/skills/%s/scripts after delete: %v", name, err)
	}
	helpers.AssertStatusCode(t, scriptsAfterDelete, http.StatusNotFound)
	helpers.ReadBody(t, scriptsAfterDelete)
}
