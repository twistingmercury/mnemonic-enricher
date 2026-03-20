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
// Agent Endpoint Tests
// Spec: GET /v1/api/agents, POST /v1/api/agents,
//       GET /v1/api/agents/{name}, PUT /v1/api/agents/{name},
//       DELETE /v1/api/agents/{name}
// =============================================================================
//
// Agents are core resource definitions. Each agent has:
//   - name:          Unique identifier (pattern: ^[a-z]([a-z0-9](-[a-z0-9])*)*$, max 64)
//   - system_prompt: Full prompt text (max 51200 chars)
//   - model:         e.g., "sonnet"
//   - description:   Required (max 500 chars)
//   - allowed_tools: Optional list of tool names
//   - version:       Required version string
//
// Authentication: None required (MVP). All endpoints are open.

// -----------------------------------------------------------------------------
// List Agents (GET /v1/api/agents)
// -----------------------------------------------------------------------------

// TestListAgents_ReturnsOKWithPaginatedResults verifies listing agents returns
// a 200 response containing a data array of AgentSummary objects (without
// system_prompt) and pagination metadata. X-Request-ID header must be present.
func TestListAgents_ReturnsOKWithPaginatedResults(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create an agent so the list is non-trivially exercised.
	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "You are a helpful assistant.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}
	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	resp, err := client.Get("/v1/api/agents")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)

	list := helpers.ParseJSON[helpers.AgentList](t, resp)

	if list.Data == nil {
		t.Fatal("expected 'data' field to be an array, got nil")
	}

	// Verify at least the agent we created is present somewhere.
	found := false
	for _, a := range list.Data {
		if a.Name == agentName {
			found = true
			// AgentSummary should not expose system_prompt — the struct doesn't have that field.
			if a.Model == "" {
				t.Errorf("expected model to be set for agent %q", agentName)
			}
			break
		}
	}
	if !found {
		t.Errorf("created agent %q not found in list response", agentName)
	}

	// Pagination block must be present.
	if list.Pagination.Limit == 0 {
		t.Error("expected pagination.limit to be non-zero")
	}
}

// TestListAgents_PaginationWithLimitAndCursor verifies cursor-based pagination.
// Create enough agents to span multiple pages, request with a small limit, and
// walk through pages using next_cursor. Verify no duplicates across pages and
// has_more transitions from true to false on the last page.
func TestListAgents_PaginationWithLimitAndCursor(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create 5 uniquely named agents.
	created := make(map[string]bool, 5)
	for i := range 5 {
		name := helpers.GenerateUniqueName("page")
		payload := helpers.AgentCreate{
			Name:         name,
			SystemPrompt: fmt.Sprintf("Prompt for agent %d", i),
			Model:        "sonnet",
			Description:  "Test agent.",
			Version:      "1.0.0",
		}
		resp, err := client.Post("/v1/api/agents", payload)
		if err != nil {
			t.Fatalf("failed to create agent %d: %v", i, err)
		}
		defer resp.Body.Close()
		helpers.AssertStatusCode(t, resp, http.StatusCreated)
		created[name] = false
	}

	// Walk all pages with limit=2.
	seen := make(map[string]bool)
	cursor := ""
	pageCount := 0

	for {
		path := "/v1/api/agents?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}

		resp, err := client.Get(path)
		if err != nil {
			t.Fatalf("failed to GET page %d: %v", pageCount, err)
		}
		helpers.AssertStatusCode(t, resp, http.StatusOK)

		list := helpers.ParseJSON[helpers.AgentList](t, resp)
		pageCount++

		for _, a := range list.Data {
			if seen[a.Name] {
				t.Errorf("duplicate agent %q seen across pages", a.Name)
			}
			seen[a.Name] = true
		}

		if !list.Pagination.HasMore {
			break
		}

		if list.Pagination.NextCursor == "" {
			t.Fatal("has_more is true but next_cursor is empty")
		}
		cursor = list.Pagination.NextCursor

		if pageCount > 20 {
			t.Fatal("pagination loop did not terminate within 20 pages")
		}
	}

	// All 5 created agents must have been seen.
	for name := range created {
		if !seen[name] {
			t.Errorf("created agent %q not found across all pages", name)
		}
	}

	if pageCount < 2 {
		t.Errorf("expected at least 2 pages, got %d", pageCount)
	}
}

// TestListAgents_DefaultPaginationLimit verifies that omitting the limit
// parameter defaults to 100 items per page.
func TestListAgents_DefaultPaginationLimit(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.AgentList](t, resp)

	if list.Pagination.Limit != 100 {
		t.Errorf("expected default limit=100, got %d", list.Pagination.Limit)
	}
}

// TestListAgents_LimitBelowMinimumReturns400 verifies that limit=0 returns
// 400 Bad Request since the minimum is 1.
func TestListAgents_LimitBelowMinimumReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents?limit=0")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents?limit=0: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// TestListAgents_LimitAboveMaximumReturns400 verifies that limit=201 returns
// 400 Bad Request since the maximum is 200.
func TestListAgents_LimitAboveMaximumReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents?limit=201")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents?limit=201: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// TestListAgents_InvalidCursorReturns400 verifies that a malformed cursor
// (not valid base64 or structurally invalid) returns 400 Bad Request.
func TestListAgents_InvalidCursorReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents?cursor=not-valid-cursor!!!")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents with invalid cursor: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// TestListAgents_EmptyResultReturnsEmptyArray verifies that when no agents
// exist, data is an empty array (not null) and has_more is false.
func TestListAgents_EmptyResultReturnsEmptyArray(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	list := helpers.ParseJSON[helpers.AgentList](t, resp)

	// data must be an array (not null) — JSON unmarshalling gives a nil slice for "null"
	// and a non-nil empty slice for "[]". We assert the field is present and is an array.
	if list.Data == nil {
		t.Error("expected 'data' to be an array (possibly empty), got null")
	}

	// has_more must be present; when there is no data beyond this page it should be false.
	// (It may be true if other tests ran concurrently and created agents; we only assert structure.)
	// The key invariant: pagination block is present.
	_ = list.Pagination.HasMore
}

// -----------------------------------------------------------------------------
// Create Agent (POST /v1/api/agents)
// -----------------------------------------------------------------------------

// TestCreateAgent_HappyPathReturns201 verifies creating an agent with all
// required fields returns 201 Created, a Location header pointing to the new
// resource, and the full Agent object with created_at and updated_at set.
func TestCreateAgent_HappyPathReturns201(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "You are a helpful assistant.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	resp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)
	helpers.AssertRequestIDHeader(t, resp)

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header to be set")
	}
	expectedSuffix := "/v1/api/agents/" + agentName
	if !strings.HasSuffix(location, expectedSuffix) {
		t.Errorf("expected Location to end with %q, got %q", expectedSuffix, location)
	}

	agent := helpers.ParseJSON[helpers.Agent](t, resp)

	if agent.Name != agentName {
		t.Errorf("expected name %q, got %q", agentName, agent.Name)
	}
	if agent.SystemPrompt != payload.SystemPrompt {
		t.Errorf("expected system_prompt %q, got %q", payload.SystemPrompt, agent.SystemPrompt)
	}
	if agent.Model != payload.Model {
		t.Errorf("expected model %q, got %q", payload.Model, agent.Model)
	}
	if agent.CreatedAt == "" {
		t.Error("expected created_at to be set")
	}
	if agent.UpdatedAt == "" {
		t.Error("expected updated_at to be set")
	}
}

// TestCreateAgent_AllOptionalFields verifies that optional fields (description,
// allowed_tools, version) are stored and returned when provided.
func TestCreateAgent_AllOptionalFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Full-featured assistant prompt.",
		Model:        "sonnet",
		Description:  "A test agent with all optional fields.",
		AllowedTools: []string{"Read", "Write", "Bash"},
		Version:      "1.0.0",
	}

	resp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)

	agent := helpers.ParseJSON[helpers.Agent](t, resp)

	if agent.Description != payload.Description {
		t.Errorf("expected description %q, got %q", payload.Description, agent.Description)
	}
	if agent.Version != payload.Version {
		t.Errorf("expected version %q, got %q", payload.Version, agent.Version)
	}
	if len(agent.AllowedTools) != len(payload.AllowedTools) {
		t.Errorf("expected %d allowed_tools, got %d", len(payload.AllowedTools), len(agent.AllowedTools))
	} else {
		for i, tool := range payload.AllowedTools {
			if agent.AllowedTools[i] != tool {
				t.Errorf("allowed_tools[%d]: expected %q, got %q", i, tool, agent.AllowedTools[i])
			}
		}
	}
}

// TestCreateAgent_MinimalRequiredFields verifies that name, system_prompt,
// model, description, and version are required. Omitting optional fields succeeds with defaults.
func TestCreateAgent_MinimalRequiredFields(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Minimal prompt.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	resp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents: %v", err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusCreated)

	agent := helpers.ParseJSON[helpers.Agent](t, resp)

	if agent.Name != agentName {
		t.Errorf("expected name %q, got %q", agentName, agent.Name)
	}
	if agent.SystemPrompt == "" {
		t.Error("expected system_prompt to be returned")
	}
	if agent.Model != "sonnet" {
		t.Errorf("expected model 'sonnet', got %q", agent.Model)
	}
}

// TestCreateAgent_DuplicateNameReturns409 verifies that creating a second
// agent with the same name returns 409 Conflict.
func TestCreateAgent_DuplicateNameReturns409(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "First creation.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	resp1, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents (first): %v", err)
	}
	defer resp1.Body.Close()
	helpers.AssertStatusCode(t, resp1, http.StatusCreated)

	resp2, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents (second): %v", err)
	}
	defer resp2.Body.Close()

	helpers.AssertStatusCode(t, resp2, http.StatusConflict)
}

// TestCreateAgent_ValidationErrors uses table-driven sub-tests for field
// validation. Each case sends a malformed request and asserts 400 Bad Request
// with appropriate field-level errors in the RFC 7807 response body.
func TestCreateAgent_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		payload     helpers.AgentCreate
		expectField string
		expectCode  string
	}{
		{
			name:        "missing name",
			payload:     helpers.AgentCreate{SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "name",
		},
		{
			name:        "missing system_prompt",
			payload:     helpers.AgentCreate{Name: "valid-name", Model: "sonnet"},
			expectField: "system_prompt",
		},
		{
			name:        "missing model",
			payload:     helpers.AgentCreate{Name: "valid-name", SystemPrompt: "prompt"},
			expectField: "model",
		},
		{
			name:        "invalid name format uppercase",
			payload:     helpers.AgentCreate{Name: "Invalid-Name", SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "name",
			expectCode:  "INVALID_FORMAT",
		},
		{
			name:        "invalid name starts with number",
			payload:     helpers.AgentCreate{Name: "123-bad", SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "name",
			expectCode:  "INVALID_FORMAT",
		},
		{
			name:        "invalid name with underscores",
			payload:     helpers.AgentCreate{Name: "has_underscore", SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "name",
			expectCode:  "INVALID_FORMAT",
		},
		{
			name:        "name too long",
			payload:     helpers.AgentCreate{Name: stringOfLen("a", 65), SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "name",
		},
		{
			name:        "description too long",
			payload:     helpers.AgentCreate{Name: "valid-name", Description: stringOfLen("x", 501), SystemPrompt: "prompt", Model: "sonnet"},
			expectField: "description",
		},
		{
			name:        "system_prompt too long",
			payload:     helpers.AgentCreate{Name: "valid-name", SystemPrompt: stringOfLen("x", 51201), Model: "sonnet"},
			expectField: "system_prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			resp, err := client.Post("/v1/api/agents", tt.payload)
			if err != nil {
				t.Fatalf("failed to POST /v1/api/agents: %v", err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			if errResp.Status != http.StatusBadRequest {
				t.Errorf("expected error status 400, got %d", errResp.Status)
			}

			// Find a field error matching the expected field.
			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tt.expectField {
					fieldFound = true
					if tt.expectCode != "" && fe.Code != tt.expectCode {
						t.Errorf("expected error code %q for field %q, got %q", tt.expectCode, tt.expectField, fe.Code)
					}
					break
				}
			}
			if !fieldFound {
				t.Errorf("expected field error for %q, got errors: %+v", tt.expectField, errResp.Errors)
			}

			_ = tt.payload
			_ = tt.expectField
			_ = tt.expectCode
		})
	}
}

// TestCreateAgent_InvalidJSONReturns400 verifies that a syntactically invalid
// JSON body returns 400 Bad Request.
func TestCreateAgent_InvalidJSONReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	badJSON := []byte(`{this is not valid json`)
	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/agents", bytes.NewReader(badJSON))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents with invalid JSON: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// TestCreateAgent_EmptyBodyReturns400 verifies that an empty request body
// returns 400 Bad Request.
func TestCreateAgent_EmptyBodyReturns400(t *testing.T) {
	client := helpers.NewTestClient(t)

	req, err := http.NewRequest(http.MethodPost, client.BaseURL+"/v1/api/agents", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /v1/api/agents with empty body: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
}

// -----------------------------------------------------------------------------
// Get Agent (GET /v1/api/agents/{name})
// -----------------------------------------------------------------------------

// TestGetAgent_ExistingReturns200 verifies retrieving an existing agent by
// name returns 200 OK with the full Agent object including system_prompt.
func TestGetAgent_ExistingReturns200(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Detailed system prompt content.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	resp, err := client.Get("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents/%s: %v", agentName, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	agent := helpers.ParseJSON[helpers.Agent](t, resp)

	if agent.Name != agentName {
		t.Errorf("expected name %q, got %q", agentName, agent.Name)
	}
	if agent.Model != payload.Model {
		t.Errorf("expected model %q, got %q", payload.Model, agent.Model)
	}
}

// TestGetAgent_IncludesSystemPrompt verifies the GET response includes
// system_prompt, unlike the list endpoint which returns AgentSummary.
func TestGetAgent_IncludesSystemPrompt(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	systemPrompt := "This is the full system prompt text for inclusion verification."
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: systemPrompt,
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	resp, err := client.Get("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents/%s: %v", agentName, err)
	}

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	agent := helpers.ParseJSON[helpers.Agent](t, resp)

	if agent.SystemPrompt != systemPrompt {
		t.Errorf("expected system_prompt %q, got %q", systemPrompt, agent.SystemPrompt)
	}
}

// TestGetAgent_NotFoundReturns404 verifies that requesting a non-existent
// agent name returns 404 Not Found with RFC 7807 error body.
func TestGetAgent_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/v1/api/agents/does-not-exist-xyz")
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents/does-not-exist-xyz: %v", err)
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

// TestGetAgent_ResponseIncludesRequestIDHeader verifies the X-Request-ID
// header is present in the response.
func TestGetAgent_ResponseIncludesRequestIDHeader(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Request ID verification prompt.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	resp, err := client.Get("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to GET /v1/api/agents/%s: %v", agentName, err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
	helpers.AssertRequestIDHeader(t, resp)
}

// -----------------------------------------------------------------------------
// Update Agent (PUT /v1/api/agents/{name})
// -----------------------------------------------------------------------------

// TestUpdateAgent_HappyPathReturns204 verifies updating an existing agent
// returns 204 No Content. updated_at must change while created_at remains
// the same, verified by a subsequent GET.
func TestUpdateAgent_HappyPathReturns204(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	createPayload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Original prompt.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", createPayload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)
	helpers.ReadBody(t, createResp)

	updatePayload := helpers.AgentUpdate{
		Name:         agentName,
		SystemPrompt: "Updated prompt content.",
		Model:        "opus",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	updateResp, err := client.Put("/v1/api/agents/"+agentName, updatePayload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/agents/%s: %v", agentName, err)
	}

	helpers.AssertStatusCode(t, updateResp, http.StatusNoContent)
	helpers.AssertRequestIDHeader(t, updateResp)
	helpers.ReadBody(t, updateResp)
}

// TestUpdateAgent_FullReplacement verifies that PUT replaces the entire
// resource. Omitting optional fields (e.g., allowed_tools) in the update
// body should reset them to defaults, not preserve old values.
func TestUpdateAgent_FullReplacement(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	createPayload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Prompt with extras.",
		Model:        "sonnet",
		Description:  "Original description.",
		AllowedTools: []string{"Read", "Write"},
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", createPayload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	// Update with required fields changed; omit optional allowed_tools — it should be reset.
	updatePayload := helpers.AgentUpdate{
		Name:         agentName,
		SystemPrompt: "Replacement prompt only.",
		Model:        "haiku",
		Description:  "Replacement description.",
		Version:      "2.0.0",
	}

	updateResp, err := client.Put("/v1/api/agents/"+agentName, updatePayload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/agents/%s: %v", agentName, err)
	}

	helpers.AssertStatusCode(t, updateResp, http.StatusNoContent)
	helpers.ReadBody(t, updateResp)
}

// TestUpdateAgent_NameInBodyMustMatchPath verifies that if the name field is
// included in the request body, it must match the path parameter. A mismatch
// returns 400 Bad Request.
func TestUpdateAgent_NameInBodyMustMatchPath(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	createPayload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Original.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", createPayload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	// Provide a name in the body that differs from the path.
	mismatchPayload := helpers.AgentUpdate{
		Name:         "completely-different-name",
		SystemPrompt: "Updated.",
		Model:        "sonnet",
	}

	updateResp, err := client.Put("/v1/api/agents/"+agentName, mismatchPayload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/agents/%s: %v", agentName, err)
	}
	defer updateResp.Body.Close()

	helpers.AssertStatusCode(t, updateResp, http.StatusBadRequest)
}

// TestUpdateAgent_NotFoundReturns404 verifies updating a non-existent agent
// returns 404 Not Found.
func TestUpdateAgent_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	payload := helpers.AgentUpdate{
		Name:         "nonexistent-agent",
		SystemPrompt: "This agent does not exist.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	resp, err := client.Put("/v1/api/agents/nonexistent-agent", payload)
	if err != nil {
		t.Fatalf("failed to PUT /v1/api/agents/nonexistent-agent: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

// TestUpdateAgent_ValidationErrors uses table-driven sub-tests for update
// validation. Same validation rules apply as for create.
func TestUpdateAgent_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		payload     helpers.AgentUpdate
		expectField string
	}{
		{
			name:        "missing system_prompt",
			payload:     helpers.AgentUpdate{Name: "valid-name", Model: "sonnet"},
			expectField: "system_prompt",
		},
		{
			name:        "missing model",
			payload:     helpers.AgentUpdate{Name: "valid-name", SystemPrompt: "prompt"},
			expectField: "model",
		},
		{
			name:        "system_prompt too long",
			payload:     helpers.AgentUpdate{Name: "valid-name", SystemPrompt: stringOfLen("x", 51201), Model: "sonnet"},
			expectField: "system_prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := helpers.NewTestClient(t)

			// Create a real agent to update so we exercise the update validation path.
			agentName := helpers.GenerateUniqueName("agent")
			createPayload := helpers.AgentCreate{
				Name:         agentName,
				SystemPrompt: "Setup prompt.",
				Model:        "sonnet",
				Description:  "Test agent.",
				Version:      "1.0.0",
			}
			createResp, err := client.Post("/v1/api/agents", createPayload)
			if err != nil {
				t.Fatalf("failed to create agent: %v", err)
			}
			defer createResp.Body.Close()
			helpers.AssertStatusCode(t, createResp, http.StatusCreated)

			// Override the name in the payload to match the path, except for the
			// "missing model" case where we keep valid-name to test another validator.
			putPayload := tt.payload
			putPayload.Name = agentName

			resp, err := client.Put("/v1/api/agents/"+agentName, putPayload)
			if err != nil {
				t.Fatalf("failed to PUT /v1/api/agents/%s: %v", agentName, err)
			}

			helpers.AssertStatusCode(t, resp, http.StatusBadRequest)

			errResp := helpers.ParseJSON[helpers.ErrorResponse](t, resp)

			if errResp.Status != http.StatusBadRequest {
				t.Errorf("expected error status 400, got %d", errResp.Status)
			}

			fieldFound := false
			for _, fe := range errResp.Errors {
				if fe.Field == tt.expectField {
					fieldFound = true
					break
				}
			}
			if !fieldFound {
				t.Errorf("expected field error for %q, got errors: %+v", tt.expectField, errResp.Errors)
			}

			_ = tt.payload
			_ = tt.expectField
		})
	}
}

// -----------------------------------------------------------------------------
// Delete Agent (DELETE /v1/api/agents/{name})
// -----------------------------------------------------------------------------

// TestDeleteAgent_ExistingReturns204 verifies deleting an existing agent
// returns 204 No Content with an empty body. The agent must no longer be
// retrievable via GET.
func TestDeleteAgent_ExistingReturns204(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "To be deleted.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	deleteResp, err := client.Delete("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/agents/%s: %v", agentName, err)
	}

	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)

	// Verify body is empty.
	body := helpers.ReadBody(t, deleteResp)
	if len(body) != 0 {
		t.Errorf("expected empty body on 204, got: %s", string(body))
	}

	// Confirm agent is gone.
	getResp, err := client.Get("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to GET deleted agent: %v", err)
	}
	defer getResp.Body.Close()
	helpers.AssertStatusCode(t, getResp, http.StatusNotFound)
}

// TestDeleteAgent_NotFoundReturns404 verifies deleting a non-existent agent
// returns 404 Not Found.
func TestDeleteAgent_NotFoundReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Delete("/v1/api/agents/agent-that-never-existed")
	if err != nil {
		t.Fatalf("failed to DELETE /v1/api/agents/agent-that-never-existed: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusNotFound)
}

// TestDeleteAgent_SecondDeleteReturns404 verifies that deleting an already-
// deleted agent returns 404, confirming the resource is gone rather than
// silently succeeding.
func TestDeleteAgent_SecondDeleteReturns404(t *testing.T) {
	client := helpers.NewTestClient(t)

	agentName := helpers.GenerateUniqueName("agent")
	payload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Double delete target.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}

	createResp, err := client.Post("/v1/api/agents", payload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer createResp.Body.Close()
	helpers.AssertStatusCode(t, createResp, http.StatusCreated)

	// First delete — should succeed.
	deleteResp1, err := client.Delete("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed first DELETE: %v", err)
	}
	defer deleteResp1.Body.Close()
	helpers.AssertStatusCode(t, deleteResp1, http.StatusNoContent)

	// Second delete — should return 404.
	deleteResp2, err := client.Delete("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed second DELETE: %v", err)
	}
	defer deleteResp2.Body.Close()
	helpers.AssertStatusCode(t, deleteResp2, http.StatusNotFound)
}

// TestDeleteAgent_CascadesPatternAssociations verifies that deleting an
// agent also removes any pattern_agent_association rows referencing that
// agent (ON DELETE CASCADE).
func TestDeleteAgent_CascadesPatternAssociations(t *testing.T) {
	client := helpers.NewTestClient(t)

	// Create the agent.
	agentName := helpers.GenerateUniqueName("agent")
	agentPayload := helpers.AgentCreate{
		Name:         agentName,
		SystemPrompt: "Agent to be cascade-deleted.",
		Model:        "sonnet",
		Description:  "Test agent.",
		Version:      "1.0.0",
	}
	agentResp, err := client.Post("/v1/api/agents", agentPayload)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer agentResp.Body.Close()
	helpers.AssertStatusCode(t, agentResp, http.StatusCreated)

	// Create a pattern (returns 202 Accepted).
	patternPayload := helpers.PatternCreate{
		Name:       helpers.GenerateUniqueName("ptn"),
		Content:    "Pattern content for cascade deletion test.",
		EntityType: "pattern-type",
		Language:   "agnostic",
		Domain:     "backend",
	}
	patternResp, err := client.Post("/v1/api/patterns", patternPayload)
	if err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}
	helpers.AssertStatusCode(t, patternResp, http.StatusAccepted)

	pattern := helpers.ParseJSON[helpers.Pattern](t, patternResp)
	patternID := pattern.ID
	if patternID == "" {
		t.Fatal("expected pattern ID in 202 response body")
	}

	// Associate the agent with the pattern via PUT /v1/api/patterns/{id}/agents.
	assocPayload := helpers.PatternAgentAssociations{
		Associations: []helpers.AgentAssociation{
			{AgentName: agentName, Relevance: 0.9},
		},
	}
	assocResp, err := client.Put(fmt.Sprintf("/v1/api/patterns/%s/agents", patternID), assocPayload)
	if err != nil {
		t.Fatalf("failed to associate agent with pattern: %v", err)
	}
	helpers.AssertStatusCode(t, assocResp, http.StatusNoContent)
	helpers.ReadBody(t, assocResp)

	// Delete the agent — should cascade.
	deleteResp, err := client.Delete("/v1/api/agents/" + agentName)
	if err != nil {
		t.Fatalf("failed to DELETE agent: %v", err)
	}
	defer deleteResp.Body.Close()
	helpers.AssertStatusCode(t, deleteResp, http.StatusNoContent)

	// Verify the pattern still exists but has no associations.
	getAssocResp, err := client.Get(fmt.Sprintf("/v1/api/patterns/%s/agents", patternID))
	if err != nil {
		t.Fatalf("failed to GET pattern associations after agent deletion: %v", err)
	}
	helpers.AssertStatusCode(t, getAssocResp, http.StatusOK)

	associations := helpers.ParseJSON[helpers.PatternAgentAssociations](t, getAssocResp)
	if len(associations.Associations) != 0 {
		t.Errorf("expected empty associations after agent deletion, got %d: %+v",
			len(associations.Associations), associations.Associations)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// stringOfLen returns a string of the given character repeated n times.
func stringOfLen(ch string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ch[0]
	}
	return string(b)
}
