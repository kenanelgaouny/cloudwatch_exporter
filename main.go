package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"os"
	"sync"

	"github.com/Technofy/cloudwatch_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	cache "github.com/victorspringer/http-cache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

var (
	listenAddress = flag.String("web.listen-address", ":9042", "Address on which to expose metrics and web interface.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose exporter's metrics.")
	scrapePath    = flag.String("web.telemetry-scrape-path", "/scrape", "Path under which to expose CloudWatch metrics.")
	configFile    = flag.String("config.file", "config.yml", "Path to configuration file.")

	globalRegistry *prometheus.Registry
	settings       *config.Settings
	totalRequests  prometheus.Counter
	totalErrors    prometheus.Counter
	scrapeDurationHistogramVec    prometheus.HistogramVec
	configMutex    = &sync.Mutex{}
	observers      = map[string]prometheus.ObserverVec{"scrapeDurationHistogram": prometheus.NewHistogramVec(prometheus.HistogramOpts{
					Name: "cloudwatch_exporter_scrape_duration_seconds_buckets",
					Help: "Time this CloudWatch scrape took, in seconds and shown in buckets",
					Buckets: []float64{.25, .5, 1., 2., 5., 8., 16., 30., },}, []string{}),}
	)
	
func loadConfigFile() error {
	var err error
	var tmpSettings *config.Settings
	configMutex.Lock()

	// Initial loading of the configuration file
	tmpSettings, err = config.Load(*configFile)
	if err != nil {
		return err
	}

	generateTemplates(tmpSettings)

	settings = tmpSettings
	configMutex.Unlock()

	return nil
}

// handleReload handles a full reload of the configuration file and regenerates the collector templates.
func handleReload(w http.ResponseWriter, req *http.Request) {
	err := loadConfigFile()
	if err != nil {
		str := fmt.Sprintf("Can't read configuration file: %s", err.Error())
		fmt.Fprintln(w, str)
		fmt.Println(str)
	}
	fmt.Fprintln(w, "Reload complete")
}

// handleTarget handles scrape requests which make use of CloudWatch service
func handleTarget(w http.ResponseWriter, req *http.Request) {
	urlQuery := req.URL.Query()

	target := urlQuery.Get("target")
	task := urlQuery.Get("task")
	region := urlQuery.Get("region")

	// Check if we have all the required parameters in the URL
	if task == "" {
		fmt.Fprintln(w, "Error: Missing task parameter")
		return
	}

	configMutex.Lock()
	registry := prometheus.NewRegistry()
	collector, err := NewCwCollector(target, task, region)
	if err != nil {
		// Can't create the collector, display error
		fmt.Fprintf(w, "Error: %s\n", err.Error())
		configMutex.Unlock()
		return
	}

	registry.MustRegister(collector)
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		DisableCompression: false,
	})

	iHandler := promhttp.InstrumentHandlerDuration(observers["scrapeDurationHistogram"],handler)
	// Serve the answer through the Collect method of the Collector
	iHandler.ServeHTTP(w, req)
	configMutex.Unlock()
}

func main() {
	flag.Parse()

	globalRegistry = prometheus.NewRegistry()

	totalRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudwatch_requests_total",
		Help: "API requests made to CloudWatch",
	})

	totalErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudwatch_errors_total",
		Help: "Failed API requests made to CloudWatch",
	})

	globalRegistry.MustRegister(totalRequests)
	globalRegistry.MustRegister(totalErrors)
	globalRegistry.MustRegister(observers["scrapeDurationHistogram"])

	prometheus.DefaultGatherer = globalRegistry

	err := loadConfigFile()
	if err != nil {
		fmt.Printf("Can't read configuration file: %s\n", err.Error())
		os.Exit(-1)
	}

	memcached, err := memory.NewAdapter(
		memory.AdapterWithAlgorithm(memory.LRU),
		memory.AdapterWithCapacity(100),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cacheClient, err := cache.NewClient(
		cache.ClientWithAdapter(memcached),
		cache.ClientWithTTL(1*time.Minute),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("CloudWatch exporter started...")

	// Expose the exporter's own metrics on /metrics
	http.Handle(*metricsPath, promhttp.Handler())

	// Expose CloudWatch through this endpoint
	scrapeHandler := http.HandlerFunc(handleTarget)
	http.Handle(*scrapePath, cacheClient.Middleware(scrapeHandler))

	// Allows manual reload of the configuration
	http.HandleFunc("/reload", handleReload)

	// Start serving for clients
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
