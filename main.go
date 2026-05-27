package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// build flags
var (
	Version   string = "development"
	BuildTime string = "unset"
	BuildHash string = "unset"
	GoVersion string = "unset"
)

// cli flags
var (
	socketFlag       = flag.String("socket", "/tmp/prom_multi_proc.sock", "Path to unix socket to listen on for incoming metrics")
	socketModeFlag   = flag.String("socket-mode", "0666", "File mode for the unix socket (octal); 0666 allows any local user to connect")
	metricsFlag      = flag.String("metrics", "", "Path to json file which contains metric definitions")
	addrFlag         = flag.String("addr", "0.0.0.0:9299", "Address to listen on for exposing prometheus metrics")
	pathFlag         = flag.String("path", "/metrics", "Path to use for exposing prometheus metrics")
	logFlag          = flag.String("log", "", "Path to log file, will write to STDOUT if empty")
	metricPrefixFlag = flag.String("metric-prefix", "", "Prefix to prepend to metric names (e.g. \"myapp\" or \"myapp_\"); a trailing \"_\" is added automatically if omitted")
	maxConnsFlag     = flag.Int("max-connections", 512, "Maximum concurrent in-flight client connections; additional clients block until a slot frees")
	versionFlag      = flag.Bool("v", false, "Print version information and exit")
)

func init() {
	prometheus.MustRegister(
		metricsTotal,
		connectionsTotal,
		connectionsActive,
		connectionsDroppedTotal,
		connectionWaitSeconds,
		bytesReceivedTotal,
		batchSizeMetrics,
		registeredMetrics,
	)
}

func versionStr() string {
	return fmt.Sprintf("%s %s %s %s %s", path.Base(os.Args[0]), Version, BuildTime, BuildHash, GoVersion)
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Println(versionStr())
		os.Exit(0)
	}

	if err := SetLogger(*logFlag); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	prefix := normalizePrefix(*metricPrefixFlag)
	if err := validatePrefix(prefix); err != nil {
		fmt.Fprintf(os.Stderr, "invalid -metric-prefix %q: %v\n", *metricPrefixFlag, err)
		os.Exit(1)
	}

	metricCh := make(chan Metric, 1024)
	dataCh := make(chan []byte, 256)
	doneCh := make(chan bool, 1)

	socketMode, err := strconv.ParseUint(*socketModeFlag, 8, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -socket-mode %q (must be octal, e.g. 0666): %v\n", *socketModeFlag, err)
		os.Exit(1)
	}
	if socketMode > 0o7777 {
		fmt.Fprintf(os.Stderr, "invalid -socket-mode %q: value must be in octal range 0000–7777\n", *socketModeFlag)
		os.Exit(1)
	}
	if *maxConnsFlag <= 0 {
		fmt.Fprintf(os.Stderr, "invalid -max-connections %d: must be > 0\n", *maxConnsFlag)
		os.Exit(1)
	}

	ln, err := net.Listen("unix", *socketFlag)
	if err != nil {
		logger.Fatal(err)
	}

	if err := os.Chmod(*socketFlag, os.FileMode(socketMode)); err != nil {
		logger.Fatal(err)
	}
	logger.Printf("Socket ready: %s (mode %04o)", *socketFlag, socketMode)

	// ctx drives DataReader shutdown. Cancelling it closes the listener and
	// causes the accept loop to exit cleanly without spinning on an Accept
	// error. readerDone is closed once DataReader returns.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readerDone := make(chan struct{})

	// shutdown signals DataReader to stop, waits briefly for it to drain, then
	// exits with the given code. Used by every path that previously called
	// os.Exit directly while holding the listener open.
	shutdown := func(code int) {
		cancel()
		select {
		case <-readerDone:
		case <-time.After(time.Second):
			logger.Println("DataReader did not exit within 1s; forcing exit")
		}
		os.Exit(code)
	}

	// listen for signals which make us quit
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sigc
		logger.Println("Goodbye!")
		shutdown(0)
	}()

	// listen for USR1 signal which makes us reload our metrics definitions
	sigu := make(chan os.Signal, 1)
	signal.Notify(sigu, syscall.SIGUSR1)
	go func() {
		for {
			<-sigu
			logger.Println("USR1 Signal received")
			doneCh <- true
		}
	}()

	registry := NewRegistry(prefix)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("Recovered panic: %s", r)
				shutdown(1)
			}
		}()
		// this for loop must always either continue, or exit the process;
		// breaking out would stop data processing and prevent USR1 reloads.
		for {
			logger.Println(versionStr())
			logger.Println("Loading metric configuration")

			names := registry.Names()

			specs, err := LoadSpecs(*metricsFlag)
			if err != nil {
				logger.Printf("Error loading configuration: %s", err)
			} else {
				newNames := []string{}
				for _, spec := range specs {
					newNames = append(newNames, spec.Name)
					if err := registry.Register(spec); err != nil {
						logger.Println(err)
					} else {
						logger.Printf("Registered %s", spec.Name)
					}
				}

				for _, name := range sliceSubStr(names, newNames) {
					if err := registry.Unregister(name); err != nil {
						logger.Println(err)
					} else {
						logger.Printf("Unregistered %s", name)
					}
				}
			}

			DataProcessor(registry, metricCh, doneCh)
		}
	}()

	// listen for HUP signal which makes us reopen our log file descriptors
	sigh := make(chan os.Signal, 1)
	signal.Notify(sigh, syscall.SIGHUP)
	go func() {
		for {
			<-sigh
			logger.Println("Re-opening logs...")
			if err := SetLogger(*logFlag); err != nil {
				fmt.Println(err)
				shutdown(1)
			}
		}
	}()

	workers := runtime.NumCPU()
	for i := 0; i < workers; i++ {
		go DataParser(dataCh, metricCh)
	}

	go func() {
		defer close(readerDone)
		dataReaderWithLimit(ctx, ln, dataCh, *maxConnsFlag)
	}()

	promHandler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ErrorLog: logger,
	})
	http.Handle(*pathFlag, promHandler)
	if err := http.ListenAndServe(*addrFlag, nil); err != nil {
		logger.Fatal(err)
	}
}
