// Package observe is Substrate's OpenTelemetry pipeline: metrics and traces
// over OTLP/HTTP, plus the slog fields EDD §12 names. An empty endpoint
// disables export and is a supported configuration, not a degraded one.
package observe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Metric and span names. The EDD spells these acp_*; AGENTS.md translates
// them. A metric name cannot be renamed later without breaking dashboards.
const (
	MetricToolLatency = "substrate_tool_latency_seconds"
	//nolint:gosec // G101: this is a metric name, not a credential
	MetricPackTokens     = "substrate_pack_tokens"
	MetricMemoryStatus   = "substrate_memory_status_total"
	MetricOutboxDepth    = "substrate_outbox_depth"
	MetricRenderDrift    = "substrate_render_drift_total"
	MetricEmbedFailures  = "substrate_embed_failures_total"
	MetricReviewOpen     = "substrate_review_open"
	MetricReady          = "substrate_ready"
	SpanCompile          = "compile"
	SpanDatabase         = "database"
	instrumentationName  = "github.com/agentic-substrate/substrate"
	defaultExportEvery   = 60 * time.Second
	defaultExportTimeout = 10 * time.Second
)

type ctxKey int

const (
	runtimeKey ctxKey = iota
	fieldsKey
	slotKey
)

// Config is -otlp / SUBSTRATE_OTLP_ENDPOINT plus test knobs. Endpoint empty
// means export is disabled (the -ollama pattern).
type Config struct {
	Endpoint       string
	Service        string
	Instance       string
	ExportInterval time.Duration
	ExportTimeout  time.Duration
	Logger         *slog.Logger
}

// Runtime holds the SDK providers and instruments. It is passed through
// context; tests must not install a process-global MeterProvider.
type Runtime struct {
	tp       *sdktrace.TracerProvider
	mp       *sdkmetric.MeterProvider
	tracer   trace.Tracer
	logger   *slog.Logger
	fails    *failLog
	instance string

	toolLatency   metric.Float64Histogram
	packTokens    metric.Int64Gauge
	memoryStatus  metric.Int64Counter
	outboxDepth   metric.Int64Gauge
	renderDrift   metric.Int64Counter
	embedFailures metric.Int64Counter
	reviewOpen    metric.Int64Gauge
	ready         metric.Int64Gauge
}

type failLog struct {
	log *slog.Logger
}

func (f *failLog) record(err error) {
	if f == nil || err == nil {
		return
	}
	exportFailOnce.Do(func() {
		f.log.Error("otlp export failed", "err", err)
	})
}

// exportFailOnce is process-global so two Setup calls (tests, or a
// mistaken double-start) still log a collector outage once. Production
// starts one Runtime per binary.
var exportFailOnce sync.Once

type requestFields struct {
	mu        sync.Mutex
	ScopePath string
}

// Setup builds a Runtime. An empty Endpoint returns a no-op Runtime that
// still accepts Record* calls. Export errors are logged once per process
// and never fail a request.
func Setup(ctx context.Context, cfg Config) (*Runtime, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Endpoint == "" {
		return nopRuntime(logger), nil
	}
	timeout := cfg.ExportTimeout
	if timeout <= 0 {
		timeout = defaultExportTimeout
	}
	interval := cfg.ExportInterval
	if interval <= 0 {
		interval = defaultExportEvery
	}
	service := cfg.Service
	if service == "" {
		service = "substrate-server"
	}
	instance := cfg.Instance
	if instance == "" {
		h, err := os.Hostname()
		if err != nil {
			h = "unknown"
		}
		instance = h
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", service),
			attribute.String("service.instance.id", instance),
		),
	)
	if err != nil {
		return nil, err
	}
	fails := &failLog{log: logger}

	host, insecure, err := otlpHost(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithTimeout(timeout),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	}
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host),
		otlpmetrichttp.WithTimeout(timeout),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}

	texp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, err
	}
	mexp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(&quietSpanExporter{next: texp, fails: fails},
			sdktrace.WithBatchTimeout(interval),
			sdktrace.WithExportTimeout(timeout),
		),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(&quietMetricExporter{Exporter: mexp, fails: fails},
			sdkmetric.WithInterval(interval),
			sdkmetric.WithTimeout(timeout),
		)),
		sdkmetric.WithResource(res),
	)

	rt := &Runtime{tp: tp, mp: mp, logger: logger, fails: fails, instance: instance}
	rt.tracer = tp.Tracer(instrumentationName)
	if err := rt.initInstruments(mp.Meter(instrumentationName)); err != nil {
		_ = rt.Shutdown(ctx)
		return nil, err
	}
	return rt, nil
}

func nopRuntime(logger *slog.Logger) *Runtime {
	rt := &Runtime{
		tracer: tracenoop.NewTracerProvider().Tracer(instrumentationName),
		logger: logger,
	}
	_ = rt.initInstruments(metricnoop.NewMeterProvider().Meter(instrumentationName))
	return rt
}

func (r *Runtime) initInstruments(m metric.Meter) error {
	var err error
	r.toolLatency, err = m.Float64Histogram(MetricToolLatency, metric.WithUnit("s"))
	if err != nil {
		return err
	}
	r.packTokens, err = m.Int64Gauge(MetricPackTokens)
	if err != nil {
		return err
	}
	r.memoryStatus, err = m.Int64Counter(MetricMemoryStatus)
	if err != nil {
		return err
	}
	r.outboxDepth, err = m.Int64Gauge(MetricOutboxDepth)
	if err != nil {
		return err
	}
	r.renderDrift, err = m.Int64Counter(MetricRenderDrift)
	if err != nil {
		return err
	}
	r.embedFailures, err = m.Int64Counter(MetricEmbedFailures)
	if err != nil {
		return err
	}
	r.reviewOpen, err = m.Int64Gauge(MetricReviewOpen)
	if err != nil {
		return err
	}
	r.ready, err = m.Int64Gauge(MetricReady)
	return err
}

// Shutdown flushes and releases providers. Safe on a no-op Runtime.
// A hanging collector can delay return up to ctx; the error is logged
// once and never returned, so process exit is not a failed export.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.tp != nil {
		r.fails.record(r.tp.Shutdown(ctx))
	}
	if r.mp != nil {
		r.fails.record(r.mp.Shutdown(ctx))
	}
	return nil
}

// ForceFlush exports pending signals. Used by tests; production relies on the
// periodic reader and batcher.
func (r *Runtime) ForceFlush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var err error
	if r.tp != nil {
		err = errors.Join(err, r.tp.ForceFlush(ctx))
	}
	if r.mp != nil {
		err = errors.Join(err, r.mp.ForceFlush(ctx))
	}
	return err
}

// FromContext returns the Runtime on ctx, or nil.
func FromContext(ctx context.Context) *Runtime {
	r, _ := ctx.Value(runtimeKey).(*Runtime)
	return r
}

// WithRuntime stores r on ctx.
func WithRuntime(ctx context.Context, r *Runtime) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, runtimeKey, r)
}

func withFields(ctx context.Context) context.Context {
	return context.WithValue(ctx, fieldsKey, &requestFields{})
}

// NewLogFields allocates a fresh scope_path slot for one tool call.
func NewLogFields(ctx context.Context) context.Context {
	return withFields(ctx)
}

func fieldsFrom(ctx context.Context) *requestFields {
	f, _ := ctx.Value(fieldsKey).(*requestFields)
	return f
}

// SetScopePath records the resolved scope on ctx for the request log. It
// never logs a body.
func SetScopePath(ctx context.Context, path string) {
	if f := fieldsFrom(ctx); f != nil {
		f.mu.Lock()
		f.ScopePath = path
		f.mu.Unlock()
	}
}

// ScopePathFrom returns the scope set by SetScopePath, or empty.
func ScopePathFrom(ctx context.Context) string {
	f := fieldsFrom(ctx)
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ScopePath
}

// Slot is the per-request observe state. HTTP middleware allocates one; the
// MCP transport copies the pointer onto the tool context so SetScopePath is
// visible to the request log.
type Slot struct {
	rt     *Runtime
	fields *requestFields
}

// SlotFrom returns the Slot on ctx. It is never nil.
func SlotFrom(ctx context.Context) *Slot {
	if s, ok := ctx.Value(slotKey).(*Slot); ok && s != nil {
		return s
	}
	return &Slot{rt: FromContext(ctx), fields: fieldsFrom(ctx)}
}

// Restore puts s onto ctx.
func (s *Slot) Restore(ctx context.Context) context.Context {
	if s == nil {
		return ctx
	}
	ctx = WithRuntime(ctx, s.rt)
	if s.fields != nil {
		ctx = context.WithValue(ctx, fieldsKey, s.fields)
	}
	ctx = context.WithValue(ctx, slotKey, s)
	return ctx
}

// Start starts a span on the Runtime in ctx, or a no-op span.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if r := FromContext(ctx); r != nil && r.tracer != nil {
		return r.tracer.Start(ctx, name, opts...)
	}
	return tracenoop.NewTracerProvider().Tracer(instrumentationName).Start(ctx, name, opts...)
}

func otlpHost(endpoint string) (host string, insecure bool, err error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", true, fmt.Errorf("observe: empty OTLP endpoint")
	}
	if !strings.Contains(endpoint, "://") {
		return endpoint, true, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("observe: OTLP endpoint: %w", err)
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("observe: OTLP endpoint missing host")
	}
	return u.Host, u.Scheme != "https", nil
}
