package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	logCloser io.WriteCloser
	logger    *log.Logger

	metricsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pmp_metrics_total",
			Help: "Total count of metrics processed by status",
		},
		[]string{"status"},
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

func (ms *MetricSpec) Hash() string {
	h := md5.New()
	io.WriteString(h, ms.Type)
	io.WriteString(h, ms.Help)
	io.WriteString(h, fmt.Sprintf("%+v", ms.Labels))
	io.WriteString(h, fmt.Sprintf("%+v", ms.Buckets))
	io.WriteString(h, fmt.Sprintf("%+v", ms.Objectives))
	return fmt.Sprintf("%x", h.Sum(nil))
}

type Metric struct {
	Name        string   `json:"name"`
	LabelValues []string `json:"label_values"`
	Method      string   `json:"method"`
	Value       float64  `json:"value"`
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}

func CountMetric(status string) {
	metricsTotal.WithLabelValues(status).Inc()
}

func SetLogger(file string) error {
	if logCloser != nil {
		logCloser.Close()
	}
	var err error
	if file == "" {
		var b bytes.Buffer
		logCloser = nopCloser{&b}
		logger = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		logCloser, err = os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("Error opening log file (%s): %s", file, err)
		}
		logger = log.New(logCloser, "", log.LstdFlags)
	}
	return nil
}

func LoadSpecs(file string) ([]*MetricSpec, error) {
	var (
		specs []*MetricSpec
		err   error
	)

	specsFile, err := os.OpenFile(file, os.O_RDONLY, 0644)
	if err != nil {
		return specs, err
	}
	defer specsFile.Close()

	return ReadSpecs(specsFile)
}

func ReadSpecs(r io.Reader) ([]*MetricSpec, error) {
	var result []*MetricSpec

	jsonBlob, err := ioutil.ReadAll(r)
	if err != nil {
		return result, err
	}

	err = json.Unmarshal(jsonBlob, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func DataReader(ln net.Listener, dataCh chan<- []byte) {
	logger.Println("Starting listening on socket")
	for {
		// accept a connection
		c, err := ln.Accept()
		if err != nil {
			CountMetric("error")
			logger.Printf("ERROR (DataReader): %s", err)
			continue
		}

		var buf bytes.Buffer
		io.Copy(&buf, c)
		dataCh <- buf.Bytes()
		c.Close()
	}
}

func DataParser(dataCh <-chan []byte, metricCh chan<- Metric) {
	for {
		var metrics []Metric
		data := <-dataCh
		err := json.Unmarshal(data, &metrics)
		if err != nil {
			CountMetric("error")
			logger.Printf("ERROR (DataParser): %s", err)
			continue
		}
		for i := 0; i < len(metrics); i++ {
			metricCh <- metrics[i]
		}
	}
}
