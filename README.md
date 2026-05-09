# prom_multi_proc

Prometheus metrics aggregation for forking servers.

Listens on a unix socket and collects prometheus metrics, then exposes metrics via http
for prometheus scrape.

Addresses issue where a master process forks several workers and each worker collects
his own metrics. When prometheus server scrapes the master, it will return the metrics
from whichever worker serviced the request. This app allows all workers to write
metrics to a single socket, where the metrics can be aggregated.

App servers where this is applicable are unicorn and puma (ruby).

See https://github.com/prometheus/client_ruby/issues/9
for background information.

## Install

Download the [latest release](https://github.com/atongen/prom_multi_proc/releases), extract it,
and put it somewhere on your PATH.

or

```sh
$ go get github.com/atongen/prom_multi_proc
```

or

```sh
$ mkdir -p $GOPATH/src/github.com/atongen
$ cd $GOPATH/src/github.com/atongen
$ git clone git@github.com:atongen/prom_multi_proc.git
$ cd prom_multi_proc
$ go install
$ rehash
```

## Testing

```sh
$ cd $GOPATH/src/github.com/atongen/prom_multi_proc
$ go test -cover
```

## Releases

```sh
$ mkdir -p $GOPATH/src/github.com/atongen
$ cd $GOPATH/src/github.com/atongen
$ git clone git@github.com:atongen/prom_multi_proc.git
$ cd prom_multi_proc
$ make release
```

## Command-Line Options

```
Usage of prom_multi_proc:
  -addr string
        Address to listen on for exposing prometheus metrics (default "0.0.0.0:9299")
  -log string
        Path to log file, will write to STDOUT if empty
  -metric-prefix string
        Prefix to prepend to metric names (e.g. "myapp" or "myapp_"); a trailing "_" is
        added automatically if omitted. Skipped for metrics whose name already starts with
        the prefix.
  -metrics string
        Path to json file which contains metric definitions
  -path string
        Path to use for exposing prometheus metrics (default "/metrics")
  -socket string
        Path to unix socket to listen on for incoming metrics (default "/tmp/prom_multi_proc.sock")
  -socket-mode string
        File mode for the unix socket (octal); 0666 allows any local user to connect (default "0666")
  -v    Print version information and exit
```

## Operations

Send the process a `HUP` signal to re-open log files.

Send the process a `USR1` signal to re-load metrics configuration json file.
Note that only new metrics can be added and existing metrics can be removed.
Changes to existing metrics will be ignored.
