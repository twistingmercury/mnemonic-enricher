package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/heartbeat"
	"github.com/twistingmercury/mnemonic/internal/health"
)

// --- Mocks ---

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

type mockConnVerifier struct {
	err error
}

func (m *mockConnVerifier) VerifyConnectivity(_ context.Context) error {
	return m.err
}

// --- Initialize tests ---

func TestInitialize_Success(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{},
	})

	require.NoError(t, err)

	descs := health.Descriptors()
	require.Len(t, descs, 4)
	assert.Equal(t, "PostgreSQL", descs[0].Name)
	assert.Equal(t, "database", descs[0].Type)
	assert.Equal(t, "Neo4j", descs[1].Name)
	assert.Equal(t, "database", descs[1].Type)
	assert.Equal(t, "OpenAI embedding model", descs[2].Name)
	assert.Equal(t, "external_api", descs[2].Type)
	assert.Equal(t, "OpenAI extraction model", descs[3].Name)
	assert.Equal(t, "external_api", descs[3].Type)
}

func TestInitialize_NilPGPool(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      nil,
		Neo4jDriver: &mockConnVerifier{},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PostgreSQL pool is nil")
}

func TestInitialize_NilNeo4jDriver(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Neo4j driver is nil")
}

// --- Descriptors before Initialize ---

func TestDescriptors_BeforeInitialize(t *testing.T) {
	t.Cleanup(health.ResetForTest)
	health.ResetForTest()

	descs := health.Descriptors()
	assert.Nil(t, descs)
}

// --- Individual check tests ---

func TestCheckPostgreSQLHealth_NotInitialized(t *testing.T) {
	t.Cleanup(health.ResetForTest)
	health.ResetForTest()

	result := health.CheckPostgreSQLHealthForTest()
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "not initialized")
}

func TestCheckPostgreSQLHealth_PingSuccess(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: nil},
		Neo4jDriver: &mockConnVerifier{},
	})
	require.NoError(t, err)

	result := health.CheckPostgreSQLHealthForTest()
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
	assert.GreaterOrEqual(t, result.RequestDuration, float64(0))
}

func TestCheckPostgreSQLHealth_PingFailure(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: errors.New("connection refused")},
		Neo4jDriver: &mockConnVerifier{},
	})
	require.NoError(t, err)

	result := health.CheckPostgreSQLHealthForTest()
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "ping failed")
	assert.Contains(t, result.Message, "connection refused")
}

func TestCheckNeo4jHealth_NotInitialized(t *testing.T) {
	t.Cleanup(health.ResetForTest)
	health.ResetForTest()

	result := health.CheckNeo4jHealthForTest()
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "not initialized")
}

func TestCheckNeo4jHealth_VerifySuccess(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{err: nil},
	})
	require.NoError(t, err)

	result := health.CheckNeo4jHealthForTest()
	assert.Equal(t, heartbeat.StatusOK, result.Status)
	assert.Equal(t, "ok", result.Message)
	assert.GreaterOrEqual(t, result.RequestDuration, float64(0))
}

func TestCheckNeo4jHealth_VerifyFailure(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{},
		Neo4jDriver: &mockConnVerifier{err: errors.New("neo4j unreachable")},
	})
	require.NoError(t, err)

	result := health.CheckNeo4jHealthForTest()
	assert.Equal(t, heartbeat.StatusCritical, result.Status)
	assert.Contains(t, result.Message, "connectivity check failed")
	assert.Contains(t, result.Message, "neo4j unreachable")
}

func TestCheckEmbeddingModel_ReturnsNotSet(t *testing.T) {
	result := health.CheckEmbeddingModelForTest()
	assert.Equal(t, heartbeat.StatusNotSet, result.Status)
	assert.Contains(t, result.Message, "no ping endpoint available")
}

func TestCheckExtractionModel_ReturnsNotSet(t *testing.T) {
	result := health.CheckExtractionModelForTest()
	assert.Equal(t, heartbeat.StatusNotSet, result.Status)
	assert.Contains(t, result.Message, "no ping endpoint available")
}

// --- CheckHealth integration tests ---

func TestCheckHealth_AllHealthy(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: nil},
		Neo4jDriver: &mockConnVerifier{err: nil},
	})
	require.NoError(t, err)

	err = health.CheckHealth()
	assert.NoError(t, err)
}

func TestCheckHealth_PostgresUnhealthy(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: errors.New("pg down")},
		Neo4jDriver: &mockConnVerifier{err: nil},
	})
	require.NoError(t, err)

	err = health.CheckHealth()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PostgreSQL")
}

func TestCheckHealth_Neo4jUnhealthy(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: nil},
		Neo4jDriver: &mockConnVerifier{err: errors.New("neo4j down")},
	})
	require.NoError(t, err)

	err = health.CheckHealth()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Neo4j")
}

func TestCheckHealth_BothUnhealthy(t *testing.T) {
	t.Cleanup(health.ResetForTest)

	err := health.Initialize(health.Dependencies{
		PGPool:      &mockPinger{err: errors.New("pg down")},
		Neo4jDriver: &mockConnVerifier{err: errors.New("neo4j down")},
	})
	require.NoError(t, err)

	err = health.CheckHealth()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PostgreSQL")
	assert.Contains(t, err.Error(), "Neo4j")
}
