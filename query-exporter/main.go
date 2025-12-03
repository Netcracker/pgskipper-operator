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
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Netcracker/qubership-query-exporter/collectors"
	"github.com/Netcracker/qubership-query-exporter/config"
	"github.com/Netcracker/qubership-query-exporter/dbservice"
	"github.com/Netcracker/qubership-query-exporter/logger"
	"github.com/Netcracker/qubership-query-exporter/monstate"
	"github.com/Netcracker/qubership-query-exporter/scheduler"
	"github.com/Netcracker/qubership-query-exporter/selfmonitor"
	"github.com/Netcracker/qubership-query-exporter/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

var (
	printVersion             = flag.Bool("version", false, "Print the query-exporter version and exit")
	addr                     = flag.String("listen-address", ":8080", "The address to listen (port) for HTTP requests")
	configPath               = flag.String("config-path", utils.GetEnv("QUERY_EXPORTER_EXTEND_QUERY_PATH", "config.yaml"), "Path to the yaml configuration")
	histSummarize            = flag.Bool("histogram-sum", utils.GetEnvBool("HISTOGRAM_SUMMARIZE", true), "If histogram values should be summarized")
	excQueriesFromEnv        = flag.String("excluded-queries", utils.GetEnv("EXCLUDED_QUERIES", ""), "Excluded queries list")
	appConfig                *config.Config
	gaugeVecRepository       = utils.GaugeVecRepository{}
	dynamicMetricsRepository = utils.DynamicMetricsRepository{}
	counterVecs              = make(map[string]*prometheus.CounterVec)
	histogramVecs            = make(map[string]*collectors.CustomHistogram)
	omnipresentLabels        map[string]string
	omnipresentLabelsKeys    []string
	croniter                 *cron.Cron
	monitoringState          = monstate.MonitoringState{}
	metricDefaultValuesRepo  *monstate.MetricDefaultValuesRepository
	dbServices               []dbservice.DBService
	nonScheduledQueries      []string
	osIntSignals             = make(chan os.Signal, 1)
	osQuitSignals            = make(chan os.Signal, 1)
	dbErrorHandlingEnabled   = false

	excludedQueries []string
	excludedMetrics []string
)

func initPrometheus() {
	omnipresentLabels = appConfig.Db.Labels
	omnipresentLabelsKeys = utils.GetKeys(omnipresentLabels)
	gaugeVecRepository.Initialize()
	dynamicMetricsRepository.Initialize()

	for metricName, metricConfig := range appConfig.Metrics {
		for _, excMetric := range excludedMetrics {
			if metricName == excMetric {
				continue
			}
		}

		labels := append(metricConfig.Labels, omnipresentLabelsKeys...)
		switch metricConfig.Type {
		case "gauge":
			gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name:        metricName,
				Help:        metricConfig.Description,
				ConstLabels: metricConfig.ConstLabels,
			}, labels)
			prometheus.MustRegister(gaugeVec)
			gaugeVecRepository.Set(metricName, gaugeVec)
			log.Infof("gaugeVec %v registered with labels %+v", metricName, labels)
		case "counter":
			counterVec := prometheus.NewCounterVec(prometheus.CounterOpts{
				Name:        metricName,
				Help:        metricConfig.Description,
				ConstLabels: metricConfig.ConstLabels,
			}, labels)
			prometheus.MustRegister(counterVec)
			counterVecs[metricName] = counterVec
			log.Infof("counterVec %v registered with labels %+v", metricName, labels)
			initCounterMetric(metricConfig, metricName, counterVec)
		case "histogram":
			customHistogram := collectors.NewCustomHistogram(
				prometheus.NewDesc(
					metricName,
					metricConfig.Description,
					labels,
					metricConfig.ConstLabels,
				),
				isSummarize(metricConfig.Summarize),
			)
			histogramVecs[metricName] = customHistogram
			prometheus.MustRegister(customHistogram)
			log.Infof("customHistogram %v registered with labels %+v", metricName, labels)
			initHistogramMetric(metricConfig, metricName, customHistogram)
		default:
			log.Errorf("Metric %v has not supported type %v", metricName, metricConfig.Type)
		}
	}
}

func initCounterMetric(metricConfig *config.MetricsConfig, metricName string, counterVec *prometheus.CounterVec) {
	initValue := metricConfig.Parameters["init-value"]
	if initValue == "" {
		return
	}
	if len(metricConfig.Labels) == 0 {
		if strings.ToUpper(initValue) == "NAN" {
			counterVec.With(omnipresentLabels).Add(math.NaN())
			log.Infof("Metric %v is initialized with value NaN", metricName)
		} else {
			initValueFloat, err := strconv.ParseFloat(initValue, 64)
			if err == nil {
				if initValueFloat >= 0 {
					counterVec.With(omnipresentLabels).Add(initValueFloat)
					log.Infof("Metric %v is initialized with value %v", metricName, initValue)
				} else {
					log.Infof("Counter metric %v can not be initialized with negative value %v", metricName, initValue)
				}
			} else {
				log.Errorf("Error parsing init-value %v for metric %v : %+v", initValue, metricName, err)
			}
		}
	} else if len(metricConfig.ExpectedLabels) > 0 {
		initValueFloat, err := strconv.ParseFloat(initValue, 64)
		if err == nil {
			if initValueFloat < 0 {
				log.Errorf("Counter metric %v can not be initialized with negative value %v", metricName, initValue)
				return
			}
		} else {
			log.Errorf("Error parsing init-value %v for metric %v : %+v", initValue, metricName, err)
			return
		}
		for itemNum, expectedLabelsItem := range metricConfig.ExpectedLabels {
			cartesian := utils.LabelsCartesian(expectedLabelsItem)
			log.Infof("For metric %v (itemNum %v) expected labels cartesian generated : %+v", metricName, itemNum, cartesian)
			for _, labels := range cartesian {
				for labelName, labelValue := range omnipresentLabels {
					labels[labelName] = labelValue
				}
				counterVec.With(labels).Add(initValueFloat)
			}
		}
	} else {
		log.Errorf("Metric %v can not be initialized because it has labels and expected labels are not defined", metricName)
	}
}

func initHistogramMetric(metricConfig *config.MetricsConfig, metricName string, customHistogram *collectors.CustomHistogram) {
	initValue := metricConfig.Parameters["init-value"]
	if initValue == "" {
		return
	}
	buckets := make(map[float64]uint64)
	for _, bucketKey := range metricConfig.Buckets {
		buckets[bucketKey] = 0
	}
	buckets[math.Inf(1.0)] = 0
	if len(metricConfig.Labels) == 0 {
		customHistogram.Observe(0, 0, buckets, omnipresentLabels, omnipresentLabelsKeys)
	} else if len(metricConfig.ExpectedLabels) > 0 {
		for itemNum, expectedLabelsItem := range metricConfig.ExpectedLabels {
			cartesian := utils.LabelsCartesian(expectedLabelsItem)
			log.Infof("For metric %v (itemNum %v) expected labels cartesian generated : %+v", metricName, itemNum, cartesian)
			for _, labels := range cartesian {
				for labelName, labelValue := range omnipresentLabels {
					labels[labelName] = labelValue
				}
				customHistogram.Observe(0, 0, buckets, labels, append(metricConfig.Labels, omnipresentLabelsKeys...))
			}
		}
	} else {
		log.Errorf("Metric %v can not be initialized because it has labels and expected labels are not defined", metricName)
	}
}

func initErrorHandling() {
	if appConfig.ErrorHandling != nil && len(appConfig.ErrorHandling.DbErrorRules) > 0 {
		dbErrorHandlingEnabled = true
	}
}

func registerGaugeDynamically(metricName string, labels []string) {
	allLabels := append(labels, omnipresentLabelsKeys...)
	gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: metricName,
		Help: "Dynamic gauge without description",
	}, allLabels)
	prometheus.MustRegister(gaugeVec)
	gaugeVecRepository.Set(metricName, gaugeVec)
	log.Warnf("Dynamic gaugeVec %v registered with labels %+v", metricName, allLabels)
}

func initMonitoringState() {
	monitoringState.Initialize()
	for metricName, metricCfg := range appConfig.Metrics {
		if metricCfg.Type != "gauge" {
			continue
		}
		metricState := monstate.MetricState{}
		metricState.Initialize()
		for itemNum, expectedLabelsItem := range metricCfg.ExpectedLabels {
			cartesian := utils.LabelsCartesian(expectedLabelsItem)
			log.Infof("For metric %v (itemNum %v) expected labels cartesian generated : %+v", metricName, itemNum, cartesian)
			for _, labels := range cartesian {
				for labelName, labelValue := range omnipresentLabels {
					labels[labelName] = labelValue
				}
				metricState.Set(utils.MapToString(labels), labels)
			}
		}
		monitoringState.Set(metricName, &metricState)
		log.Debugf("New metric %v registered in monitoringState map", metricName)
	}
	metricDefaultValuesRepo = monstate.CreateMetricDefaultValuesRepository(appConfig.Metrics)
}

func main() {
	flag.Parse()
	if *printVersion {
		fmt.Printf("%v\n", versionString())
		return
	}
	signal.Notify(osIntSignals, syscall.SIGINT, syscall.SIGTERM)
	signal.Notify(osQuitSignals, syscall.SIGQUIT)
	go interruptionHandler()
	go quitHandler()

	defer func() {
		stopCroniter()
		log.Info("QUERY EXPORTER STOPPED, no signal received")
		fmt.Printf("\nstop query-exporter, %v\n", versionString())
	}()
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("Panic in main : %+v ; Stacktrace of the panic : %v", rec, string(debug.Stack()))
		}
	}()

	fmt.Printf("start query-exporter, %v\n", versionString())

	logger.ConfigureLog()

	log.Infof("QUERY EXPORTER STARTED; %v", versionString())

	var err error
	appConfig, err = config.Read(*configPath)
	if err != nil {
		log.Fatalf("Fatal: %v", err)
		return
	}
	reapplyFlags()
	initErrorHandling()
	initExcludedQueries()
	croniter = utils.GetCron()

	initPrometheus()
	initMonitoringState()
	selfmonitor.InitSelfMonitoring(appConfig)

	dbServices = dbservice.GetDBServices(appConfig)
	defer func() {
		go hardstopAfterTimeout(time.Minute)
		for _, dbService := range dbServices {
			dbService.Close()
		}
	}()
	//dbService.PrintSimpleQuery("select sys_context( 'userenv', 'current_schema' ) from dual")

	nonScheduledQueries = scheduler.Schedule(croniter, appConfig, executeQueryAndUpdateMetrics, updateDatabases)

	http.HandleFunc("/metrics", httpHandlerFunc)
	http.HandleFunc("/health", health)

	log.Error(http.ListenAndServe(*addr, nil))
}

func initExcludedQueries() {
	excludedQueriesArr := strings.Split(*excQueriesFromEnv, ",")
	for qName, qcontent := range appConfig.Queries {
		for _, excludedQuery := range excludedQueriesArr {
			if qName == excludedQuery {
				log.Infof("query %s was excluded by initial config", qName)
				excludedQueries = append(excludedQueries, qName)
				for _, m := range qcontent.Metrics {
					log.Infof("metric %s was excluded by initial config", m)
					excludedMetrics = append(excludedMetrics, m)
				}
			}
		}
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	dbService := dbServices[0]
	if dbService != nil {
		if dbService.Ping() {
			w.Write([]byte("OK"))
		}
	} else {
		w.WriteHeader(503)
	}
}

func httpHandlerFunc(w http.ResponseWriter, r *http.Request) {
	log.Debugf("HttpHandler started, %v non-scheduled queries need to be executed: %+v", len(nonScheduledQueries), nonScheduledQueries)
	defer log.Debug("HttpHandler finished")

	for _, qName := range nonScheduledQueries {
		executeQueryAndUpdateMetrics(qName)
	}

	promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{},
	).ServeHTTP(w, r)
}

func executeQueryAndUpdateMetrics(qName string) {
	startTime := time.Now()
	qRand := utils.GenerateRandomString(10)
	var rows []map[string]string
	defer func() {
		log.Debugf("[%v] Update job for %v executed in %+v ; total rows returned : %v", qRand, qName, time.Since(startTime), len(rows))
	}()
	defer func() {
		if rec := recover(); rec != nil {
			log.Errorf("[%v] Panic during execution of query %v: %+v ; Stacktrace of the panic : %v", qRand, qName, rec, string(debug.Stack()))
		}
	}()

	qCfg := appConfig.Queries[qName]

	queryExecutedSuccessfully := true
	totalUpdatedMetricLabelValues := make(map[string]map[string]prometheus.Labels)
	defer func() {
		if len(qCfg.Metrics) != 0 {
			resetNotUpdatedGauges(totalUpdatedMetricLabelValues, qCfg.Metrics, queryExecutedSuccessfully, qRand)
		} else {
			metricSet := dynamicMetricsRepository.Get(qName)
			if metricSet != nil {
				metrics := metricSet.GetAllEntries()
				log.Debugf("[%v] Resetting not updated gauges to NaN for dynamic query %v, metrics list to check is %+v", qRand, qName, metrics)
				resetNotUpdatedGauges(totalUpdatedMetricLabelValues, metrics, queryExecutedSuccessfully, qRand)
			} else {
				log.Warnf("[%v] Metric Set for dynamic query %v is nil, resetting not updated gauges to NaN can not be performed", qRand, qName)
			}
		}
	}()
	dbSrv := dbServices
	for _, dbService := range dbSrv {
		if skipQuery(dbService, qName, qCfg) {
			continue
		}
		timeBeforeQueryExecution := time.Now()
		rows, columnNames, err := dbService.ExecuteSelect(qName, qRand, qCfg)
		selfmonitor.QueryLatencyExecution(timeBeforeQueryExecution, qName, dbService.GetDatabaseName())
		if err != nil {
			log.Errorf("[%v] Error running query %s : %+v", qRand, qName, err)
			selfmonitor.IncQueryStatusCount(qName, dbService.GetDatabaseName(), "failed")
			queryExecutedSuccessfully = false
			if dbErrorHandlingEnabled {
				handleDbError(err)
			}
			continue
		}
		selfmonitor.IncQueryStatusCount(qName, dbService.GetDatabaseName(), "completed")

		if len(qCfg.Metrics) == 0 && len(columnNames) < 2 {
			log.Errorf("[%v] For query %v metrics are not defined, but result has %v columns. Impossible to apply dynamic behavior.", qRand, qName, len(columnNames))
			return
		}

		var updatedMetricLabelValues map[string]map[string]prometheus.Labels
		for _, row := range rows {
			if len(qCfg.Metrics) != 0 {
				updatedMetricLabelValues = updateStaticMetricByRow(row, qCfg.Metrics, qRand)
			} else {
				updatedMetricLabelValues = updateDynamicMetricByRow(row, columnNames, qRand)
				for metricName := range updatedMetricLabelValues {
					registerNewDynamicMetricInRepository(qName, qRand, metricName)
				}
			}

			for metric, updatedLabelValues := range updatedMetricLabelValues {
				if totalUpdatedMetricLabelValues[metric] == nil {
					totalUpdatedMetricLabelValues[metric] = make(map[string]prometheus.Labels)
				}
				for labelValues, labels := range updatedLabelValues {
					totalUpdatedMetricLabelValues[metric][labelValues] = labels
				}
			}
		}
	}
}

func updateStaticMetricByRow(row map[string]string, metrics []string, qRand string) map[string]map[string]prometheus.Labels {
	updatedMetricsLabelsValues := make(map[string]map[string]prometheus.Labels)
MainUpdateStaticMetricByRowLoop:
	for _, metric := range metrics {
		metricsConfig := appConfig.Metrics[metric]
		if metricsConfig == nil {
			//log.Warnf("Ignoring metric %v, because it is not defined", metric)
			continue
		}
		labels := make(map[string]string)
		for label, labelValue := range omnipresentLabels {
			labels[label] = labelValue
		}
		for _, label := range metricsConfig.Labels {
			labels[label] = row[strings.ToUpper(label)]
		}
		switch metricsConfig.Type {
		case "gauge":
			if monitoringState.Get(metric) == nil {
				metricState := monstate.MetricState{}
				metricState.Initialize()
				monitoringState.Set(metric, &metricState)
				log.Infof("[%v] New metric %v registered in runtime for monitoringState map", qRand, metric)
			}

			updatedLabelValues := make(map[string]prometheus.Labels)
			existingLabelValues := monitoringState.Get(metric)
			gaugeVec := gaugeVecRepository.Get(metric)

			val, err := strconv.ParseFloat(row[strings.ToUpper(metric)], 64)
			stringLabels := utils.MapToString(labels)
			if err != nil {
				updatedLabelValues[stringLabels] = labels
				if existingLabelValues.Get(stringLabels) == nil {
					existingLabelValues.Set(stringLabels, labels)
					selfmonitor.IncMonStateEntriesCount()
				}
				updatedMetricsLabelsValues[metric] = updatedLabelValues
				defaultValueQueryReturnedNaN := metricDefaultValuesRepo.GetMetricDefaultValueQueryReturnedNaN(metric)
				gaugeVec.With(labels).Set(defaultValueQueryReturnedNaN)
				if row[strings.ToUpper(metric)] == "" {
					log.Debugf("[%v] Empty value received for metric %v and labels %v; metric set with the default value %v", qRand, metric, stringLabels, defaultValueQueryReturnedNaN)
				} else {
					log.Warnf("[%v] Error for metric %v and labels %v : Can not parse value %v : %+v ; metric set with the default value %v", qRand, metric, stringLabels, row[strings.ToUpper(metric)], err, defaultValueQueryReturnedNaN)
				}
			} else {
				log.Tracef("[%v] For metric %v and labels %v exported value %v", qRand, metric, stringLabels, val)
				updatedLabelValues[stringLabels] = labels
				if existingLabelValues.Get(stringLabels) == nil {
					existingLabelValues.Set(stringLabels, labels)
					selfmonitor.IncMonStateEntriesCount()
				}
				updatedMetricsLabelsValues[metric] = updatedLabelValues
				gaugeVec.With(labels).Set(val)
			}
		case "counter":
			counterVec := counterVecs[metric]
			val, err := strconv.ParseFloat(row[strings.ToUpper(metric)], 64)
			if err != nil {
				log.Errorf("[%v] Error parsing metric %v for value %v : %+v", qRand, metric, row[strings.ToUpper(metric)], err)
			} else if val < 0 {
				log.Errorf("[%v] Error processing metric %v for value %+v : Counter can not be decreased, value of the metric is not changed", qRand, metric, val)
			} else {
				counterVec.With(labels).Add(val)
			}
		case "histogram":
			histogramVec := histogramVecs[metric]
			sum, err := strconv.ParseFloat(row[strings.ToUpper(metricsConfig.Sum)], 64)
			if err != nil {
				log.Errorf("[%v] Error parsing sum of histogram metric %v for value %v : %+v", qRand, metric, row[strings.ToUpper(metric)], err)
				continue
			}
			cnt, err := strconv.ParseUint(row[strings.ToUpper(metricsConfig.Count)], 10, 64)
			if err != nil {
				log.Errorf("[%v] Error parsing count of histogram metric %v for value %v : %+v", qRand, metric, row[strings.ToUpper(metric)], err)
				continue
			}
			buckets := make(map[float64]uint64)
			for columnName, bucketKey := range metricsConfig.Buckets {
				bucketValue, err := strconv.ParseUint(row[strings.ToUpper(columnName)], 10, 64)
				if err != nil {
					log.Errorf("[%v] Error parsing column %v of histogram metric %v for value %v : %+v", qRand, columnName, metric, row[strings.ToUpper(metric)], err)
					continue MainUpdateStaticMetricByRowLoop
				}
				buckets[bucketKey] = bucketValue
			}
			labelKeys := append(metricsConfig.Labels, omnipresentLabelsKeys...)
			buckets[math.Inf(1.0)] = cnt
			histogramVec.Observe(sum, cnt, buckets, labels, labelKeys)
		default:
			log.Errorf("[%v] Metric %v has not supported type %v", qRand, metric, metricsConfig.Type)
		}
	}

	return updatedMetricsLabelsValues
}

func updateDynamicMetricByRow(row map[string]string, columnNames []string, qRand string) map[string]map[string]prometheus.Labels {
	updatedMetricsLabelsValues := make(map[string]map[string]prometheus.Labels)
	metric := row[strings.ToUpper(columnNames[0])]

	if monitoringState.Get(metric) == nil {
		metricState := monstate.MetricState{}
		metricState.Initialize()
		monitoringState.Set(metric, &metricState)
		log.Infof("[%v] New dynamic metric %v registered in runtime in monitoringState map", qRand, metric)
	}

	updatedLabelValues := make(map[string]prometheus.Labels)
	existingLabelValues := monitoringState.Get(metric)

	labels := make(map[string]string)
	for label, labelValue := range omnipresentLabels {
		labels[label] = labelValue
	}
	for i := 2; i < len(columnNames); i++ {
		labels[columnNames[i]] = row[strings.ToUpper(columnNames[i])]
	}

	if gaugeVecRepository.Get(metric) == nil {
		registerGaugeDynamically(metric, columnNames[2:])
	}

	gaugeVec := gaugeVecRepository.Get(metric)
	val, err := strconv.ParseFloat(row[strings.ToUpper(columnNames[1])], 64)
	stringLabels := utils.MapToString(labels)
	if err != nil {
		if existingLabelValues.Get(stringLabels) == nil {
			existingLabelValues.Set(stringLabels, labels)
			selfmonitor.IncMonStateEntriesCount()
		}
		if row[strings.ToUpper(metric)] == "" {
			log.Debugf("[%v] Empty value received for metric %v and labels %v", qRand, metric, stringLabels)
		} else {
			log.Warnf("[%v] Error for metric %v and labels %v : Can not parse value %v : %+v", qRand, metric, stringLabels, row[strings.ToUpper(metric)], err)
		}
		gaugeVec.With(labels).Set(math.NaN())
	} else {
		log.Tracef("[%v] For metric %v and labels %v exported value %v", qRand, metric, stringLabels, val)
		updatedLabelValues[stringLabels] = labels
		if existingLabelValues.Get(stringLabels) == nil {
			existingLabelValues.Set(stringLabels, labels)
			selfmonitor.IncMonStateEntriesCount()
		}
		updatedMetricsLabelsValues[metric] = updatedLabelValues
		gaugeVec.With(labels).Set(val)
	}

	return updatedMetricsLabelsValues
}

func updateDatabases() {
	rDbList := []string{}
	resultServices := []dbservice.DBService{}

	newDbSrv := dbservice.GetNewDBServices(dbServices)
	dbSrvs := append(dbServices, newDbSrv...)
	for _, dbSrv := range dbSrvs {
		if dbSrv.GetType() != dbservice.TypePostgres {
			continue
		}
		if dbSrv.Ping() {
			resultServices = append(resultServices, dbSrv)
		} else {
			log.Infof("database %s has been removed from db list", dbSrv.GetDatabaseName())
			rDbList = append(rDbList, dbSrv.GetDatabaseName())
		}
	}

	dbServices = resultServices
	unregisterUnusedMetrics(rDbList)
}

func unregisterUnusedMetrics(databases []string) {
	for _, database := range databases {
		labels := prometheus.Labels{"datname": database}
		for metric, metricConfig := range appConfig.Metrics {
			switch metricConfig.Type {
			case "gauge":
				metricState := monitoringState.Get(metric)
				for _, key := range metricState.GetAllKeys() {
					if strings.Contains(key, "datname=\""+database+"\"") {
						metricState.Delete(key)
					}
				}
				gaugeVec := gaugeVecRepository.Get(metric)
				if gaugeVec != nil {
					gaugeVec.DeletePartialMatch(labels)
				}
			case "counter":
				if counterVec, ok := counterVecs[metric]; ok {
					counterVec.DeletePartialMatch(labels)
				}
			case "histogram":
				if histMetric, ok := histogramVecs[metric]; ok {
					histMetric.DeletePartialMatch(labels)
				}
			default:
				log.Errorf("Metric %s has not supported type", metric)
			}
		}
	}
}

func registerNewDynamicMetricInRepository(qName string, qRand string, metric string) {
	if dynamicMetricsRepository.Get(qName) == nil {
		metricSet := utils.SyncStringSet{}
		metricSet.Initialize()
		dynamicMetricsRepository.Set(qName, &metricSet)
	}
	metricSet := dynamicMetricsRepository.Get(qName)
	if metricSet.Get(metric) == false {
		metricSet.Add(metric)
		log.Infof("[%v] New metric %v registered in DynamicMetricRepository for query %v", qRand, metric, qName)
	}
}

func resetNotUpdatedGauges(totalUpdatedMetricLabelValues map[string]map[string]prometheus.Labels, metrics []string, queryExecutedSuccessfully bool, qRand string) {
	for _, metric := range metrics {
		if totalUpdatedMetricLabelValues[metric] == nil {
			totalUpdatedMetricLabelValues[metric] = make(map[string]prometheus.Labels)
		}

		metricState := monitoringState.Get(metric)
		if metricState == nil {
			if gaugeVecRepository.Get(metric) != nil {
				log.Errorf("[%v] For gauge metric %v metricState is not defined, skipping resetNotUpdatedGauges for this metric", qRand, metric)
			}
			continue
		}
		metricStateKeys := metricState.GetAllKeys()

		var resetValue float64
		if queryExecutedSuccessfully {
			resetValue = metricDefaultValuesRepo.GetMetricDefaultValue(metric)
		} else {
			resetValue = metricDefaultValuesRepo.GetMetricDefaultValueQueryFailed(metric)
		}

		for _, labelValues := range metricStateKeys {
			labels := metricState.Get(labelValues)
			if totalUpdatedMetricLabelValues[metric][labelValues] == nil {
				log.Tracef("[%v] Gauge for metric %v with labelValues %v was not updated. Resetting it to %v", qRand, metric, labelValues, resetValue)
				gaugeVec := gaugeVecRepository.Get(metric)
				if gaugeVec != nil {
					gaugeVec.With(labels).Set(resetValue)
				} else {
					log.Debugf("[%v] gaugeVecRepository.Get(%v) is nil, which is unexpected", qRand, metric)
				}
			}
		}
	}
}

func interruptionHandler() {
	signal := <-osIntSignals

	go hardstopAfterTimeout(time.Minute)
	logRuntimeInfo()
	for _, dbService := range dbServices {
		dbService.Close()
	}
	stopCroniter()

	log.Infof("STOPPING QUERY EXPORTER (received %+v signal)", signal)
	fmt.Printf("\nstop query-exporter, %v\n", versionString())
	os.Exit(0)
}

func hardstopAfterTimeout(d time.Duration) {
	time.Sleep(d)
	log.Infof("STOPPING QUERY EXPORTER (hardstop after timeout)")
	os.Exit(0)
}

func quitHandler() {
	for {
		signal := <-osQuitSignals
		log.Infof("Received %+v signal, printing thread dumps...", signal)
		logRuntimeInfo()
	}
}

func stopCroniter() {
	if croniter != nil {
		croniter.Stop()
	} else {
		log.Warnf("croniter is nil before exiting, nothing to stop")
	}
}

func handleDbError(err error) {
	for ruleName, dbErrorRule := range appConfig.ErrorHandling.DbErrorRules {
		if !strings.Contains(err.Error(), dbErrorRule.Contains) {
			log.Tracef("Error [ %+v ] does not contain text %v ; skipping errorHandling rule %v", err, dbErrorRule.Contains, ruleName)
			return
		}

		var timeout time.Duration = 0
		if dbErrorRule.Timeout != "" {
			t, parseErr := time.ParseDuration(dbErrorRule.Timeout)
			if parseErr == nil {
				timeout = t
			} else {
				log.Errorf("Error parsing dbErrorHandling timeout %v for errorHandling rule %v : %v ; the rule will be executed without timeout", dbErrorRule.Timeout, ruleName, parseErr)
			}
		}
		log.Tracef("Timeout for errorHandling rule %v is set to %+v", ruleName, timeout)

		if dbErrorRule.Action == "exit" {
			go exitAfterTimeout(timeout, ruleName)
		} else {
			log.Warnf("Unknown action %v for rule %v; action will be skipped", dbErrorRule.Action, ruleName)
		}
	}
}

func exitAfterTimeout(timeout time.Duration, ruleName string) {
	stopCroniter()
	log.Warnf("Croniter is stopped after triggering rule %v. Query-exporter will be stopped after %+v", ruleName, timeout)
	time.Sleep(timeout)
	go hardstopAfterTimeout(time.Minute)
	for _, dbService := range dbServices {
		dbService.Close()
	}
	log.Infof("STOPPING QUERY EXPORTER (triggered by rule %+v)", ruleName)
	fmt.Printf("\nstop query-exporter, %v\n", versionString())
	os.Exit(0)
}

func logRuntimeInfo() {
	buf := make([]byte, 1<<20)
	stacklen := runtime.Stack(buf, true)
	log.Errorf("GOROUTINES DUMP: %s", buf[:stacklen])
	for _, dbService := range dbServices {
		log.Error(dbService.DBStat())
	}
}

func reapplyFlags() {
	if len(appConfig.Flags) == 0 {
		log.Info("No flags need to be reloaded from YAML config")
	} else {
		for name, value := range appConfig.Flags {
			err := flag.Set(name, value)
			if err != nil {
				log.Errorf("Failed to set flag %v with new value %v", name, value)
			} else {
				log.Infof("Flag %v successfully set to new value %v", name, value)
			}
		}
	}
}

func isSummarize(valueFromMetric *bool) bool {
	if valueFromMetric == nil {
		return *histSummarize
	}
	return *valueFromMetric
}

func skipQuery(dbSrv dbservice.DBService, qName string, qcfg *config.QueryConfig) bool {
	for _, qForExc := range excludedQueries {
		if qName == qForExc {
			return true
		}
	}

	if qcfg.Master && !dbSrv.IsMaster() {
		return true
	}

	skippedlisted := false
	for _, v := range dbservice.GetLongRunningQueries() {
		if qName == v {
			skippedlisted = true
		}
	}

	if skippedlisted {
		log.Infof("Query %s is skipped as long-running", qName)
		return true
	}

	if len(qcfg.LogicalDatabases) > 0 {
		for _, d := range qcfg.LogicalDatabases {
			if d == dbSrv.GetDatabaseName() {
				return false
			}
		}
		return true
	}

	if len(qcfg.Classifiers) > 0 {
		currentClassifier := dbSrv.GetClassifier()
		for _, classifier := range qcfg.Classifiers {
			if isAcceptable(classifier, currentClassifier) {
				return false
			}
		}
		return true
	}

	return false
}

func isAcceptable(mappingClassifier, dbClassifier config.Classifier) bool {
	for k, v := range mappingClassifier {
		if dbClassifier[k] != v {
			return false
		}
	}
	return true
}
