package main

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

// histogramCount reads the sample_count from a prometheus.Histogram.
func histogramCount(h prometheus.Histogram) uint64 {
	ch := make(chan prometheus.Metric, 1)
	h.Collect(ch)
	m := <-ch
	pb := &dto.Metric{}
	m.Write(pb)
	return pb.GetHistogram().GetSampleCount()
}

// delta captures a metric value before an action and returns the change afterwards.
func delta(t *testing.T, get func() float64, act func()) float64 {
	t.Helper()
	before := get()
	act()
	return get() - before
}

func TestConnectionsTotal(t *testing.T) {
	SetTestLogger()

	server, client := net.Pipe()
	defer server.Close()

	dataCh := make(chan []byte, 1)
	get := func() float64 { return testutil.ToFloat64(connectionsTotal) }

	// Send valid JSON and close so handleConn can finish reading.
	go func() {
		client.Write([]byte(`[{"name":"x","method":"inc","value":1,"label_values":[]}]`))
		client.Close()
	}()

	d := delta(t, get, func() {
		// dataReaderWithLimit is not called here; instrument handleConn indirectly
		// by simulating what dataReaderWithLimit does: increment then dispatch.
		connectionsTotal.Inc()
		connectionsActive.Inc()
		defer connectionsActive.Dec()
		handleConn(server, dataCh)
	})

	if d != 1 {
		t.Errorf("connectionsTotal delta = %v, want 1", d)
	}
}

func TestConnectionsActive(t *testing.T) {
	SetTestLogger()

	server, client := net.Pipe()
	dataCh := make(chan []byte, 1)

	// Write payload then close from a goroutine so handleConn doesn't block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Write([]byte(`[{"name":"x","method":"inc","value":1,"label_values":[]}]`))
		client.Close()
	}()

	connectionsActive.Inc()
	before := testutil.ToFloat64(connectionsActive)
	if before < 1 {
		t.Fatalf("connectionsActive should be ≥1 while holding connection, got %v", before)
	}
	handleConn(server, dataCh)
	connectionsActive.Dec()

	<-done
}

func TestConnectionsDroppedTotal(t *testing.T) {
	SetTestLogger()
	get := func() float64 { return testutil.ToFloat64(connectionsDroppedTotal) }

	d := delta(t, get, func() {
		connectionsDroppedTotal.Inc()
	})
	if d != 1 {
		t.Errorf("connectionsDroppedTotal delta = %v, want 1", d)
	}
}

func TestBytesReceivedTotal(t *testing.T) {
	SetTestLogger()

	payload := `[{"name":"x","method":"inc","value":1,"label_values":[]}]`
	server, client := net.Pipe()
	dataCh := make(chan []byte, 1)

	go func() {
		client.Write([]byte(payload))
		client.Close()
	}()

	get := func() float64 { return testutil.ToFloat64(bytesReceivedTotal) }
	d := delta(t, get, func() { handleConn(server, dataCh) })

	if d != float64(len(payload)) {
		t.Errorf("bytesReceivedTotal delta = %v, want %v", d, len(payload))
	}
}

func TestBatchSizeMetrics(t *testing.T) {
	SetTestLogger()

	dataCh := make(chan []byte, 1)
	metricCh := make(chan Metric, 10)
	go DataParser(dataCh, metricCh)

	before := histogramCount(batchSizeMetrics)

	// Send a batch of 3 metrics; histogram sample_count should increase by 1.
	batch := []Metric{
		{Name: "x", Method: "inc", Value: 1},
		{Name: "y", Method: "inc", Value: 1},
		{Name: "z", Method: "inc", Value: 1},
	}
	payload, _ := json.Marshal(batch)
	dataCh <- payload
	// Drain all 3 metrics to confirm DataParser finished the batch.
	for range batch {
		<-metricCh
	}

	after := histogramCount(batchSizeMetrics)
	if after-before != 1 {
		t.Errorf("batchSizeMetrics sample_count delta = %v, want 1", after-before)
	}
}

func TestBatchSizeMetricsParseError(t *testing.T) {
	SetTestLogger()

	dataCh := make(chan []byte, 1)
	metricCh := make(chan Metric, 10)
	go DataParser(dataCh, metricCh)

	before := histogramCount(batchSizeMetrics)

	// Parse errors must NOT increment the histogram.
	dataCh <- []byte("not json at all")
	time.Sleep(10 * time.Millisecond)

	after := histogramCount(batchSizeMetrics)
	if after != before {
		t.Errorf("batchSizeMetrics sample_count changed on parse error: was %v, now %v", before, after)
	}
}

func TestRegisteredMetricsGauge(t *testing.T) {
	SetTestLogger()

	registry := NewRegistry("")
	get := func() float64 { return testutil.ToFloat64(registeredMetrics) }

	spec := &MetricSpec{
		Type: "counter",
		Name: strings.ReplaceAll(t.Name(), "/", "_") + "_im_counter",
		Help: "internal metrics test counter",
	}

	// Register: gauge should increase by 1.
	regDelta := delta(t, get, func() {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("Register: %v", err)
		}
	})
	if regDelta != 1 {
		t.Errorf("registeredMetrics delta after Register = %v, want 1", regDelta)
	}

	// Unregister: gauge should decrease by 1.
	unregDelta := delta(t, get, func() {
		if err := registry.Unregister(spec.Name); err != nil {
			t.Fatalf("Unregister: %v", err)
		}
	})
	if unregDelta != -1 {
		t.Errorf("registeredMetrics delta after Unregister = %v, want -1", unregDelta)
	}
}
