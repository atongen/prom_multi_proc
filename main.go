package main

import (
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
	socketFlag     = flag.String("socket", "/tmp/prom_multi_proc.sock", "Path to unix socket to listen on for incoming metrics")
	socketModeFlag = flag.String("socket-mode", "0666", "File mode for the unix socket (octal); 0666 allows any local user to connect")
	metricsFlag    = flag.String("metrics", "", "Path to json file which contains metric definitions")
	addrFlag       = flag.String("addr", "0.0.0.0:9299", "Address to listen on for exposing prometheus metrics")
	pathFlag       = flag.String("path", "/metrics", "Path to use for exposing prometheus metrics")
	logFlag        = flag.String("log", "", "Path to log file, will write to STDOUT if empty")
	versionFlag    = flag.Bool("v", false, "Print version information and exit")
)

func init() {
	prometheus.MustRegister(metricsTotal)
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

	ln, err := net.Listen("unix", *socketFlag)
	if err != nil {
		logger.Fatal(err)
	}
	defer ln.Close()

	if err := os.Chmod(*socketFlag, os.FileMode(socketMode)); err != nil {
		logger.Fatal(err)
	}
	logger.Printf("Socket ready: %s (mode %04o)", *socketFlag, socketMode)

	// listen for signals which make us quit
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sigc
		logger.Println("Goodbye!")
		ln.Close()
		os.Exit(0)
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

	registry := NewRegistry()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("Recovered panic: %s", r)
				ln.Close()
				os.Exit(1)
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
				ln.Close()
				os.Exit(1)
			}
		}
	}()

	workers := runtime.NumCPU()
	for i := 0; i < workers; i++ {
		go DataParser(dataCh, metricCh)
	}

	go DataReader(ln, dataCh)

	promHandler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ErrorLog: logger,
	})
	http.Handle(*pathFlag, promHandler)
	if err := http.ListenAndServe(*addrFlag, nil); err != nil {
		logger.Fatal(err)
	}
}
