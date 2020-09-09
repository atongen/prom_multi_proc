package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"
)

func getTestSpecs(t *testing.T, i int) []*MetricSpec {
	specsStr := fmt.Sprintf(`[
	{
		"type": "counter",
		"name": "test_%d_counter",
		"help": "Test %d counter"
	},
	{
		"type": "counter",
		"name": "test_%d_counter_vec",
		"help": "Test %d counter vector",
		"labels": [
			"one",
			"two",
			"three"
		]
	},
	{
		"type": "gauge",
		"name": "test_%d_gauge",
		"help": "Test %d gauge"
	},
	{
		"type": "gauge",
		"name": "test_%d_gauge_vec",
		"help": "Test %d gauge vector",
		"labels": [
			"one",
			"two",
			"three"
		]
	},
	{
		"type": "histogram",
		"name": "test_%d_histogram",
		"help": "Test %d histogram"
	},
	{
		"type": "histogram",
		"name": "test_%d_histogram_vec",
		"help": "Test %d histogram vector",
		"labels": [
			"one",
			"two",
			"three"
		]
	},
	{
		"type": "histogram",
		"name": "test_%d_histogram_vec_buckets",
		"help": "Test %d histogram vector",
		"labels": [
			"one",
			"two",
			"three"
		],
		"buckets": [0.1, 0.5, 0.9]
	},
	{
		"type": "summary",
		"name": "test_%d_summary",
		"help": "Test %d summary"
	},
	{
		"type": "summary",
		"name": "test_%d_summary_vec",
		"help": "Test %d summary vector",
		"labels": [
			"one",
			"two",
			"three"
		]
	},
	{
		"type": "summary",
		"name": "test_%d_summary_vec_objectives",
		"help": "Test %d summary vector",
		"labels": [
			"one",
			"two",
			"three"
		],
		"objectives": {
			"0.1": 0.1,
			"0.5": 0.5,
			"0.9": 0.9
		}
	}
]`, []interface{}{i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i, i}...)
	specsReader := strings.NewReader(specsStr)
	specs, err := ReadSpecs(specsReader)
	if err != nil {
		t.Fatal(err)
	}

	return specs
}

func SetTestLogger() {
	var out bytes.Buffer
	logger = log.New(&out, "", log.LstdFlags)
}

func TestMetrics1(t *testing.T) {
	SetTestLogger()
	specs := getTestSpecs(t, 1)

	if len(specs) != 10 {
		t.Errorf("Expected 10 metric specs, but got %d", len(specs))
	}

	registry := NewRegistry()
	registry.Reload(specs)

	for _, spec := range specs {
		var labelValues []string
		if strings.Contains(spec.Name, "_vec") {
			labelValues = []string{"a", "b", "c"}
		}

		var methods []string
		switch spec.Type {
		default:
			t.Fatalf("Invalid metric handler type: %+v", spec)
		case "counter", "counter_vec":
			methods = []string{"inc", "add"}
		case "gauge", "gauge_vec":
			methods = []string{"set", "inc", "dec", "add", "sub", "set_to_current_time"}
		case "histogram", "histogram_vec", "summary", "summary_vec":
			methods = []string{"observe"}
		}

		if len(methods) == 0 {
			t.Fatalf("No methods found for spec: %+v", spec)
		}

		for _, method := range methods {
			m := Metric{
				Name:        spec.Name,
				Method:      method,
				Value:       1.0,
				LabelValues: labelValues,
			}
			if err := registry.Handle(&m); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMetrics2Fail(t *testing.T) {
	SetTestLogger()
	specs := getTestSpecs(t, 2)

	if len(specs) != 10 {
		t.Errorf("Expected 10 metric specs, but got %d", len(specs))
	}

	registry := NewRegistry()
	registry.Reload(specs)

	for _, spec := range specs {
		var labelValues []string
		if strings.Contains(spec.Name, "_vec") {
			labelValues = []string{"a", "b", "c", "d"}
		}

		var methods []string
		switch spec.Type {
		default:
			t.Fatalf("Invalid metric handler type: %+v", spec)
		case "counter", "counter_vec":
			methods = []string{"inc", "add"}
		case "gauge", "gauge_vec":
			methods = []string{"set", "inc", "dec", "add", "sub", "set_to_current_time"}
		case "histogram", "histogram_vec", "summary", "summary_vec":
			methods = []string{"observe"}
		}

		if len(methods) == 0 {
			t.Fatalf("No methods found for spec: %+v", spec)
		}

		for _, method := range methods {
			m := Metric{
				Name:        spec.Name,
				Method:      method,
				Value:       1.0,
				LabelValues: labelValues,
			}
			err := registry.Handle(&m)
			if strings.Contains(spec.Name, "_vec") {
				// here we expect failure due to label length miss-match
				if err == nil {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestMetrics3Rereg(t *testing.T) {
	SetTestLogger()
	specs := getTestSpecs(t, 3)
	specsUpdate := getTestSpecs(t, 4)

	if len(specs) != 10 {
		t.Errorf("Expected 10 metric specs, but got %d", len(specs))
	}

	if len(specsUpdate) != 10 {
		t.Errorf("Expected 10 metric specs, but got %d", len(specsUpdate))
	}

	registry := NewRegistry()
	registry.Reload(specs)

	regLen := len(registry.Names())
	if regLen != 10 {
		t.Errorf("Expected 10 registered metric specs, but got %d", regLen)
	}

	// save specs[2] for later testing
	mySpec := specs[2]

	// emulate a USR1 with some of specsUpdate
	specs[3] = specsUpdate[3]
	specs[7] = specsUpdate[7]

	registry.Reload(specs)

	regLen = len(registry.Names())
	if regLen != 10 {
		t.Errorf("Expected 10 registered metrics, but got %d", regLen)
	}

	// modify a spec
	mySpec.Labels = []string{"Now", "for", "something", "completely", "different"}
	specs[2] = mySpec
	registry.Reload(specs)

	expected := []string{
		"test_3_gauge", "test_3_histogram", "test_4_gauge_vec", "test_3_counter_vec",
		"test_3_histogram_vec", "test_3_histogram_vec_buckets", "test_3_summary_vec",
		"test_3_summary_vec_objectives", "test_4_summary", "test_3_counter",
	}

	names := registry.Names()
	actualLen := len(names)
	expectedLen := len(expected)
	if actualLen != expectedLen {
		t.Errorf("Expected %d re-registered metrics, but got %d.", expectedLen, actualLen)
	}

	for _, e := range expected {
		if !sliceContainsStr(names, e) {
			t.Errorf("Expected re-registered metrics to contain %s, but it did not.", e)
		}
	}
}

func TestMetrics5Multi(t *testing.T) {
	SetTestLogger()
	specs := getTestSpecs(t, 5)

	metricCh := make(chan Metric)
	dataCh := make(chan []byte)

	registry := NewRegistry()
	registry.Reload(specs)

	go DataParser(dataCh, metricCh)

	data := []Metric{
		Metric{
			Name:   "test_5_counter",
			Method: "inc",
		},
		Metric{
			Name: "test_5_gauge_vec",
			LabelValues: []string{
				"1",
				"2",
				"3",
			},
			Method: "add",
			Value:  39.0,
		},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		dataCh <- b
	}()

	for i := 0; i < 2; i++ {
		metric := <-metricCh
		switch i {
		default:
			t.Fatalf("Invalid metric number: %d", i)
		case 0:
			if metric.Name != "test_5_counter" {
				t.Fatalf("Expected metric 1 name to be 'test_5_counter', but was %s", metric.Name)
			}
			if metric.Method != "inc" {
				t.Fatalf("Expected metric 1 method to be 'inc', but was %s", metric.Method)
			}
			if len(metric.LabelValues) != 0 {
				t.Fatalf("Expected length of metric 1 label values to be to be 0, but was %d", len(metric.LabelValues))
			}
			if metric.Value != 0.0 {
				t.Fatalf("Expected metric 1 value to be 0.0, but was %f", metric.Value)
			}
		case 1:
			if metric.Name != "test_5_gauge_vec" {
				t.Fatalf("Expected metric 2 name to be 'test_5_gauge_vec', but was %s", metric.Name)
			}
			if metric.Method != "add" {
				t.Fatalf("Expected metric 2 method to be 'add', but was %s", metric.Method)
			}
			if len(metric.LabelValues) != 3 {
				t.Fatalf("Expected length of metric 3 label values to be to be 3, but was %d", len(metric.LabelValues))
			}
			if metric.Value != 39.0 {
				t.Fatalf("Expected metric 2 value to be 39.0, but was %f", metric.Value)
			}
		}
	}
}

func TestMetrics6Rereg(t *testing.T) {
	SetTestLogger()
	specsBefore, err := ReadSpecs(strings.NewReader(`[
		{
			"type": "counter",
			"name": "test_6_counter_vec_0",
			"help": "Test 6 counter vector 0",
			"labels": ["one", "two", "three"]
		},
		{
			"type": "counter",
			"name": "test_6_counter_vec_1",
			"help": "Test 6 counter vector 1",
			"labels": ["aaa", "bbb", "ccc"]
		},
		{
			"type": "counter",
			"name": "test_6_counter_vec_2",
			"help": "Test 6 counter vector 2",
			"labels": ["apple", "banana", "carrot"]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	specsAfter, err := ReadSpecs(strings.NewReader(`[
		{
			"type": "counter",
			"name": "test_6_counter_vec_1",
			"help": "Test 6 counter vector 1",
			"labels": ["aaa", "bbb", "ccc"]
		},
		{
			"type": "counter",
			"name": "test_6_counter_vec_2",
			"help": "Test 6 counter vector 2",
			"labels": ["alice", "bob", "carol"]
		},
		{
			"type": "counter",
			"name": "test_6_counter_vec_3",
			"help": "Test 6 counter vector 3",
			"labels": ["dog", "cat", "bird"]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	if len(specsBefore) != 3 {
		t.Errorf("Expected 3 specsBefore, but got %d", len(specsBefore))
	}

	if len(specsAfter) != 3 {
		t.Errorf("Expected 3 specsAfter, but got %d", len(specsAfter))
	}

	registry := NewRegistry()

	registry.Reload(specsBefore)
	regLen := len(registry.Names())
	if regLen != 3 {
		t.Errorf("Expected 3 registered specsBefore, but got %d", regLen)
	}

	expected := []string{
		"test_6_counter_vec_0",
		"test_6_counter_vec_1",
		"test_6_counter_vec_2",
	}

	for _, e := range expected {
		if !sliceContainsStr(registry.Names(), e) {
			t.Errorf("Expected registered specsBefore to contain %s, but it did not.", e)
		}
	}

	registry.Reload(specsAfter)

	regLen = len(registry.Names())
	if regLen != 3 {
		t.Errorf("Expected 3 registered specsAfter, but got %d", regLen)
	}

	expected = []string{
		"test_6_counter_vec_1",
		"test_6_counter_vec_2",
		"test_6_counter_vec_3",
	}

	for _, e := range expected {
		if !sliceContainsStr(registry.Names(), e) {
			t.Errorf("Expected re-registered specsAfter to contain %s, but it did not.", e)
		}
	}
}
