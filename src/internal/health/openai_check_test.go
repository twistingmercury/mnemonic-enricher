package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/mnemonic-enricher/internal/health"
)

func TestOpenAIModelCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models/text-embedding-3-small", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":"text-embedding-3-small"}`))
	}))
	defer srv.Close()

	check := health.OpenAIModelCheckForTest(srv.URL, "sk-test", "text-embedding-3-small")
	result := check(context.Background())

	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, "text-embedding-3-small", result.Resource)
}

func TestOpenAIModelCheck_ErrorStatusIsWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	result := health.OpenAIModelCheckForTest(srv.URL, "bad-key", "gpt-4o-mini")(context.Background())

	assert.Equal(t, heartbeat.StatusWarning, result.Status)
	assert.Equal(t, http.StatusUnauthorized, result.StatusCode)
	assert.Contains(t, result.Message, "401")
}

func TestOpenAIModelCheck_UnreachableIsWarning(t *testing.T) {
	result := health.OpenAIModelCheckForTest("http://127.0.0.1:0", "sk-test", "gpt-4o-mini")(context.Background())

	assert.Equal(t, heartbeat.StatusWarning, result.Status)
	assert.Equal(t, "request failed", result.Message)
}

func TestCheckHealth_OpenAIWarningDoesNotFail(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{},
	})
	require.NoError(t, err)

	status, results := heartbeat.CheckDependencies(context.Background(), health.Descriptors())
	assert.Equal(t, heartbeat.StatusWarning, status)
	for _, r := range results[2:] {
		assert.Equal(t, heartbeat.StatusWarning, r.Status, r.Name)
	}

	assert.NoError(t, health.CheckHealth())
}
