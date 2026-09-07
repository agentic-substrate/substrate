package observe

import (
	"context"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type quietSpanExporter struct {
	next  sdktrace.SpanExporter
	fails *failLog
}

func (e *quietSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.next == nil {
		return nil
	}
	e.fails.record(e.next.ExportSpans(ctx, spans))
	return nil
}

func (e *quietSpanExporter) Shutdown(ctx context.Context) error {
	if e.next == nil {
		return nil
	}
	// Honors ctx: a hanging collector delays process exit up to the
	// caller's deadline (5s in substrate-server). Requests are unaffected.
	e.fails.record(e.next.Shutdown(ctx))
	return nil
}

type quietMetricExporter struct {
	sdkmetric.Exporter
	fails *failLog
}

func (e *quietMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if e.Exporter == nil {
		return nil
	}
	e.fails.record(e.Exporter.Export(ctx, rm))
	return nil
}

func (e *quietMetricExporter) ForceFlush(ctx context.Context) error {
	if e.Exporter == nil {
		return nil
	}
	e.fails.record(e.Exporter.ForceFlush(ctx))
	return nil
}

func (e *quietMetricExporter) Shutdown(ctx context.Context) error {
	if e.Exporter == nil {
		return nil
	}
	e.fails.record(e.Exporter.Shutdown(ctx))
	return nil
}
