package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestNewRegistry(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	registry, err := metrics.NewRegistry(meter)
	require.NoError(t, err)
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.Patterns)
	assert.NotNil(t, registry.Database)
}
