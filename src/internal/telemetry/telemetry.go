package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic/internal/config"
	"github.com/twistingmercury/mnemonic/internal/metrics"
	"github.com/twistingmercury/mnemonic/internal/version"
	"github.com/twistingmercury/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry wraps the otelx.Telemetry with application-specific helpers.
type Telemetry struct {
	otel            *otelx.Telemetry
	logger          zerolog.Logger
	metricsRegistry *metrics.Registry
}

// Initialize creates and configures the telemetry system using otelx.
// It sets up logging, metrics, and tracing based on the provided configuration.
// Returns an error if the log level is invalid or telemetry initialization fails.
func Initialize(ctx context.Context, cfg *config.MnemonicConfig) (*Telemetry, error) {
	opts, err := buildOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build telemetry options: %w", err)
	}

	tel, err := otelx.Initialize(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	// Get meter for metrics registry, using global noop meter if metrics not enabled
	var meter metric.Meter
	if tel.MeterProvider != nil {
		meter = tel.MeterProvider.Meter("mnemonic")
	} else {
		meter = otel.Meter("mnemonic")
	}

	// Create metrics registry for handler instrumentation
	registry, err := metrics.NewRegistry(meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics registry: %w", err)
	}

	return &Telemetry{
		otel:            tel,
		logger:          tel.Logger,
		metricsRegistry: registry,
	}, nil
}

// buildOptions constructs otelx options from the configuration.
// Returns an error if the log level cannot be parsed.
func buildOptions(cfg *config.MnemonicConfig) ([]otelx.Option, error) {
	logLevel, err := parseLogLevel(cfg.Logging.Level)
	if err != nil {
		return nil, err
	}

	opts := []otelx.Option{
		otelx.WithService(
			"mnemonic",
			version.Version(),
			getEnvironment(),
		),
		otelx.WithLogLevel(logLevel),
	}

	// Metrics configuration - otelx uses opt-in pattern
	if cfg.Observability.Metrics.Enabled {
		opts = append(opts, otelx.WithMetrics(cfg.Observability.Metrics.Port))
		opts = append(opts, otelx.WithMetricsPath(cfg.Observability.Metrics.Path))
	}

	// Tracing configuration - otelx uses opt-in pattern
	if cfg.Observability.Tracing.Enabled {
		opts = append(opts, otelx.WithTracing())
		opts = append(opts, otelx.WithTraceSampleRate(cfg.Observability.Tracing.SampleRate))
		if cfg.Observability.Tracing.Endpoint != "" {
			opts = append(opts, otelx.WithOTLPEndpoint(cfg.Observability.Tracing.Endpoint))
		}
		if cfg.Observability.Tracing.OTLPInsecure {
			opts = append(opts, otelx.WithOTLPInsecure())
		}
	}

	return opts, nil
}

// parseLogLevel converts a string log level to zerolog.Level.
// Returns an error if the level string is invalid.
func parseLogLevel(level string) (zerolog.Level, error) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.NoLevel, fmt.Errorf("invalid log level %q: %w", level, err)
	}
	return l, nil
}

// getEnvironment determines the environment from the MNEMONIC_ENV environment variable.
// Returns "development" if the environment variable is not set.
func getEnvironment() string {
	if env := os.Getenv("MNEMONIC_ENV"); env != "" {
		return env
	}
	return "development"
}

// Shutdown gracefully shuts down telemetry, flushing pending data.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	return t.otel.Shutdown(ctx)
}

// Logger returns the zerolog logger with trace correlation support.
func (t *Telemetry) Logger() zerolog.Logger {
	return t.logger
}

// MetricsRegistry returns the metrics registry for handler instrumentation.
func (t *Telemetry) MetricsRegistry() *metrics.Registry {
	return t.metricsRegistry
}

// Tracer returns an OpenTelemetry tracer for creating spans.
func (t *Telemetry) Tracer(name string) trace.Tracer {
	if t.otel.TracerProvider != nil {
		return t.otel.TracerProvider.Tracer(name)
	}
	// Return global noop tracer if tracing not enabled
	return otel.Tracer(name)
}

// Meter returns an OpenTelemetry meter for creating metrics.
func (t *Telemetry) Meter(name string) metric.Meter {
	if t.otel.MeterProvider != nil {
		return t.otel.MeterProvider.Meter(name)
	}
	// Return global noop meter if metrics not enabled
	return otel.Meter(name)
}

// TracerProvider returns the underlying trace provider.
func (t *Telemetry) TracerProvider() trace.TracerProvider {
	if t.otel.TracerProvider != nil {
		return t.otel.TracerProvider
	}
	return otel.GetTracerProvider()
}

// MeterProvider returns the underlying meter provider.
func (t *Telemetry) MeterProvider() metric.MeterProvider {
	if t.otel.MeterProvider != nil {
		return t.otel.MeterProvider
	}
	return otel.GetMeterProvider()
}

// Otelx returns the underlying otelx.Telemetry instance.
// This provides access to otelx-specific functionality like middleware.
func (t *Telemetry) Otelx() *otelx.Telemetry {
	return t.otel
}
