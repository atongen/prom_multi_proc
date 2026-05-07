package main

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type MetricHandler interface {
	Spec() *MetricSpec
	Handle(*Metric) error
	Collector() prometheus.Collector
}

type CounterHandler struct {
	spec    *MetricSpec
	Counter prometheus.Counter
}

func (h *CounterHandler) Spec() *MetricSpec { return h.spec }

func (h *CounterHandler) Handle(m *Metric) error {
	switch m.Method {
	case "inc":
		h.Counter.Inc()
	case "add":
		if m.Value < 0 {
			return fmt.Errorf("counter cannot decrease in value")
		}
		h.Counter.Add(m.Value)
	default:
		return fmt.Errorf("invalid counter method %q for metric %s", m.Method, m.Name)
	}
	return nil
}

func (h *CounterHandler) Collector() prometheus.Collector { return h.Counter }

type CounterVecHandler struct {
	spec       *MetricSpec
	CounterVec *prometheus.CounterVec
}

func (h *CounterVecHandler) Spec() *MetricSpec { return h.spec }

func (h *CounterVecHandler) Handle(m *Metric) error {
	metric, err := h.CounterVec.GetMetricWithLabelValues(m.LabelValues...)
	if err != nil {
		return err
	}
	switch m.Method {
	case "inc":
		metric.Inc()
	case "add":
		if m.Value < 0 {
			return fmt.Errorf("counter cannot decrease in value")
		}
		metric.Add(m.Value)
	default:
		return fmt.Errorf("invalid counter method %q for metric %s", m.Method, m.Name)
	}
	return nil
}

func (h *CounterVecHandler) Collector() prometheus.Collector { return h.CounterVec }

type GaugeHandler struct {
	spec  *MetricSpec
	Gauge prometheus.Gauge
}

func (h *GaugeHandler) Spec() *MetricSpec { return h.spec }

func (h *GaugeHandler) Handle(m *Metric) error {
	switch m.Method {
	case "set":
		h.Gauge.Set(m.Value)
	case "inc":
		h.Gauge.Inc()
	case "dec":
		h.Gauge.Dec()
	case "add":
		h.Gauge.Add(m.Value)
	case "sub":
		h.Gauge.Sub(m.Value)
	case "set_to_current_time":
		h.Gauge.SetToCurrentTime()
	default:
		return fmt.Errorf("invalid gauge method %q for metric %s", m.Method, m.Name)
	}
	return nil
}

func (h *GaugeHandler) Collector() prometheus.Collector { return h.Gauge }

type GaugeVecHandler struct {
	spec     *MetricSpec
	GaugeVec *prometheus.GaugeVec
}

func (h *GaugeVecHandler) Spec() *MetricSpec { return h.spec }

func (h *GaugeVecHandler) Handle(m *Metric) error {
	metric, err := h.GaugeVec.GetMetricWithLabelValues(m.LabelValues...)
	if err != nil {
		return err
	}
	switch m.Method {
	case "set":
		metric.Set(m.Value)
	case "inc":
		metric.Inc()
	case "dec":
		metric.Dec()
	case "add":
		metric.Add(m.Value)
	case "sub":
		metric.Sub(m.Value)
	case "set_to_current_time":
		metric.SetToCurrentTime()
	default:
		return fmt.Errorf("invalid gauge method %q for metric %s", m.Method, m.Name)
	}
	return nil
}

func (h *GaugeVecHandler) Collector() prometheus.Collector { return h.GaugeVec }

type HistogramHandler struct {
	spec      *MetricSpec
	Histogram prometheus.Histogram
}

func (h *HistogramHandler) Spec() *MetricSpec { return h.spec }

func (h *HistogramHandler) Handle(m *Metric) error {
	h.Histogram.Observe(m.Value)
	return nil
}

func (h *HistogramHandler) Collector() prometheus.Collector { return h.Histogram }

type HistogramVecHandler struct {
	spec         *MetricSpec
	HistogramVec *prometheus.HistogramVec
}

func (h *HistogramVecHandler) Spec() *MetricSpec { return h.spec }

func (h *HistogramVecHandler) Handle(m *Metric) error {
	metric, err := h.HistogramVec.GetMetricWithLabelValues(m.LabelValues...)
	if err != nil {
		return err
	}
	metric.Observe(m.Value)
	return nil
}

func (h *HistogramVecHandler) Collector() prometheus.Collector { return h.HistogramVec }

type SummaryHandler struct {
	spec    *MetricSpec
	Summary prometheus.Summary
}

func (h *SummaryHandler) Spec() *MetricSpec { return h.spec }

func (h *SummaryHandler) Handle(m *Metric) error {
	h.Summary.Observe(m.Value)
	return nil
}

func (h *SummaryHandler) Collector() prometheus.Collector { return h.Summary }

type SummaryVecHandler struct {
	spec       *MetricSpec
	SummaryVec *prometheus.SummaryVec
}

func (h *SummaryVecHandler) Spec() *MetricSpec { return h.spec }

func (h *SummaryVecHandler) Handle(m *Metric) error {
	metric, err := h.SummaryVec.GetMetricWithLabelValues(m.LabelValues...)
	if err != nil {
		return err
	}
	metric.Observe(m.Value)
	return nil
}

func (h *SummaryVecHandler) Collector() prometheus.Collector { return h.SummaryVec }
