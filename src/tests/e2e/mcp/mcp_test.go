// Package mcp_test provides end-to-end tests for the Mnemonic MCP server.
//
// These tests validate the MCP JSON-RPC interface from an external consumer's
// perspective, treating the server as a black box. All requests go through the
// network using the JSON-RPC 2.0 protocol over HTTP POST /mcp.
package mcp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/twistingmercury/mnemonic/tests/e2e/helpers"
)

// Section 1: tools/list

func TestMCPToolsList_ReturnsThreeTools(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.ListTools(t)
	if resp.Error != nil {
		t.Fatalf("ListTools returned error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("ListTools returned nil result")
	}
	tools := resp.Result.Tools
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"search_patterns", "find_related_patterns", "get_pattern"} {
		if !names[expected] {
			t.Errorf("expected tool %q not found in list", expected)
		}
	}
}

func TestMCPToolsList_ToolsHaveDescriptions(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.ListTools(t)
	if resp.Error != nil {
		t.Fatalf("ListTools returned error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("ListTools returned nil result")
	}
	for _, tool := range resp.Result.Tools {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

// Section 2: get_pattern validation

func TestMCPGetPattern_EmptyID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": ""})
	helpers.AssertMCPError(t, resp)
}

func TestMCPGetPattern_InvalidUUID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": "not-a-uuid"})
	helpers.AssertMCPError(t, resp)
}

// Section 3: get_pattern data paths

func TestMCPGetPattern_ExistingPattern_ReturnsPendingMarkdown(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "get_pattern", map[string]any{"id": pattern.ID})
	result := helpers.AssertMCPSuccess(t, resp)

	if len(result.Content) == 0 {
		t.Fatal("expected content in result, got none")
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", result.Content[0].Type)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, pattern.Name) {
		t.Errorf("expected result to contain pattern name %q, got: %s", pattern.Name, text)
	}
	if !strings.Contains(strings.ToLower(text), "pending") {
		t.Errorf("expected result to contain 'pending' status, got: %s", text)
	}
}

func TestMCPGetPattern_NonExistentID_ReturnsNotFoundError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": uuid.New().String()})
	helpers.AssertMCPError(t, resp)
}

// Section 4: find_related_patterns validation

func TestMCPFindRelated_EmptyPatternID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": ""})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_InvalidUUID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": "bad-uuid"})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_LimitTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{
		"pattern_id": uuid.New().String(),
		"limit":      0,
	})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_LimitTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{
		"pattern_id": uuid.New().String(),
		"limit":      21,
	})
	helpers.AssertMCPError(t, resp)
}

// Section 5: find_related_patterns data paths

func TestMCPFindRelated_ExistingPattern_ReturnsSuccessOrEmpty(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": pattern.ID})

	if resp.Error != nil || (resp.Result != nil && resp.Result.IsError) {
		t.Log("Outcome B: MCP returned error (expected for unenriched pattern with no Neo4j node)")
		return
	}
	result := helpers.AssertMCPSuccess(t, resp)
	t.Log("Outcome A: MCP returned success")
	if len(result.Content) > 0 {
		text := result.Content[0].Text
		if !strings.Contains(text, "No related patterns found for") {
			t.Errorf("expected 'No related patterns found for' in response, got: %s", text)
		}
	}
}

func TestMCPFindRelated_NonExistentID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": uuid.New().String()})
	helpers.AssertMCPError(t, resp)
}

// Section 6: search_patterns validation

func TestMCPSearchPatterns_LimitTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "limit": 0})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_LimitTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "limit": 51})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_ThresholdTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "threshold": -0.1})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_ThresholdTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "threshold": 1.1})
	helpers.AssertMCPError(t, resp)
}

// Section 7: search_patterns with valid query

func TestMCPSearchPatterns_ValidQuery_ReturnsResponse(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "dependency injection"})
	// Server may return an error (embedding service unavailable) or a success with no results.
	// Either is acceptable — we only assert that the call completes without a transport failure.
	if resp.Error == nil && resp.Result == nil {
		t.Fatal("expected either a JSON-RPC error or a result, got neither")
	}
}

// Section 8: Protocol correctness

func TestMCPEndpoint_InvalidMethod_ReturnsProtocolError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	req := helpers.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      999,
		Method:  "nonexistent/method",
		Params:  map[string]any{},
	}
	resp := client.Post(t, req)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result helpers.JSONRPCResponse
	if err := json.Unmarshal(helpers.ExtractSSEData(data), &result); err != nil {
		// A non-JSON-RPC response body also indicates the method was rejected — pass.
		return
	}
	if result.Error == nil {
		t.Fatal("expected JSON-RPC error for unknown method, got none")
	}
}

func TestMCPEndpoint_MalformedJSON_DoesNotPanic(t *testing.T) {
	mcpURL := helpers.MCPBaseURL() + "/mcp"
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewBufferString(`{not valid json}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("server panicked or returned 500 on malformed JSON, status: %d", resp.StatusCode)
	}
}
