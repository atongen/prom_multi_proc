package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// metricNameRe matches valid Prometheus metric names.
	metricNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	// labelNameRe matches valid Prometheus label names.
	labelNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	defaultBuckets = []float64{
		0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
	}
	defaultObjectives = map[float64]float64{
		0.5:  0.05,
		0.9:  0.01,
		0.99: 0.001,
	}
)

type ireg struct {
	Handlers map[string]MetricHandler
	prefix   string
	mu       sync.Mutex
}

type Registry interface {
	Names() []string
	Register(*MetricSpec) error
	Unregister(string) error
	Handle(*Metric) error
}

func NewRegistry(prefix string) Registry {
	return &ireg{Handlers: make(map[string]MetricHandler), prefix: prefix}
}

// normalizePrefix ensures the prefix ends with "_" if non-empty.
func normalizePrefix(prefix string) string {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		return prefix + "_"
	}
	return prefix
}

// validatePrefix checks that a normalized prefix is composed of valid metric name characters.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	p := strings.TrimSuffix(prefix, "_")
	if p == "" {
		return fmt.Errorf("prefix %q is not valid", prefix)
	}
	return validateMetricName(p)
}

// applyPrefix prepends prefix to name unless name already starts with prefix.
func applyPrefix(name, prefix string) string {
	if prefix == "" || strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

func (r *ireg) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]string, 0, len(r.Handlers))
	for name := range r.Handlers {
		result = append(result, name)
	}
	return result
}

func (r *ireg) Register(spec *MetricSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.Handlers[spec.Name]; ok {
		return fmt.Errorf("metric %s already exists", spec.Name)
	}

	if err := validateMetricName(spec.Name); err != nil {
		return err
	}

	handler, err := buildHandler(spec, r.prefix)
	if err != nil {
		return err
	}

	if err := prometheus.Register(handler.Collector()); err != nil {
		return err
	}

	r.Handlers[spec.Name] = handler
	return nil
}

func (r *ireg) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	handler, ok := r.Handlers[name]
	if !ok {
		return fmt.Errorf("unregister: metric %s does not exist", name)
	}

	if ok := prometheus.Unregister(handler.Collector()); !ok {
		return fmt.Errorf("failed to unregister %s", name)
	}

	delete(r.Handlers, name)
	return nil
}

func (r *ireg) Handle(metric *Metric) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	handler, ok := r.Handlers[metric.Name]
	if !ok {
		return fmt.Errorf("handle: metric %s does not exist", metric.Name)
	}

	return handler.Handle(metric)
}

func buildHandler(spec *MetricSpec, prefix string) (MetricHandler, error) {
	name := applyPrefix(spec.Name, prefix)
	switch spec.Type {
	case "counter":
		opts := prometheus.CounterOpts{Name: name, Help: spec.Help}
		if len(spec.Labels) == 0 {
			return &CounterHandler{spec, prometheus.NewCounter(opts)}, nil
		}
		if err := validateLabels(spec.Labels); err != nil {
			return nil, err
		}
		return &CounterVecHandler{spec, prometheus.NewCounterVec(opts, spec.Labels)}, nil

	case "gauge":
		opts := prometheus.GaugeOpts{Name: name, Help: spec.Help}
		if len(spec.Labels) == 0 {
			return &GaugeHandler{spec, prometheus.NewGauge(opts)}, nil
		}
		if err := validateLabels(spec.Labels); err != nil {
			return nil, err
		}
		return &GaugeVecHandler{spec, prometheus.NewGaugeVec(opts, spec.Labels)}, nil

	case "histogram":
		buckets := defaultBuckets
		if len(spec.Buckets) > 0 {
			buckets = spec.Buckets
		}
		opts := prometheus.HistogramOpts{Name: name, Help: spec.Help, Buckets: buckets}
		if len(spec.Labels) == 0 {
			return &HistogramHandler{spec, prometheus.NewHistogram(opts)}, nil
		}
		if err := validateLabels(spec.Labels); err != nil {
			return nil, err
		}
		return &HistogramVecHandler{spec, prometheus.NewHistogramVec(opts, spec.Labels)}, nil

	case "summary":
		objectives := defaultObjectives
		if len(spec.Objectives) > 0 {
			var err error
			objectives, err = validateObjectives(spec.Objectives)
			if err != nil {
				return nil, err
			}
		}
		opts := prometheus.SummaryOpts{Name: name, Help: spec.Help, Objectives: objectives}
		if len(spec.Labels) == 0 {
			return &SummaryHandler{spec, prometheus.NewSummary(opts)}, nil
		}
		if err := validateLabels(spec.Labels); err != nil {
			return nil, err
		}
		return &SummaryVecHandler{spec, prometheus.NewSummaryVec(opts, spec.Labels)}, nil

	default:
		return nil, fmt.Errorf("metric %s has unknown type %s", spec.Name, spec.Type)
	}
}

func validateMetricName(name string) error {
	if !metricNameRe.MatchString(name) {
		return fmt.Errorf("metric name %q is not valid", name)
	}
	return nil
}

func validateLabelName(name string) error {
	if !labelNameRe.MatchString(name) {
		return fmt.Errorf("label name %q is not valid", name)
	}
	return nil
}

func validateLabels(labels []string) error {
	for i, label := range labels {
		if err := validateLabelName(label); err != nil {
			return err
		}
		for j := i + 1; j < len(labels); j++ {
			if labels[i] == labels[j] {
				return fmt.Errorf("duplicate label found: %s", labels[i])
			}
		}
	}
	return nil
}

func validateObjectives(objectives map[string]float64) (map[float64]float64, error) {
	result := make(map[float64]float64, len(objectives))
	for key, value := range objectives {
		f, err := strconv.ParseFloat(key, 64)
		if err != nil {
			return nil, err
		}
		result[f] = value
	}
	return result, nil
}
