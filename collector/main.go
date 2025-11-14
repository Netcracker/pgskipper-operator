// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/initiate"
	pgScraper "github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/metrics"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	addr = flag.String("localhost", ":9273",
		"The address to listen on for HTTP requests.")
	timeout = util.GetEnv("METRIC_COLLECTION_INTERVAL", "60")

	metrics      = make([]string, 0)
	metricsMutex sync.RWMutex
	metricsReady bool
)

func collectMetrics(scraper *pgScraper.Scraper, dr bool) {
	log.Printf("Start collecting metrics")

	scraper.CollectCommonMetrics()
	scraper.CollectMetrics()
	scraper.CollectBackupMetrics()
	if dr {
		scraper.CollectDRMetrics()
	}

	mode := util.GetEnv("METRICS_PROFILE", "prod")
	if mode == "dev" {
		scraper.CollectPerformanceMetrics()
	}

	scraper.CollectMetricsFromCM()
	metricsMutex.Lock()
	metrics = scraper.PrintMetrics()
	metricsReady = true
	metricsMutex.Unlock()
}

func Metrics(w http.ResponseWriter, r *http.Request) {
	log.Printf("metrics gets requested")
	metricsMutex.RLock()
	defer metricsMutex.RUnlock()
	fmt.Fprintf(w, "%s", strings.Join(metrics, "\n"))
}

func LivenessProbe(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func ReadinessProbe(w http.ResponseWriter, r *http.Request) {
	metricsMutex.RLock()
	ready := metricsReady
	metricsMutex.RUnlock()

	if ready {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "Not ready")
	}
}

func HttpHandler() {
	// Registering our handler functions, and creating paths.
	http.HandleFunc("/metrics", Metrics)
	http.HandleFunc("/healthz", LivenessProbe)
	http.HandleFunc("/ready", ReadinessProbe)
	log.Println("Started on port", *addr)
	// Spinning up the server.
	err := http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	log.Printf("Init Metric collector")
	initiate.InitMetricCollector()
	log.Println("Starting http server")
	go HttpHandler()

	scrapeTimeout, _ := strconv.Atoi(timeout)
	protocol, port := util.GetProtocol()
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	dr := util.IsSiteManagerEnabled()
	if dr {
		log.Println("DR mode is enabled")
	}

	for {
		scraper := pgScraper.GetScraper(client, util.GetHttpClient(), protocol, port)
		collectMetrics(scraper, dr)
		log.Printf("Timeout %v", scrapeTimeout)
		time.Sleep(time.Duration(scrapeTimeout) * time.Second)
	}
}
