package observe

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func alertsRepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

// Goes red if deploy/alerts.yaml is missing a Phase 1 rule, if a firing
// series does not interpolate the machine/instance into the message, or
// if the drift annotation no longer says someone is bypassing the adapter
// and no longer points at the review queue.
func TestPhase1AlertsFireAgainstSyntheticSeries(t *testing.T) {
	path := filepath.Join(alertsRepoRoot(), "deploy", "alerts.yaml")
	raw, err := os.ReadFile(path) //nolint:gosec // test fixture path is the repo's deploy/alerts.yaml
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	groups, err := ParseAlertGroups(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outbox := mustAlert(t, groups, "SubstrateOutboxDepth")
	ready := mustAlert(t, groups, "SubstrateReadyz")
	drift := mustAlert(t, groups, "SubstrateDriftProposals")

	t.Run("outbox_high_names_machine", func(t *testing.T) {
		start := time.Unix(0, 0).UTC()
		var samples []Sample
		for i := 0; i <= 31; i++ {
			samples = append(samples, Sample{T: start.Add(time.Duration(i) * time.Minute), V: 501})
		}
		now := start.Add(31 * time.Minute)
		got := EvalAlert(outbox, now, []Series{{
			Name:    MetricOutboxDepth,
			Labels:  map[string]string{"machine": "wsl"},
			Samples: samples,
		}})
		if len(got) != 1 {
			t.Fatalf("high outbox on wsl: %d firing, want 1", len(got))
		}
		msg := got[0].Summary + " " + got[0].Description
		if !strings.Contains(msg, "wsl") {
			t.Fatalf("outbox alert message does not name the machine:\n%s", msg)
		}
	})

	t.Run("outbox_zero_does_not_fire", func(t *testing.T) {
		start := time.Unix(0, 0).UTC()
		var samples []Sample
		for i := 0; i <= 31; i++ {
			samples = append(samples, Sample{T: start.Add(time.Duration(i) * time.Minute), V: 0})
		}
		now := start.Add(31 * time.Minute)
		got := EvalAlert(outbox, now, []Series{{
			Name:    MetricOutboxDepth,
			Labels:  map[string]string{"machine": "mac"},
			Samples: samples,
		}})
		if len(got) != 0 {
			t.Fatalf("zero outbox fired %d alerts; absent-as-zero would also fire here", len(got))
		}
	})

	t.Run("outbox_absent_does_not_fire_as_zero", func(t *testing.T) {
		if strings.Contains(outbox.Expr, "vector(0)") || strings.Contains(outbox.Expr, "or 0") {
			t.Fatalf("expr %q defaults missing series to 0; a machine that stopped reporting would look healthy", outbox.Expr)
		}
		now := time.Unix(0, 0).UTC().Add(31 * time.Minute)
		got := EvalAlert(outbox, now, nil)
		if len(got) != 0 {
			t.Fatalf("no series fired %d alerts; missing must not be treated as zero", len(got))
		}
	})

	t.Run("readyz_names_instance", func(t *testing.T) {
		start := time.Unix(0, 0).UTC()
		var samples []Sample
		for i := 0; i <= 6; i++ {
			samples = append(samples, Sample{T: start.Add(time.Duration(i) * time.Minute), V: 0})
		}
		now := start.Add(6 * time.Minute)
		got := EvalAlert(ready, now, []Series{{
			Name:    MetricReady,
			Labels:  map[string]string{"instance": "cp-0"},
			Samples: samples,
		}})
		if len(got) != 1 {
			t.Fatalf("readyz failing: %d firing, want 1", len(got))
		}
		msg := got[0].Summary + " " + got[0].Description
		if !strings.Contains(msg, "cp-0") {
			t.Fatalf("readyz alert message does not name the instance:\n%s", msg)
		}
	})

	t.Run("drift_names_machine_and_review_queue", func(t *testing.T) {
		start := time.Unix(0, 0).UTC()
		got := EvalAlert(drift, start.Add(24*time.Hour), []Series{{
			Name:   MetricRenderDrift,
			Labels: map[string]string{"machine": "wsl"},
			Samples: []Sample{
				{T: start, V: 0},
				{T: start.Add(24 * time.Hour), V: 11},
			},
		}})
		if len(got) != 1 {
			t.Fatalf("drift > 10/day: %d firing, want 1", len(got))
		}
		msg := got[0].Summary + " " + got[0].Description
		if !strings.Contains(msg, "wsl") {
			t.Fatalf("drift alert message does not name the machine:\n%s", msg)
		}
		if strings.Contains(msg, "$labels.scope_path") || strings.Contains(strings.ToLower(msg), "at scope") {
			t.Fatalf("drift alert still interpolates scope_path (unbounded cardinality):\n%s", msg)
		}
		if !strings.Contains(strings.ToLower(msg), "review") {
			t.Fatalf("drift alert does not point at the review queue:\n%s", msg)
		}
		if !strings.Contains(strings.ToLower(msg), "bypass") && !strings.Contains(strings.ToLower(msg), "missing instruction") {
			t.Fatalf("drift alert does not say someone is bypassing the adapter or an instruction key is missing:\n%s", msg)
		}
	})
}

// Goes red if docs/ops/runbook.md's markdown link to alerts.yaml does not
// resolve on disk. Lychee walks relative links from the linking file, so
// ../deploy/alerts.yaml from docs/ops/ is docs/deploy/alerts.yaml — missing.
func TestRunbookAlertsYamlLinkResolves(t *testing.T) {
	runbook := filepath.Join(alertsRepoRoot(), "docs", "ops", "runbook.md")
	raw, err := os.ReadFile(runbook) //nolint:gosec // test reads the repo runbook
	if err != nil {
		t.Fatalf("read %s: %v", runbook, err)
	}
	re := regexp.MustCompile(`\]\(([^)]*alerts\.yaml)\)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("docs/ops/runbook.md has no markdown link to alerts.yaml")
	}
	dir := filepath.Dir(runbook)
	for _, m := range matches {
		target := filepath.Clean(filepath.Join(dir, m[1]))
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("runbook link %q resolves to %s: %v", m[1], target, err)
		}
		want := filepath.Join(alertsRepoRoot(), "deploy", "alerts.yaml")
		if target != want {
			t.Fatalf("runbook link %q resolves to %s, want %s", m[1], target, want)
		}
	}
}

func mustAlert(t *testing.T, groups []AlertGroup, name string) AlertRule {
	t.Helper()
	for _, g := range groups {
		for _, r := range g.Rules {
			if r.Alert == name {
				return r
			}
		}
	}
	t.Fatalf("alert %q not in deploy/alerts.yaml", name)
	return AlertRule{}
}
