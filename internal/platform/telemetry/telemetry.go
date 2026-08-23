// Package telemetry centraliza logging estruturado (slog) e OpenTelemetry.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
)

// NewLogger cria um logger JSON estruturado no nível configurado.
func NewLogger(cfg config.Telemetry) *slog.Logger {
	level := parseLevel(cfg.LogLevel)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With(slog.String("service", cfg.ServiceName))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Shutdown libera os recursos de telemetria. Seguro chamar mesmo sem inicialização.
type Shutdown func(context.Context) error

// SetupTracing configura o tracer provider OTLP quando habilitado.
// Quando desabilitado, retorna um Shutdown no-op — a aplicação roda normalmente.
func SetupTracing(ctx context.Context, cfg config.Telemetry) (Shutdown, error) {
	if !cfg.OTelEnabled {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(cfg.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
