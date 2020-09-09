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
	socketFlag  = flag.String("socket", "/tmp/prom_multi_proc.sock", "Path to unix socket to listen on for incoming metrics")
	metricsFlag = flag.String("metrics", "", "Path to json file which contains metric definitions")
	addrFlag    = flag.String("addr", "0.0.0.0:9299", "Address to listen on for exposing prometheus metrics")
	pathFlag    = flag.String("path", "/metrics", "Path to use for exposing prometheus metrics")
	logFlag     = flag.String("log", "", "Path to log file, will write to STDOUT if empty")
	versionFlag = flag.Bool("v", false, "Print version information and exit")
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

	// setup logger, this may be reloaded later with HUP signal
	err := SetLogger(*logFlag)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// setup metrics and done channels
	metricCh := make(chan Metric)
	dataCh := make(chan []byte)
	doneCh := make(chan bool)

	// begin listening on socket
	ln, err := net.Listen("unix", *socketFlag)
	if err != nil {
		logger.Fatal(err)
	}
	defer ln.Close()

	err = os.Chmod(*socketFlag, 0777)
	if err != nil {
		logger.Fatal(err)
	}

	// listen for signals which make us quit
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)
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
			// stop the data processor
			doneCh <- true
		}
	}()

	registry := NewRegistry()

	go func() {
		for {
			logger.Println(versionStr())
			logger.Println("Loading metric configuration")

			// note beginning names of metrics
			names := registry.Names()

			// reload metrics definitions file
			specs, err := LoadSpecs(*metricsFlag)
			if err != nil {
				logger.Printf("Error loading configuration: %s", err)
			} else {
				// only register/unregister if there is no error processing
				// the metrics definition json
				newNames := []string{}
				for _, spec := range specs {
					newNames = append(newNames, spec.Name)
					if err := registry.Register(spec); err != nil {
						logger.Println(err)
					} else {
						logger.Printf("Registered %s", spec.Name)
					}
				}

				// get names of metrics no longer present and unregister them
				unreg := sliceSubStr(names, newNames)
				for _, name := range unreg {
					if err := registry.Unregister(name); err != nil {
						logger.Println(err)
					} else {
						logger.Printf("Unregistered %s", name)
					}
				}
			}

			// begin processing incoming metrics
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
			err := SetLogger(*logFlag)
			if err != nil {
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

	// setup prometheus http handlers and begin listening
	promHandler := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ErrorLog: logger,
	})
	http.Handle(*pathFlag, promHandler)
	http.ListenAndServe(*addrFlag, nil)
}
