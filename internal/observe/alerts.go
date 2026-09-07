package observe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// AlertGroup is one Prometheus rule group.
type AlertGroup struct {
	Name  string      `yaml:"name"`
	Rules []AlertRule `yaml:"rules"`
}

// AlertRule is one firing condition. Expr is a small PromQL subset evaluated
// by EvalAlert against synthetic series.
type AlertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

// Sample is one (time, value) point.
type Sample struct {
	T time.Time
	V float64
}

// Series is a named metric with labels.
type Series struct {
	Name    string
	Labels  map[string]string
	Samples []Sample
}

// Firing is one evaluated alert.
type Firing struct {
	Alert       string
	Labels      map[string]string
	Summary     string
	Description string
}

type alertFile struct {
	Groups []AlertGroup `yaml:"groups"`
}

// ParseAlertGroups loads a Prometheus-style rule file.
func ParseAlertGroups(raw []byte) ([]AlertGroup, error) {
	var f alertFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if len(f.Groups) == 0 {
		return nil, fmt.Errorf("observe: no alert groups")
	}
	return f.Groups, nil
}

var (
	reIncrease = regexp.MustCompile(`^increase\(([A-Za-z0-9_]+)\[([0-9]+[smhd])\]\)\s*>\s*([0-9.]+)$`)
	reCmp      = regexp.MustCompile(`^([A-Za-z0-9_]+)\s*(==|>|<|>=|<=)\s*([0-9.]+)$`)
	reLabel    = regexp.MustCompile(`\{\{\s*\$labels\.([A-Za-z0-9_]+)\s*\}\}`)
)

// EvalAlert fires rule against series at now. Missing series are not zero.
func EvalAlert(rule AlertRule, now time.Time, series []Series) []Firing {
	hold, err := parsePromDuration(rule.For)
	if err != nil {
		hold = 0
	}
	if m := reIncrease.FindStringSubmatch(strings.TrimSpace(rule.Expr)); m != nil {
		window, err := parsePromDuration(m[2])
		if err != nil {
			return nil
		}
		thresh, _ := strconv.ParseFloat(m[3], 64)
		return evalIncrease(rule, now, m[1], window, thresh, series)
	}
	if m := reCmp.FindStringSubmatch(strings.TrimSpace(rule.Expr)); m != nil {
		thresh, _ := strconv.ParseFloat(m[3], 64)
		return evalCmp(rule, now, m[1], m[2], thresh, hold, series)
	}
	return nil
}

func evalIncrease(rule AlertRule, now time.Time, name string, window time.Duration, thresh float64, series []Series) []Firing {
	var out []Firing
	for _, s := range series {
		if s.Name != name {
			continue
		}
		first, ok0 := valueAt(s.Samples, now.Add(-window))
		last, ok1 := valueAt(s.Samples, now)
		if !ok0 || !ok1 {
			continue
		}
		if last-first > thresh {
			out = append(out, firingOf(rule, s.Labels))
		}
	}
	return out
}

func evalCmp(rule AlertRule, now time.Time, name, op string, thresh float64, hold time.Duration, series []Series) []Firing {
	var out []Firing
	for _, s := range series {
		if s.Name != name {
			continue
		}
		if hold > 0 {
			if !held(s.Samples, now.Add(-hold), now, op, thresh) {
				continue
			}
		} else {
			v, ok := valueAt(s.Samples, now)
			if !ok || !cmp(v, op, thresh) {
				continue
			}
		}
		out = append(out, firingOf(rule, s.Labels))
	}
	return out
}

func held(samples []Sample, from, to time.Time, op string, thresh float64) bool {
	var n int
	var first, last time.Time
	for _, s := range samples {
		if s.T.Before(from) || s.T.After(to) {
			continue
		}
		if !cmp(s.V, op, thresh) {
			return false
		}
		if n == 0 || s.T.Before(first) {
			first = s.T
		}
		if n == 0 || s.T.After(last) {
			last = s.T
		}
		n++
	}
	if n == 0 {
		return false
	}
	// Prometheus `for` is a pending duration: matching samples must cover
	// the whole [from, to] window, not merely exist somewhere inside it.
	return !first.After(from) && !last.Before(to)
}

func valueAt(samples []Sample, at time.Time) (float64, bool) {
	var best Sample
	found := false
	for _, s := range samples {
		if s.T.After(at) {
			continue
		}
		if !found || s.T.After(best.T) {
			best = s
			found = true
		}
	}
	return best.V, found
}

func cmp(v float64, op string, thresh float64) bool {
	switch op {
	case ">":
		return v > thresh
	case ">=":
		return v >= thresh
	case "<":
		return v < thresh
	case "<=":
		return v <= thresh
	case "==":
		return v == thresh
	}
	return false
}

func firingOf(rule AlertRule, labels map[string]string) Firing {
	ann := rule.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	return Firing{
		Alert:       rule.Alert,
		Labels:      labels,
		Summary:     interpolate(ann["summary"], labels),
		Description: interpolate(ann["description"], labels),
	}
}

func interpolate(s string, labels map[string]string) string {
	return reLabel.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLabel.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := labels[sub[1]]; ok {
			return v
		}
		return m
	})
}

func parsePromDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
