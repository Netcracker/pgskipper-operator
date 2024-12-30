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

	metrics = make([]string, 0)
)

func collectMetrics(scraper *pgScraper.Scraper) {
	log.Printf("Start collecting metrics")

	scraper.CollectCommonMetrics()
	scraper.CollectMetrics()
	scraper.CollectBackupMetrics()

	mode := util.GetEnv("METRICS_PROFILE", "prod")
	if mode == "dev" {
		scraper.CollectPerformanceMetrics()
	}

	scraper.CollectMetricsFromCM()
	metrics = scraper.PrintMetrics()
}

func Metrics(w http.ResponseWriter, r *http.Request) {
	log.Printf("metrics gets requested")
	fmt.Fprintf(w, "%s", strings.Join(metrics, "\n"))
}

func HttpHandler() {
	// Registering our handler functions, and creating paths.
	http.HandleFunc("/metrics", Metrics)
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

	for {
		scraper := pgScraper.GetScraper(client, util.GetHttpClient(), protocol, port)
		collectMetrics(scraper)
		log.Printf("Timeout %v", scrapeTimeout)
		time.Sleep(time.Duration(scrapeTimeout) * time.Second)
	}
}
