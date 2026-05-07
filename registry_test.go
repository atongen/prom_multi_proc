package main

import (
	"strings"
	"testing"
)

func TestValidateMetricName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{"my_metric", true},
		{"MyMetric", true},
		{"my_metric_total", true},
		{"my:metric", true},
		{"_private", true},
		{"metric123", true},
		{"", false},
		{"1starts_with_digit", false},
		{"has-hyphen", false},
		{"has space", false},
	} {
		err := validateMetricName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("validateMetricName(%q) unexpectedly failed: %v", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateMetricName(%q) should have failed but did not", tt.name)
		}
	}
}

func TestValidateLabelName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{"label", true},
		{"Label", true},
		{"label_name", true},
		{"label123", true},
		{"_private", true},
		{"", false},
		{"1starts_digit", false},
		{"has-hyphen", false},
		{"has:colon", false},
		{"has space", false},
	} {
		err := validateLabelName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("validateLabelName(%q) unexpectedly failed: %v", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateLabelName(%q) should have failed but did not", tt.name)
		}
	}
}

func TestValidateLabels(t *testing.T) {
	for _, tt := range []struct {
		labels []string
		errMsg string
	}{
		{[]string{"a", "b", "c"}, ""},
		{[]string{"one"}, ""},
		{[]string{}, ""},
		{[]string{"a", "a"}, "duplicate"},
		{[]string{"a", "b", "b"}, "duplicate"},
		{[]string{"a", "b", "a"}, "duplicate"},
		{[]string{"bad-name"}, "not valid"},
		{[]string{"a", "bad-name", "c"}, "not valid"},
		// last label is now validated (fixed off-by-one)
		{[]string{"a", "b", "bad-name"}, "not valid"},
	} {
		err := validateLabels(tt.labels)
		if tt.errMsg == "" {
			if err != nil {
				t.Errorf("validateLabels(%v) unexpectedly failed: %v", tt.labels, err)
			}
		} else {
			if err == nil {
				t.Errorf("validateLabels(%v) should have failed with %q but did not", tt.labels, tt.errMsg)
			} else if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("validateLabels(%v) error %q does not contain %q", tt.labels, err.Error(), tt.errMsg)
			}
		}
	}
}

func TestRegisterInvalidMetricName(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{
		Type: "counter",
		Name: "1invalid_name",
		Help: "Invalid metric name",
	}
	if err := registry.Register(spec); err == nil {
		t.Fatal("Expected error registering invalid metric name, but got nil")
	}
}

func TestRegisterInvalidLabelName(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{
		Type:   "counter",
		Name:   "valid_metric_name",
		Help:   "Help",
		Labels: []string{"valid", "bad-label"},
	}
	if err := registry.Register(spec); err == nil {
		t.Fatal("Expected error registering invalid label name, but got nil")
	}
}

func TestRegisterDuplicateLabels(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{
		Type:   "gauge",
		Name:   "dup_label_metric",
		Help:   "Help",
		Labels: []string{"foo", "bar", "foo"},
	}
	if err := registry.Register(spec); err == nil {
		t.Fatal("Expected error for duplicate labels, but got nil")
	}
}

func TestRegisterDuplicateMetric(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{Type: "counter", Name: "dup_reg_counter", Help: "Help"}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(spec); err == nil {
		t.Fatal("Expected error re-registering same metric, but got nil")
	}
}

func TestRegisterUnknownType(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{Type: "bogus", Name: "bogus_metric", Help: "Help"}
	if err := registry.Register(spec); err == nil {
		t.Fatal("Expected error for unknown metric type, but got nil")
	}
}

func TestUnregisterNonExistent(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	if err := registry.Unregister("does_not_exist"); err == nil {
		t.Fatal("Expected error unregistering non-existent metric, but got nil")
	}
}

func TestHandleNonExistentMetric(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	m := &Metric{Name: "nonexistent", Method: "inc"}
	if err := registry.Handle(m); err == nil {
		t.Fatal("Expected error handling non-existent metric, but got nil")
	}
}
