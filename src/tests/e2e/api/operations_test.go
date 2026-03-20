package api_test

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twistingmercury/mnemonic/tests/e2e/helpers"
)

// =============================================================================
// Operations Endpoint Tests
// Spec: GET /health, GET /version, GET /metrics
// =============================================================================
//
// All operations endpoints are public -- no authentication is required.

// metricsBaseURL returns the base URL for the metrics endpoint.
// Metrics are served by the otelx library on a separate port (default 9090).
func metricsBaseURL() string {
	if url := os.Getenv("METRICS_URL"); url != "" {
		return url
	}
	return "http://localhost:9090"
}

// -----------------------------------------------------------------------------
// Health Check (GET /health)
// -----------------------------------------------------------------------------

// TestHealthCheck_AllHealthyReturns200 verifies that when all components
// (postgres, pgvector, neo4j) are healthy, the endpoint returns 200 OK with
// status "OK" and a dependencies array containing each component.
func TestHealthCheck_AllHealthyReturns200(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/health")
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	// The heartbeat library returns a Response struct with fields:
	//   status, name, resource, machine, utc_DateTime, request_duration_ms, dependencies
	// Use a flexible map to parse the response.
	body := helpers.ParseJSON[map[string]any](t, resp)

	status, ok := body["status"].(string)
	if !ok {
		t.Fatalf("expected 'status' field to be a string, got %T", body["status"])
	}

	// Healthy status is "OK" or "NotSet" (when only placeholder deps exist).
	// Both map to HTTP 200 in the heartbeat library.
	validStatuses := map[string]bool{"OK": true, "NotSet": true}
	if !validStatuses[status] {
		t.Fatalf("expected status to be 'OK' or 'NotSet', got %q", status)
	}

	// Verify name field is "mnemonic"
	name, ok := body["name"].(string)
	if !ok {
		t.Fatalf("expected 'name' field to be a string, got %T", body["name"])
	}
	if name != "mnemonic" {
		t.Fatalf("expected name 'mnemonic', got %q", name)
	}
}

// TestHealthCheck_NoAuthRequired verifies the health endpoint returns a
// successful response without any authentication headers. This confirms the
// endpoint is public as specified.
func TestHealthCheck_NoAuthRequired(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Get("/health")
	if err != nil {
		t.Fatalf("failed to GET /health without auth: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestHealthCheck_ResponseContainsAllComponents verifies the dependencies array
// in the response contains entries for PostgreSQL, Neo4j, and the two OpenAI
// placeholders registered via the health package.
func TestHealthCheck_ResponseContainsAllComponents(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/health")
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	body := helpers.ParseJSON[map[string]any](t, resp)

	depsRaw, ok := body["dependencies"]
	if !ok {
		t.Fatal("expected 'dependencies' field in health response")
	}

	deps, ok := depsRaw.([]any)
	if !ok {
		t.Fatalf("expected 'dependencies' to be an array, got %T", depsRaw)
	}

	// The health package registers 4 dependency descriptors:
	//   "PostgreSQL", "Neo4j", "OpenAI embedding model", "OpenAI extraction model"
	expectedNames := map[string]bool{
		"PostgreSQL":              false,
		"Neo4j":                   false,
		"OpenAI embedding model":  false,
		"OpenAI extraction model": false,
	}

	for _, d := range deps {
		depMap, ok := d.(map[string]any)
		if !ok {
			continue
		}
		name, _ := depMap["name"].(string)
		if _, exists := expectedNames[name]; exists {
			expectedNames[name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected dependency %q not found in health response", name)
		}
	}
}

// TestHealthCheck_ContentTypeIsJSON verifies the Content-Type header is
// application/json (with optional charset parameter).
func TestHealthCheck_ContentTypeIsJSON(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/health")
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected Content-Type to start with 'application/json', got %q", ct)
	}
}

// TestHealthCheck_UnhealthyReturns503 verifies that when a dependency is
// down, the endpoint returns 503 Service Unavailable with status "Critical".
//
// Note: This test requires infrastructure manipulation (stopping a
// dependency container) and is skipped in standard E2E runs.
func TestHealthCheck_UnhealthyReturns503(t *testing.T) {
	t.Skip("requires infrastructure manipulation")
}

// -----------------------------------------------------------------------------
// Version (GET /version)
// -----------------------------------------------------------------------------

// TestVersion_ReturnsOKWithVersionInfo verifies the version endpoint returns
// 200 OK with service, version, build_date, and commit fields populated.
func TestVersion_ReturnsOKWithVersionInfo(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/version")
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	ver := helpers.ParseJSON[helpers.VersionResponse](t, resp)

	if ver.Service != "mnemonic" {
		t.Fatalf("expected service 'mnemonic', got %q", ver.Service)
	}

	if ver.Version == "" {
		t.Fatal("expected 'version' field to be non-empty")
	}
}

// TestVersion_NoAuthRequired verifies the version endpoint returns a
// successful response without any authentication headers.
func TestVersion_NoAuthRequired(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Get("/version")
	if err != nil {
		t.Fatalf("failed to GET /version without auth: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestVersion_ContentTypeIsJSON verifies the Content-Type header is
// application/json (with optional charset parameter).
func TestVersion_ContentTypeIsJSON(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/version")
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected Content-Type to start with 'application/json', got %q", ct)
	}
}

// TestVersion_FieldsArePresent verifies that all documented fields (service,
// version, build_date, commit) are present in the response. Fields default to
// "n/a" when the binary is built without ldflags.
func TestVersion_FieldsArePresent(t *testing.T) {
	client := helpers.NewTestClient(t)

	resp, err := client.Get("/version")
	if err != nil {
		t.Fatalf("failed to GET /version: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)

	// Parse into a generic map to verify all expected keys exist.
	body := helpers.ParseJSON[map[string]any](t, resp)

	expectedFields := []string{"service", "version", "build_date", "commit"}
	for _, field := range expectedFields {
		val, ok := body[field]
		if !ok {
			t.Errorf("expected field %q to be present in version response", field)
			continue
		}
		str, ok := val.(string)
		if !ok {
			t.Errorf("expected field %q to be a string, got %T", field, val)
			continue
		}
		if str == "" {
			t.Errorf("expected field %q to be non-empty", field)
		}
	}
}

// -----------------------------------------------------------------------------
// Metrics (GET /metrics)
// -----------------------------------------------------------------------------

// TestMetrics_ReturnsOKWithPrometheusFormat verifies the metrics endpoint
// returns 200 OK with Prometheus exposition format content type.
func TestMetrics_ReturnsOKWithPrometheusFormat(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	// Prometheus exposition format uses text/plain or the OpenMetrics content type.
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Fatalf("expected Content-Type to contain 'text/plain' or 'openmetrics', got %q", ct)
	}
}

// TestMetrics_NoAuthRequired verifies the metrics endpoint returns a
// successful response without any authentication headers.
func TestMetrics_NoAuthRequired(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// Swagger UI (GET /swagger/index.html)
// -----------------------------------------------------------------------------

// TestSwaggerUI_ReturnsOK verifies the Swagger UI index page is served and
// returns HTTP 200. The endpoint is public and requires no authentication.
func TestSwaggerUI_ReturnsOK(t *testing.T) {
	client := helpers.NewUnauthenticatedClient(t)

	resp, err := client.Get("/swagger/index.html")
	if err != nil {
		t.Fatalf("failed to GET /swagger/index.html: %v", err)
	}
	defer resp.Body.Close()

	helpers.AssertStatusCode(t, resp, http.StatusOK)
}

// TestMetrics_ContainsStandardGoMetrics verifies the response body contains
// standard Go runtime metrics (e.g., go_goroutines, go_memstats_alloc_bytes)
// as evidence that the Prometheus handler is wired correctly.
func TestMetrics_ContainsStandardGoMetrics(t *testing.T) {
	metricsURL := metricsBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Get(metricsURL + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET %s/metrics: %v", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := helpers.ReadBody(t, resp)
	bodyStr := string(body)

	// Standard Go runtime metrics exposed by the default Prometheus registry.
	expectedMetrics := []string{
		"go_goroutines",
		"go_memstats_alloc_bytes",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(bodyStr, metric) {
			t.Errorf("expected metrics body to contain %q", metric)
		}
	}
}
