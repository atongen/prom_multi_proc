package main

// DataReader concurrency benchmark.
//
// Run on each branch and compare with benchstat:
//
//   git checkout benchmark-baseline
//   go test -run='^$' -bench='^BenchmarkDataReader' -benchtime=5s -count=6 ./... > baseline.txt
//
//   git checkout updates
//   go test -run='^$' -bench='^BenchmarkDataReader' -benchtime=5s -count=6 ./... > updates.txt
//
//   go tool benchstat baseline.txt updates.txt
//
// Install benchstat if needed:
//   go install golang.org/x/perf/cmd/benchstat@latest

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
)

// benchPayload is a realistic JSON metric batch (~180 bytes).
var benchPayload = []byte(
	`[{"name":"b_gauge","method":"set","value":1.0},` +
		`{"name":"b_gauge","method":"set","value":2.0},` +
		`{"name":"b_gauge","method":"set","value":3.0},` +
		`{"name":"b_gauge","method":"set","value":4.0},` +
		`{"name":"b_gauge","method":"set","value":5.0}]`,
)

// benchmarkDataReader sends numConns connections concurrently per iteration
// and waits until all payloads have been received through dataCh.
//
// On benchmark-baseline the DataReader accepts one connection at a time
// (serial), so N concurrent connections are processed in sequence.
// On the updates branch they are handled in parallel goroutines.
func benchmarkDataReader(b *testing.B, numConns int) {
	b.Helper()
	SetTestLogger()

	sockPath := fmt.Sprintf("/tmp/pmp_bench_dr_%d.sock", os.Getpid())
	os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		ln.Close()
		os.Remove(sockPath)
	})

	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)

	dataCh := make(chan []byte, numConns*4)
	go DataReader(ctx, ln, dataCh)

	b.SetBytes(int64(numConns) * int64(len(benchPayload)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < numConns; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := net.Dial("unix", sockPath)
				if err != nil {
					return
				}
				c.Write(benchPayload)
				c.Close()
			}()
		}
		// Block until all numConns payloads arrive in dataCh.
		for j := 0; j < numConns; j++ {
			<-dataCh
		}
		wg.Wait()
	}
}

func BenchmarkDataReader_1conn(b *testing.B)   { benchmarkDataReader(b, 1) }
func BenchmarkDataReader_5conn(b *testing.B)   { benchmarkDataReader(b, 5) }
func BenchmarkDataReader_20conn(b *testing.B)  { benchmarkDataReader(b, 20) }
func BenchmarkDataReader_100conn(b *testing.B) { benchmarkDataReader(b, 100) }
