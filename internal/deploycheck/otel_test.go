package deploycheck

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The collector's ConfigMap is a YAML document embedded in a YAML document,
// and the outer one being well-formed says nothing about the inner one. The
// previous test here only asserted that the ConfigMap *string* contained a
// Service name and a port, which is true of a config that cannot start:
// `traces` was wired to the `prometheus` exporter, which is metrics-only, so
// the collector failed config validation at startup and the Deployment sat in
// CrashLoopBackOff — with every structural assertion still green.
//
// So parse it and check the semantics.

// Which telemetry types each component we use is actually valid for. This is a
// property of the collector's component set, not of our config; extend it when
// a new component is introduced.
var exporterTypes = map[string]map[string]bool{
	// Metrics only. This is the whole finding.
	"prometheus":            {"metrics": true},
	"prometheusremotewrite": {"metrics": true},
	"otlp":                  {"metrics": true, "traces": true, "logs": true},
	"otlphttp":              {"metrics": true, "traces": true, "logs": true},
	"debug":                 {"metrics": true, "traces": true, "logs": true},
	"loki":                  {"logs": true},
}

type collectorConfig struct {
	Extensions map[string]struct {
		Endpoint string `yaml:"endpoint"`
		Path     string `yaml:"path"`
	} `yaml:"extensions"`
	Receivers  map[string]any `yaml:"receivers"`
	Processors map[string]any `yaml:"processors"`
	Exporters  map[string]struct {
		Endpoint          string `yaml:"endpoint"`
		AddMetricSuffixes *bool  `yaml:"add_metric_suffixes"`
	} `yaml:"exporters"`
	Service struct {
		Extensions []string `yaml:"extensions"`
		Pipelines  map[string]struct {
			Receivers  []string `yaml:"receivers"`
			Processors []string `yaml:"processors"`
			Exporters  []string `yaml:"exporters"`
		} `yaml:"pipelines"`
	} `yaml:"service"`
}

func loadCollectorConfig(t *testing.T) collectorConfig {
	t.Helper()
	cm := docOfKind(t, "deploy/otel/collector.yaml", "ConfigMap")
	data := asMap(t, cm["data"], "data")
	raw, ok := data["collector.yaml"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		t.Fatal("ConfigMap has no collector.yaml payload")
	}
	var cfg collectorConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("collector.yaml embedded in the ConfigMap is not valid YAML: %v", err)
	}
	if len(cfg.Service.Pipelines) == 0 {
		t.Fatal("collector.yaml declares no pipelines: it would start and do nothing")
	}
	return cfg
}

// Red when: a pipeline names an exporter that cannot serve that pipeline's
// telemetry type (the `traces -> prometheus` bug), or names a component that
// is not defined in the config at all. Both are startup-fatal: the collector
// exits non-zero and CrashLoopBackOffs, so the metrics the rules in
// deploy/alerts.yaml depend on never arrive and every rule stays green.
func TestOTelPipelinesOnlyNameExportersValidForTheirTelemetryType(t *testing.T) {
	cfg := loadCollectorConfig(t)

	for name, p := range cfg.Service.Pipelines {
		// A pipeline id is `metrics` or `metrics/two`.
		telemetry := name
		if i := strings.Index(name, "/"); i >= 0 {
			telemetry = name[:i]
		}
		switch telemetry {
		case "metrics", "traces", "logs":
		default:
			t.Errorf("pipeline %q is not a known telemetry type", name)
			continue
		}

		if len(p.Exporters) == 0 {
			t.Errorf("pipeline %q has no exporters", name)
		}
		for _, r := range p.Receivers {
			if _, ok := cfg.Receivers[r]; !ok {
				t.Errorf("pipeline %q names receiver %q, which is not defined", name, r)
			}
		}
		for _, pr := range p.Processors {
			if _, ok := cfg.Processors[pr]; !ok {
				t.Errorf("pipeline %q names processor %q, which is not defined", name, pr)
			}
		}
		for _, e := range p.Exporters {
			if _, ok := cfg.Exporters[e]; !ok {
				t.Errorf("pipeline %q names exporter %q, which is not defined", name, e)
				continue
			}
			base := e
			if i := strings.Index(e, "/"); i >= 0 {
				base = e[:i]
			}
			supported, known := exporterTypes[base]
			if !known {
				t.Errorf("exporter %q is not in this test's capability table; add it (with its real telemetry types) before shipping it", base)
				continue
			}
			if !supported[telemetry] {
				t.Errorf("pipeline %q uses exporter %q, which does not support %s. "+
					"The collector fails config validation at startup (\"telemetry type is not supported\") "+
					"and CrashLoopBackOffs forever", name, e, telemetry)
			}
		}
	}

	for _, ext := range cfg.Service.Extensions {
		if _, ok := cfg.Extensions[ext]; !ok {
			t.Errorf("service.extensions names %q, which is not defined", ext)
		}
	}
}

// The prometheus exporter serves /metrics and 404s on /. kubelet treats >=400
// as a probe failure, so a livenessProbe on `/` of the exporter's port kills
// the collector every period — a second, independent CrashLoop cause that no
// structural assertion could see.
//
// Red when: a probe path is not one the component listening on that port
// actually serves.
func TestOTelProbesHitAPathTheConfiguredComponentServes(t *testing.T) {
	cfg := loadCollectorConfig(t)

	// port number -> the paths that return 200 on it.
	served := map[int]map[string]bool{}
	for name, e := range cfg.Exporters {
		if strings.HasPrefix(name, "prometheus") && e.Endpoint != "" {
			p := portOfEndpoint(t, e.Endpoint)
			served[p] = map[string]bool{"/metrics": true}
		}
	}
	for name, ext := range cfg.Extensions {
		if strings.HasPrefix(name, "health_check") && ext.Endpoint != "" {
			path := ext.Path
			if path == "" {
				path = "/" // documented default
			}
			p := portOfEndpoint(t, ext.Endpoint)
			served[p] = map[string]bool{path: true}
		}
	}
	if len(served) == 0 {
		t.Fatal("no HTTP-serving component found in collector.yaml; nothing can back a probe")
	}

	dep := docOfKind(t, "deploy/otel/collector.yaml", "Deployment")
	containers, _ := nested(t, dep, "spec", "template", "spec", "containers").([]any)
	if len(containers) == 0 {
		t.Fatal("collector Deployment has no containers")
	}
	c := asMap(t, containers[0], "container")

	// name -> containerPort
	byName := map[string]int{}
	ports, _ := c["ports"].([]any)
	for _, p := range ports {
		pm := asMap(t, p, "port")
		n, _ := pm["name"].(string)
		byName[n] = intOf(t, pm["containerPort"], "containerPort")
	}

	checked := 0
	for _, probe := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
		pv, ok := c[probe]
		if !ok {
			continue
		}
		get, ok := asMap(t, pv, probe)["httpGet"].(map[string]any)
		if !ok {
			continue
		}
		checked++
		path, _ := get["path"].(string)
		var portNum int
		switch v := get["port"].(type) {
		case string:
			n, ok := byName[v]
			if !ok {
				t.Fatalf("%s targets port name %q, which the container does not declare", probe, v)
			}
			portNum = n
		default:
			portNum = intOf(t, v, probe+".port")
		}
		paths, ok := served[portNum]
		if !ok {
			t.Errorf("%s targets container port %d, but no component in collector.yaml listens there", probe, portNum)
			continue
		}
		if !paths[path] {
			want := make([]string, 0, len(paths))
			for p := range paths {
				want = append(want, p)
			}
			t.Errorf("%s gets %q on port %d, which serves only %v. kubelet treats >=400 as a failure, "+
				"so this probe restarts (or never readies) the collector every period",
				probe, path, portNum, want)
		}
	}
	if checked == 0 {
		t.Fatal("the collector has no HTTP probe at all: a wedged collector is never restarted and the alerts go quiet")
	}
}

// substrate_render_drift_total is already a *_total counter and
// substrate_tool_latency_seconds already carries its unit. The prometheus
// exporter's add_metric_suffixes defaults to true; whether it de-duplicates an
// already-present suffix has varied across releases, and a
// substrate_render_drift_total_total silently breaks SubstrateDriftProposals
// in deploy/alerts.yaml — an alert that can never fire looks exactly like an
// alert that has nothing to fire about.
//
// Red when: add_metric_suffixes is removed or set true.
func TestOTelPrometheusExporterDoesNotRewriteMetricNames(t *testing.T) {
	cfg := loadCollectorConfig(t)
	for name, e := range cfg.Exporters {
		if !strings.HasPrefix(name, "prometheus") {
			continue
		}
		if e.AddMetricSuffixes == nil {
			t.Fatalf("exporter %q leaves add_metric_suffixes at its default (true): the scraped names may become "+
				"substrate_render_drift_total_total / substrate_tool_latency_seconds_seconds and deploy/alerts.yaml stops matching", name)
		}
		if *e.AddMetricSuffixes {
			t.Fatalf("exporter %q sets add_metric_suffixes: true; the instrument names in internal/observe already carry their suffixes", name)
		}
	}
}

func portOfEndpoint(t *testing.T, endpoint string) int {
	t.Helper()
	i := strings.LastIndex(endpoint, ":")
	if i < 0 {
		t.Fatalf("endpoint %q has no port", endpoint)
	}
	n := 0
	for _, r := range endpoint[i+1:] {
		if r < '0' || r > '9' {
			t.Fatalf("endpoint %q has a non-numeric port", endpoint)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func intOf(t *testing.T, v any, path string) int {
	t.Helper()
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	t.Fatalf("%s: want an integer, got %T", path, v)
	return 0
}
