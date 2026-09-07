package observe

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RecordToolLatency records substrate_tool_latency_seconds{tool}.
func (r *Runtime) RecordToolLatency(ctx context.Context, tool string, d time.Duration) {
	if r == nil || r.toolLatency == nil {
		return
	}
	r.toolLatency.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("tool", tool)))
}

// RecordPackTokens records substrate_pack_tokens{section} for one compile.
func (r *Runtime) RecordPackTokens(ctx context.Context, section string, tokens int) {
	if r == nil || r.packTokens == nil {
		return
	}
	r.packTokens.Record(ctx, int64(tokens), metric.WithAttributes(attribute.String("section", section)))
}

// RecordMemoryStatus increments substrate_memory_status_total{status}.
func (r *Runtime) RecordMemoryStatus(ctx context.Context, status string) {
	if r == nil || r.memoryStatus == nil {
		return
	}
	r.memoryStatus.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status)))
}

// SetOutboxDepth records substrate_outbox_depth{machine}, including zero.
func (r *Runtime) SetOutboxDepth(ctx context.Context, machine string, depth int64) {
	if r == nil || r.outboxDepth == nil {
		return
	}
	r.outboxDepth.Record(ctx, depth, metric.WithAttributes(attribute.String("machine", machine)))
}

// RecordRenderDrift increments substrate_render_drift_total{machine,scope_path}.
func (r *Runtime) RecordRenderDrift(ctx context.Context, machine, scopePath string) {
	if r == nil || r.renderDrift == nil {
		return
	}
	r.renderDrift.Add(ctx, 1, metric.WithAttributes(
		attribute.String("machine", machine),
		attribute.String("scope_path", scopePath),
	))
}

// RecordEmbedFailure increments substrate_embed_failures_total.
func (r *Runtime) RecordEmbedFailure(ctx context.Context) {
	if r == nil || r.embedFailures == nil {
		return
	}
	r.embedFailures.Add(ctx, 1)
}

// SetReviewOpen records substrate_review_open{kind}.
func (r *Runtime) SetReviewOpen(ctx context.Context, kind string, n int64) {
	if r == nil || r.reviewOpen == nil {
		return
	}
	r.reviewOpen.Record(ctx, n, metric.WithAttributes(attribute.String("kind", kind)))
}

// SetReady records substrate_ready{instance} as 1 or 0.
func (r *Runtime) SetReady(ctx context.Context, ready bool) {
	if r == nil || r.ready == nil {
		return
	}
	v := int64(0)
	if ready {
		v = 1
	}
	instance := r.instance
	if instance == "" {
		instance = "unknown"
	}
	r.ready.Record(ctx, v, metric.WithAttributes(attribute.String("instance", instance)))
}
