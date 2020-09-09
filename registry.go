package main

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricRe = regexp.MustCompile(`^[a-z]+[0-9a-z_]+$`)

	defaultBuckets = []float64{
		0.005,
		0.01,
		0.025,
		0.05,
		0.1,
		0.25,
		0.5,
		1.0,
		2.5,
		5.0,
		10.0,
	}
	defaultObjectives = map[float64]float64{
		0.5:  0.05,
		0.9:  0.01,
		0.99: 0.001,
	}
)

type ireg struct {
	handlers map[string]MetricHandler
	mu       sync.Mutex
}

type Registry interface {
	Names() []string
	Reload([]*MetricSpec)
	IsEmpty() bool
	Handle(*Metric) error
	Process(<-chan Metric, <-chan bool)
}

func NewRegistry() Registry {
	return &ireg{handlers: make(map[string]MetricHandler)}
}

func (r *ireg) IsEmpty() bool {
	return len(r.handlers) == 0
}

func (r *ireg) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.doNames()
}

func (r *ireg) doNames() []string {
	var result []string

	for name, _ := range r.handlers {
		result = append(result, name)
	}

	return result
}

func (r *ireg) register(spec *MetricSpec) error {
	if err := validateMetric(spec.Name); err != nil {
		return err
	}

	var (
		handler MetricHandler
		err     error
		ok      bool
	)

	handler, ok = r.handlers[spec.Name]
	if ok {
		// spec with same name already exists
		if handler.Spec().Hash() == spec.Hash() {
			// old spec is same as new spec, we're done
			return nil
		} else {
			// old spec is different from new spec
			// attempt to re-register
			if ok := prometheus.Unregister(handler.Collector()); !ok {
				return fmt.Errorf("Failed to re-register %s", spec.Name)
			}

			delete(r.handlers, spec.Name)
		}
	} else {
		handler, err = buildHandler(spec)
		if err != nil {
			return err
		}
	}

	if err = prometheus.Register(handler.Collector()); err != nil {
		return err
	}

	r.handlers[spec.Name] = handler
	return nil
}

func (r *ireg) unregister(name string) error {
	handler, ok := r.handlers[name]
	if !ok {
		return fmt.Errorf("Unregister: metric %s does not exist", name)
	}

	if ok := prometheus.Unregister(handler.Collector()); !ok {
		return fmt.Errorf("Failed to unregister %s", name)
	}

	delete(r.handlers, name)

	return nil
}

func (r *ireg) Reload(specs []*MetricSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := r.doNames()

	newNames := []string{}
	for _, spec := range specs {
		newNames = append(newNames, spec.Name)
		if err := r.register(spec); err != nil {
			logger.Printf("Error registering %s: %s", spec.Name, err)
		} else {
			logger.Printf("Registered %s", spec.Name)
		}
	}

	// get names of metrics no longer present and unregister them
	unreg := sliceSubStr(names, newNames)
	for _, name := range unreg {
		if err := r.unregister(name); err != nil {
			logger.Println(err)
		} else {
			logger.Printf("Unregistered %s", name)
		}
	}

}

func (r *ireg) Handle(metric *Metric) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.doHandle(metric)
}

func (r *ireg) doHandle(metric *Metric) error {
	handler, ok := r.handlers[metric.Name]
	if !ok {
		return fmt.Errorf("Handle: metric %s does not exist", metric.Name)
	}

	return handler.Handle(metric)
}

func (r *ireg) Process(metricCh <-chan Metric, doneCh <-chan bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger.Println("Starting processing data")
	for {
		select {
		case metric := <-metricCh:
			err := r.doHandle(&metric)
			if err != nil {
				CountMetric("error")
				logger.Printf("ERROR (Process): %s %+v\n", err, metric)
				continue
			}
			CountMetric("ok")
		case <-doneCh:
			return
		}
	}
}

func buildHandler(spec *MetricSpec) (MetricHandler, error) {
	var handler MetricHandler

	switch spec.Type {
	default:
		return nil, fmt.Errorf("Unknown metric %s is unknown type %s", spec.Name, spec.Type)
	case "counter":
		opts := prometheus.CounterOpts{
			Name: spec.Name,
			Help: spec.Help,
		}
		if len(spec.Labels) == 0 {
			counter := prometheus.NewCounter(opts)
			handler = &CounterHandler{spec, counter}
		} else {
			if err := validateLabels(spec.Labels); err != nil {
				return nil, err
			}

			counterVec := prometheus.NewCounterVec(opts, spec.Labels)
			handler = &CounterVecHandler{spec, counterVec}
		}
	case "gauge":
		opts := prometheus.GaugeOpts{
			Name: spec.Name,
			Help: spec.Help,
		}
		if len(spec.Labels) == 0 {
			gauge := prometheus.NewGauge(opts)
			handler = &GaugeHandler{spec, gauge}
		} else {
			if err := validateLabels(spec.Labels); err != nil {
				return nil, err
			}

			gaugeVec := prometheus.NewGaugeVec(opts, spec.Labels)
			handler = &GaugeVecHandler{spec, gaugeVec}
		}
	case "histogram":
		var buckets []float64
		if len(spec.Buckets) > 0 {
			buckets = spec.Buckets
		} else {
			buckets = defaultBuckets
		}
		opts := prometheus.HistogramOpts{
			Name:    spec.Name,
			Help:    spec.Help,
			Buckets: buckets,
		}
		if len(spec.Labels) == 0 {
			histogram := prometheus.NewHistogram(opts)
			handler = &HistogramHandler{spec, histogram}
		} else {
			if err := validateLabels(spec.Labels); err != nil {
				return nil, err
			}

			histogramVec := prometheus.NewHistogramVec(opts, spec.Labels)
			handler = &HistogramVecHandler{spec, histogramVec}
		}
	case "summary":
		var (
			objectives map[float64]float64
			err        error
		)
		if len(spec.Objectives) > 0 {
			objectives, err = validateObjectives(spec.Objectives)
			if err != nil {
				return nil, err
			}
		} else {
			objectives = defaultObjectives
		}
		opts := prometheus.SummaryOpts{
			Name:       spec.Name,
			Help:       spec.Help,
			Objectives: objectives,
		}
		if len(spec.Labels) == 0 {
			summary := prometheus.NewSummary(opts)
			handler = &SummaryHandler{spec, summary}
		} else {
			if err := validateLabels(spec.Labels); err != nil {
				return nil, err
			}

			summaryVec := prometheus.NewSummaryVec(opts, spec.Labels)
			handler = &SummaryVecHandler{spec, summaryVec}
		}
	}

	return handler, nil
}

func validateMetric(name string) error {
	if !metricRe.MatchString(name) {
		return fmt.Errorf("Metric name '%s' is not valid", name)
	}

	return nil
}

func validateLabels(labels []string) error {
	n := len(labels)

	for i := 0; i < n-1; i++ {
		err := validateMetric(labels[i])
		if err != nil {
			return err
		}

		for j := i + 1; j < n; j++ {
			if labels[i] == labels[j] {
				return fmt.Errorf("Duplicate label found: %s", labels[i])
			}
		}
	}

	return nil
}

func validateObjectives(objectives map[string]float64) (map[float64]float64, error) {
	result := make(map[float64]float64)

	for key, value := range objectives {
		f, err := strconv.ParseFloat(key, 64)
		if err != nil {
			return result, err
		}
		result[f] = value
	}

	return result, nil
}
