package observe_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	colmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// otlpReceiver is an in-process OTLP/HTTP collector. Production code talks to
// it the same way it would talk to the homelab collector; tests inspect what
// arrived rather than the SDK's in-memory reader, so a broken exporter path
// cannot hide behind a mock Meter.
type otlpReceiver struct {
	URL string

	mu      sync.Mutex
	paths   []string
	lastErr string
	spans   []spanRec
	metrics []metricRec
}

type spanRec struct {
	Name         string
	TraceID      string
	SpanID       string
	ParentSpanID string
}

type metricRec struct {
	Name       string
	Attributes []kv
}

type kv struct{ Key, Value string }

func newOTLPReceiver(t *testing.T) *otlpReceiver {
	t.Helper()
	r := &otlpReceiver{}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(srv.Close)
	r.URL = srv.URL
	return r
}

func (r *otlpReceiver) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.paths = append(r.paths, req.Method+" "+req.URL.Path)
	r.mu.Unlock()
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<22))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body, err = io.ReadAll(zr)
		_ = zr.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	switch {
	case strings.HasSuffix(req.URL.Path, "/v1/traces"):
		var msg coltracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &msg); err != nil {
			r.mu.Lock()
			r.lastErr = req.URL.Path + " unmarshal: " + err.Error()
			r.mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.ingestTraces(&msg)
		out, _ := proto.Marshal(&coltracepb.ExportTraceServiceResponse{})
		_, _ = w.Write(out)
	case strings.HasSuffix(req.URL.Path, "/v1/metrics"):
		var msg colmetricpb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &msg); err != nil {
			r.mu.Lock()
			r.lastErr = req.URL.Path + " unmarshal: " + err.Error()
			r.mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.ingestMetrics(&msg)
		out, _ := proto.Marshal(&colmetricpb.ExportMetricsServiceResponse{})
		_, _ = w.Write(out)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (r *otlpReceiver) ingestTraces(msg *coltracepb.ExportTraceServiceRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rs := range msg.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				r.spans = append(r.spans, spanRec{
					Name:         sp.GetName(),
					TraceID:      formatHex(sp.GetTraceId()),
					SpanID:       formatHex(sp.GetSpanId()),
					ParentSpanID: formatHex(sp.GetParentSpanId()),
				})
			}
		}
	}
}

func (r *otlpReceiver) ingestMetrics(msg *colmetricpb.ExportMetricsServiceRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rm := range msg.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				rec := metricRec{Name: m.GetName(), Attributes: metricAttrs(m)}
				r.metrics = append(r.metrics, rec)
			}
		}
	}
}

func metricAttrs(m *metricpb.Metric) []kv {
	var out []kv
	switch d := m.GetData().(type) {
	case *metricpb.Metric_Histogram:
		for _, dp := range d.Histogram.GetDataPoints() {
			out = append(out, kvsOf(dp.GetAttributes())...)
		}
	case *metricpb.Metric_Sum:
		for _, dp := range d.Sum.GetDataPoints() {
			out = append(out, kvsOf(dp.GetAttributes())...)
		}
	case *metricpb.Metric_Gauge:
		for _, dp := range d.Gauge.GetDataPoints() {
			out = append(out, kvsOf(dp.GetAttributes())...)
		}
	}
	return out
}

func kvsOf(attrs []*commonpb.KeyValue) []kv {
	out := make([]kv, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, kv{Key: a.GetKey(), Value: a.GetValue().GetStringValue()})
	}
	return out
}

func formatHex(b []byte) string {
	const hexd = "0123456789abcdef"
	if len(b) == 0 {
		return ""
	}
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0f]
	}
	return string(out)
}

func (r *otlpReceiver) hasMetric(name, attrKey, attrVal string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.metrics {
		if m.Name != name {
			continue
		}
		if attrKey == "" {
			return true
		}
		for _, a := range m.Attributes {
			if a.Key == attrKey && a.Value == attrVal {
				return true
			}
		}
	}
	return false
}

func (r *otlpReceiver) spansNamed(name string) []spanRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []spanRec
	for _, s := range r.spans {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func (r *otlpReceiver) spanNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.spans))
	for _, s := range r.spans {
		out = append(out, s.Name)
	}
	return out
}

func (r *otlpReceiver) metricNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.metrics))
	for _, m := range r.metrics {
		out = append(out, m.Name)
	}
	return out
}

func (r *otlpReceiver) allSpans() []spanRec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]spanRec, len(r.spans))
	copy(out, r.spans)
	return out
}

func (r *otlpReceiver) requestPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.paths))
	copy(out, r.paths)
	return out
}

func (r *otlpReceiver) errorText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}
