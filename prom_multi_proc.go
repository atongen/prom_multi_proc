package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	maxConcurrentConns = 64
)

var metricsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "pmp_metrics_total",
		Help: "Total count of metrics processed by status",
	},
	[]string{"status"},
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

func DataReader(ln net.Listener, dataCh chan<- []byte) {
	dataReaderWithLimit(ln, dataCh, maxConcurrentConns)
}

func dataReaderWithLimit(ln net.Listener, dataCh chan<- []byte, limit int) {
	sem := make(chan struct{}, limit)
	logger.Println("Starting listening on socket")
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logger.Println("DataReader: listener closed")
				return
			}
			CountMetric("error")
			logger.Printf("ERROR (DataReader): %s", err)
			continue
		}
		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				handleConn(c, dataCh)
			}()
		default:
			CountMetric("error")
			logger.Printf("ERROR (DataReader): connection limit %d reached, dropping connection", limit)
			c.Close()
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
