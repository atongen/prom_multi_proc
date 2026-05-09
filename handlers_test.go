package main

import (
	"strings"
	"testing"
)

func TestCounterHandlerInvalidMethod(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry("")
	spec := &MetricSpec{Type: "counter", Name: "handler_counter_inv", Help: "Help"}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	m := &Metric{Name: spec.Name, Method: "bogus"}
	err := registry.Handle(m)
	if err == nil {
		t.Fatal("Expected error for invalid counter method, got nil")
	}
	if !strings.Contains(err.Error(), "invalid counter method") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCounterHandlerNegativeAdd(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry("")
	spec := &MetricSpec{Type: "counter", Name: "handler_counter_neg", Help: "Help"}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	m := &Metric{Name: spec.Name, Method: "add", Value: -1.0}
	if err := registry.Handle(m); err == nil {
		t.Fatal("Expected error for negative counter add, got nil")
	}
}

func TestCounterVecHandlerInvalidMethod(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry("")
	spec := &MetricSpec{
		Type:   "counter",
		Name:   "handler_counter_vec_inv",
		Help:   "Help",
		Labels: []string{"l"},
	}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	m := &Metric{Name: spec.Name, Method: "bogus", LabelValues: []string{"v"}}
	err := registry.Handle(m)
	if err == nil {
		t.Fatal("Expected error for invalid counter vec method, got nil")
	}
	if !strings.Contains(err.Error(), "invalid counter method") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGaugeHandlerInvalidMethod(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry("")
	spec := &MetricSpec{Type: "gauge", Name: "handler_gauge_inv", Help: "Help"}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	m := &Metric{Name: spec.Name, Method: "bogus"}
	err := registry.Handle(m)
	if err == nil {
		t.Fatal("Expected error for invalid gauge method, got nil")
	}
	if !strings.Contains(err.Error(), "invalid gauge method") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGaugeVecHandlerInvalidMethod(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry("")
	spec := &MetricSpec{
		Type:   "gauge",
		Name:   "handler_gauge_vec_inv",
		Help:   "Help",
		Labels: []string{"l"},
	}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	m := &Metric{Name: spec.Name, Method: "bogus", LabelValues: []string{"v"}}
	err := registry.Handle(m)
	if err == nil {
		t.Fatal("Expected error for invalid gauge vec method, got nil")
	}
	if !strings.Contains(err.Error(), "invalid gauge method") {
		t.Errorf("unexpected error message: %v", err)
	}
}
