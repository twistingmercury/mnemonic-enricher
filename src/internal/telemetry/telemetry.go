package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/twistingmercury/mnemonic-enricher/internal/config"
	"github.com/twistingmercury/mnemonic-enricher/internal/metrics"
	"github.com/twistingmercury/mnemonic-enricher/internal/version"
	"github.com/twistingmercury/otelx/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
	otelCfg, err := buildConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build telemetry config: %w", err)
	}

	tel, err := otelx.Initialize(ctx, otelCfg)
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

// buildConfig maps the application configuration onto an otelx.Config.
// Returns an error if the log level is invalid or the trace exporter cannot be created.
func buildConfig(ctx context.Context, cfg *config.MnemonicConfig) (otelx.Config, error) {
	if _, err := zerolog.ParseLevel(cfg.Logging.Level); err != nil {
		return otelx.Config{}, fmt.Errorf("invalid log level %q: %w", cfg.Logging.Level, err)
	}

	otelCfg := otelx.Config{
		ServiceName:    "mnemonic",
		ServiceVersion: version.Version(),
		Environment:    getEnvironment(),
		LogLevel:       cfg.Logging.Level,
	}

	if cfg.Observability.Metrics.Enabled {
		otelCfg.MetricsAddr = fmt.Sprintf(":%d", cfg.Observability.Metrics.Port)
		otelCfg.MetricsPath = cfg.Observability.Metrics.Path
	}

	if cfg.Observability.Tracing.Enabled {
		otelCfg.Tracing = true
		otelCfg.Sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Observability.Tracing.SampleRate))

		// otelx v2 reads the OTLP endpoint from OTEL_EXPORTER_OTLP_* variables.
		// Build the exporter here so the mnemonic config keeps working.
		var opts []otlptracegrpc.Option
		if cfg.Observability.Tracing.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Observability.Tracing.Endpoint))
		}
		if cfg.Observability.Tracing.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return otelx.Config{}, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
		otelCfg.TraceExporter = exporter
	}

	return otelCfg, nil
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
