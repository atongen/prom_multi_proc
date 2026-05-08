package main

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestReadSpecsValid(t *testing.T) {
	input := `[
		{"type":"counter","name":"rs_counter","help":"Help"},
		{"type":"gauge","name":"rs_gauge","help":"Help","labels":["a","b"]}
	]`
	specs, err := ReadSpecs(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Type != "counter" || specs[0].Name != "rs_counter" {
		t.Errorf("unexpected spec[0]: %+v", specs[0])
	}
	if specs[1].Labels[0] != "a" || specs[1].Labels[1] != "b" {
		t.Errorf("unexpected spec[1] labels: %+v", specs[1].Labels)
	}
}

func TestReadSpecsInvalidJSON(t *testing.T) {
	if _, err := ReadSpecs(strings.NewReader("not json")); err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}

func TestSetLogger(t *testing.T) {
	// Empty path should write to stdout (no error).
	if err := SetLogger(""); err != nil {
		t.Fatalf("SetLogger(\"\") returned error: %v", err)
	}

	// Non-existent directory should return error.
	if err := SetLogger("/no/such/dir/pmp.log"); err == nil {
		t.Fatal("Expected error for bad log path, got nil")
	}

	// Restore discard logger for other tests.
	SetTestLogger()
}

func TestDataReaderConcurrentConnections(t *testing.T) {
	SetTestLogger()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	dataCh := make(chan []byte, 10)
	go DataReader(ln, dataCh)

	payload := `[{"name":"dr_counter","method":"inc"}]`
	conns := 3
	for i := 0; i < conns; i++ {
		go func() {
			c, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				return
			}
			c.Write([]byte(payload))
			c.Close()
		}()
	}

	received := 0
	timeout := time.After(2 * time.Second)
	for received < conns {
		select {
		case <-dataCh:
			received++
		case <-timeout:
			t.Fatalf("timed out waiting for data: got %d of %d", received, conns)
		}
	}
}

func TestHandleConnPayloadLimit(t *testing.T) {
	SetTestLogger()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	dataCh := make(chan []byte, 2)
	go DataReader(ln, dataCh)

	// Send a payload that exceeds maxPayloadSize — dataCh should NOT receive it.
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	oversized := make([]byte, maxPayloadSize+2)
	c.Write(oversized)
	c.Close()

	select {
	case <-dataCh:
		t.Fatal("oversized payload should not have been forwarded to dataCh")
	case <-time.After(300 * time.Millisecond):
		// expected: nothing forwarded
	}
}

func TestDataProcessorDoneCh(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	metricCh := make(chan Metric, 1)
	doneCh := make(chan bool, 1)

	done := make(chan struct{})
	go func() {
		DataProcessor(registry, metricCh, doneCh)
		close(done)
	}()

	doneCh <- true

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("DataProcessor did not exit after doneCh signal")
	}
}

func TestDataProcessorHandlesMetric(t *testing.T) {
	SetTestLogger()
	registry := NewRegistry()
	spec := &MetricSpec{Type: "counter", Name: "dp_counter", Help: "Help"}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}

	metricCh := make(chan Metric, 1)
	doneCh := make(chan bool, 1)

	go DataProcessor(registry, metricCh, doneCh)

	metricCh <- Metric{Name: "dp_counter", Method: "inc"}
	// Give the processor a moment then stop it.
	time.Sleep(50 * time.Millisecond)
	doneCh <- true
}

func TestDataReaderConnectionLimit(t *testing.T) {
	SetTestLogger()

	const limit = 2

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	dataCh := make(chan []byte, limit+1)
	go dataReaderWithLimit(ln, dataCh, limit)

	// Open `limit` connections and keep them open (no EOF) so they hold semaphore
	// slots inside handleConn for the duration of the test.
	holders := make([]net.Conn, limit)
	for i := range holders {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		holders[i] = c
	}
	defer func() {
		for _, c := range holders {
			c.Close()
		}
	}()

	// Give handleConn goroutines time to start and acquire their semaphore slots.
	time.Sleep(20 * time.Millisecond)

	// This connection should be immediately closed by the server (limit reached).
	extra, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()

	extra.SetDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	_, readErr := extra.Read(buf)
	if readErr == nil {
		t.Fatal("expected server to close the connection when limit is reached, but Read succeeded")
	}
}
