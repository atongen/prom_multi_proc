package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	maxPayloadSize     = 1 << 20 // 1 MiB per connection
	readTimeout        = 5 * time.Second
	maxConcurrentConns = 512
)

var (
	metricsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pmp_metrics_total",
			Help: "Total individual metrics processed, by status (ok/error).",
		},
		[]string{"status"},
	)

	connectionsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "pmp_connections_total",
			Help: "Total connections accepted on the Unix socket.",
		},
	)

	connectionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "pmp_connections_active",
			Help: "Number of connections currently being read.",
		},
	)

	connectionsDroppedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "pmp_connections_dropped_total",
			Help: "Connections dropped while waiting for a slot because the process is shutting down.",
		},
	)

	connectionWaitSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "pmp_connection_wait_seconds",
			Help:    "Time spent waiting for a free in-flight connection slot before handling. A nonzero distribution here indicates saturation of -max-connections.",
			Buckets: []float64{0.0001, 0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
	)

	bytesReceivedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "pmp_bytes_received_total",
			Help: "Total bytes received from client connections.",
		},
	)

	batchSizeMetrics = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "pmp_batch_size_metrics",
			Help:    "Distribution of the number of metrics per successfully parsed batch.",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		},
	)

	registeredMetrics = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "pmp_registered_metrics",
			Help: "Number of user metrics currently registered.",
		},
	)
)

type MetricSpec struct {
	Type       string             `json:"type"`
	Name       string             `json:"name"`
	Help       string             `json:"help"`
	Labels     []string           `json:"labels"`
	Buckets    []float64          `json:"buckets"`
	Objectives map[string]float64 `json:"objectives"`
}

type Metric struct {
	Name        string   `json:"name"`
	LabelValues []string `json:"label_values"`
	Method      string   `json:"method"`
	Value       float64  `json:"value"`
}

// safeLogger wraps *log.Logger with a read-write mutex so the underlying
// writer can be swapped atomically (e.g. on SIGHUP) without data races.
type safeLogger struct {
	mu sync.RWMutex
	l  *log.Logger
	c  io.WriteCloser
}

// logger is the process-wide logger; initialized to discard until SetLogger is called.
var logger = &safeLogger{l: log.New(io.Discard, "", 0)}

func (s *safeLogger) Printf(format string, v ...any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.l.Printf(format, v...)
}

func (s *safeLogger) Println(v ...any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.l.Println(v...)
}

func (s *safeLogger) Fatal(v ...any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.l.Fatal(v...)
}

func (s *safeLogger) set(l *log.Logger, c io.WriteCloser) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		s.c.Close()
	}
	s.l = l
	s.c = c
}

func CountMetric(status string) {
	metricsTotal.WithLabelValues(status).Inc()
}

func SetLogger(file string) error {
	if file == "" {
		// No timestamp prefix: stdout is typically captured by journald or another
		// log aggregator that adds its own timestamp, so prefixing here duplicates it.
		logger.set(log.New(os.Stdout, "", 0), nil)
		return nil
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("error opening log file (%s): %w", file, err)
	}
	logger.set(log.New(f, "", log.LstdFlags), f)
	return nil
}

func LoadSpecs(file string) ([]*MetricSpec, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadSpecs(f)
}

func ReadSpecs(r io.Reader) ([]*MetricSpec, error) {
	jsonBlob, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var result []*MetricSpec
	if err := json.Unmarshal(jsonBlob, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func DataReader(ctx context.Context, ln net.Listener, dataCh chan<- []byte) {
	dataReaderWithLimit(ctx, ln, dataCh, maxConcurrentConns)
}

func dataReaderWithLimit(ctx context.Context, ln net.Listener, dataCh chan<- []byte, limit int) {
	if limit <= 0 {
		limit = maxConcurrentConns
	}
	sem := make(chan struct{}, limit)

	// When the context is cancelled, close the listener so the blocked Accept call
	// returns immediately. This is the only path that should ever close ln, which
	// makes the shutdown signal unambiguous: ctx.Err() != nil means "we did this".
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	logger.Println("Starting listening on socket")
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				logger.Println("DataReader: shutting down")
				return
			}
			CountMetric("error")
			logger.Printf("ERROR (DataReader): %s", err)
			continue
		}
		connectionsTotal.Inc()

		// Block until a slot is free. This restores the backpressure the old
		// serial reader got from the OS socket backlog: clients see brief
		// connect()/read() latency under burst instead of silent EOF, and the
		// 5s per-connection read deadline still bounds how long a slot is held.
		waitStart := time.Now()
		select {
		case sem <- struct{}{}:
			connectionWaitSeconds.Observe(time.Since(waitStart).Seconds())
			connectionsActive.Inc()
			go func() {
				defer func() {
					<-sem
					connectionsActive.Dec()
				}()
				handleConn(c, dataCh)
			}()
		case <-ctx.Done():
			connectionsDroppedTotal.Inc()
			c.Close()
			logger.Println("DataReader: shutting down")
			return
		}
	}
}

func handleConn(c net.Conn, dataCh chan<- []byte) {
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(readTimeout)); err != nil {
		CountMetric("error")
		logger.Printf("ERROR (DataReader SetDeadline): %s", err)
		return
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(c, maxPayloadSize+1))
	if err != nil {
		CountMetric("error")
		logger.Printf("ERROR (DataReader read): %s", err)
		return
	}
	if n > maxPayloadSize {
		CountMetric("error")
		logger.Printf("ERROR (DataReader): payload exceeds %d byte limit", maxPayloadSize)
		return
	}
	bytesReceivedTotal.Add(float64(n))
	dataCh <- buf.Bytes()
}

func DataParser(dataCh <-chan []byte, metricCh chan<- Metric) {
	for {
		var metrics []Metric
		data := <-dataCh
		if err := json.Unmarshal(data, &metrics); err != nil {
			CountMetric("error")
			logger.Printf("ERROR (DataParser): %s", err)
			continue
		}
		batchSizeMetrics.Observe(float64(len(metrics)))
		for _, m := range metrics {
			metricCh <- m
		}
	}
}

func DataProcessor(registry Registry, metricCh <-chan Metric, doneCh <-chan bool) {
	logger.Println("Starting processing data")
	for {
		select {
		case metric := <-metricCh:
			if err := registry.Handle(&metric); err != nil {
				CountMetric("error")
				logger.Printf("ERROR (DataProcessor): %s %+v", err, metric)
				continue
			}
			CountMetric("ok")
		case <-doneCh:
			return
		}
	}
}
