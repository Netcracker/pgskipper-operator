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

package selfmonitor

import (
	"database/sql"
	"flag"
	"strings"
	"time"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/Netcracker/qubership-query-exporter/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

var (
	disabledSelfMonitor = flag.Bool("disable-self-monitor", utils.GetEnvBool("QUERY_EXPORTER_DISABLE_SELF_MONITOR", false), "Disables self monitoring")

	dbPoolStatsGaugeVec        *prometheus.GaugeVec
	queryDurationsHistogramVec *prometheus.HistogramVec
	queryStatusCounterVec      *prometheus.CounterVec
	monStateEntriesCounterVec  *prometheus.CounterVec
	labelsMapCache             map[string]map[string]string
	dbPoolStatsLabelValues     []string
	staticLabels               map[string]string
	omnipresentLabelsKeys      []string
)

func InitSelfMonitoring(appConfig *config.Config) {
	if *disabledSelfMonitor {
		return
	}
	staticLabels = appConfig.Db.Labels
	omnipresentLabelsKeys = utils.GetKeys(staticLabels)
	if appConfig.Db.Type == "" || strings.ToLower(appConfig.Db.Type) == "oracle" {
		dbPoolStatsLabelValues = []string{"MaxOpenConnections", "OpenConnections", "InUse",
			"Idle", "WaitCount", "WaitDuration", "MaxIdleClosed", "MaxIdleTimeClosed", "MaxLifetimeClosed"}
	} else if strings.ToLower(appConfig.Db.Type) == "postgres" {
		dbPoolStatsLabelValues = []string{"AcquireCount", "AcquireDuration", "AcquiredConns", "CanceledAcquireCount",
			"ConstructingConns", "EmptyAcquireCount", "IdleConns", "MaxConns", "MaxIdleDestroyCount",
			"MaxLifetimeDestroyCount", "NewConnsCount", "TotalConns"}
	}
	initLabelsMapCache()
	dbPoolStatsGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "db_connection_pool_stats",
		Help: "DB connection pool statistic",
	}, append([]string{"name"}, omnipresentLabelsKeys...))
	prometheus.MustRegister(dbPoolStatsGaugeVec)

	var queryLatencyBuckets []float64
	if len(appConfig.SelfMonitoring.QueryLatencyBuckets) != 0 {
		queryLatencyBuckets = appConfig.SelfMonitoring.QueryLatencyBuckets
		log.Infof("Using query latency buckets from yaml for self-monitoring: %+v", queryLatencyBuckets)
	} else {
		queryLatencyBuckets = []float64{0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 30, 60}
		log.Infof("Using default query latency buckets for self-monitoring: %+v", queryLatencyBuckets)
	}
	queryDurationsHistogramVec = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "query_latency",
		Help:    "Query execution latency in seconds",
		Buckets: queryLatencyBuckets,
	}, append([]string{"query", "datname"}, omnipresentLabelsKeys...))
	prometheus.MustRegister(queryDurationsHistogramVec)

	queryStatusCounterVec = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "query_status",
		Help: "Query execution count by status",
	}, append([]string{"query", "datname", "status"}, omnipresentLabelsKeys...))
	prometheus.MustRegister(queryStatusCounterVec)

	monStateEntriesCounterVec = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mon_state_map_entries_count",
		Help: "Number of entries in the monitoring state map",
	}, omnipresentLabelsKeys)
	prometheus.MustRegister(monStateEntriesCounterVec)
}

func UpdateOracleDbPoolStats(dbStat sql.DBStats) {
	if *disabledSelfMonitor {
		return
	}
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxOpenConnections"]).Set(float64(dbStat.MaxOpenConnections))
	dbPoolStatsGaugeVec.With(labelsMapCache["OpenConnections"]).Set(float64(dbStat.OpenConnections))
	dbPoolStatsGaugeVec.With(labelsMapCache["InUse"]).Set(float64(dbStat.InUse))
	dbPoolStatsGaugeVec.With(labelsMapCache["Idle"]).Set(float64(dbStat.Idle))
	dbPoolStatsGaugeVec.With(labelsMapCache["WaitCount"]).Set(float64(dbStat.WaitCount))
	dbPoolStatsGaugeVec.With(labelsMapCache["WaitDuration"]).Set(float64(dbStat.WaitDuration / time.Second))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxIdleClosed"]).Set(float64(dbStat.MaxIdleClosed))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxIdleTimeClosed"]).Set(float64(dbStat.MaxIdleTimeClosed))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxLifetimeClosed"]).Set(float64(dbStat.MaxLifetimeClosed))
}

func UpdatePostgresDbPoolStats(dbStat pgxpool.Stat) {
	if *disabledSelfMonitor {
		return
	}
	dbPoolStatsGaugeVec.With(labelsMapCache["AcquireCount"]).Set(float64(dbStat.AcquireCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["AcquireDuration"]).Set(float64(dbStat.AcquireDuration() / time.Second))
	dbPoolStatsGaugeVec.With(labelsMapCache["AcquiredConns"]).Set(float64(dbStat.AcquiredConns()))
	dbPoolStatsGaugeVec.With(labelsMapCache["CanceledAcquireCount"]).Set(float64(dbStat.CanceledAcquireCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["ConstructingConns"]).Set(float64(dbStat.ConstructingConns()))
	dbPoolStatsGaugeVec.With(labelsMapCache["EmptyAcquireCount"]).Set(float64(dbStat.EmptyAcquireCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["IdleConns"]).Set(float64(dbStat.IdleConns()))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxConns"]).Set(float64(dbStat.MaxConns()))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxIdleDestroyCount"]).Set(float64(dbStat.MaxIdleDestroyCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["MaxLifetimeDestroyCount"]).Set(float64(dbStat.MaxLifetimeDestroyCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["NewConnsCount"]).Set(float64(dbStat.NewConnsCount()))
	dbPoolStatsGaugeVec.With(labelsMapCache["TotalConns"]).Set(float64(dbStat.TotalConns()))
}

func initLabelsMapCache() {
	labelsMapCache = make(map[string]map[string]string)
	for _, nameValue := range dbPoolStatsLabelValues {
		labels := make(map[string]string)
		for label, labelValue := range staticLabels {
			labels[label] = labelValue
		}
		labels["name"] = nameValue
		labelsMapCache[nameValue] = labels
		log.Debugf("LabelsMapCache value generated for %v, value is %+v", nameValue, labelsMapCache[nameValue])
	}
}

func QueryLatencyExecution(start time.Time, qName string, dbName string) {
	if *disabledSelfMonitor {
		return
	}

	elapsed := time.Since(start)
	seconds := float64(elapsed) / float64(time.Second)
	labels := make(map[string]string)
	for label, labelValue := range staticLabels {
		labels[label] = labelValue
	}
	labels["query"] = qName
	labels["datname"] = dbName
	queryDurationsHistogramVec.With(labels).Observe(seconds)
}

func IncQueryStatusCount(qName string, dbName string, status string) {
	if *disabledSelfMonitor {
		return
	}

	labels := make(map[string]string)
	for label, labelValue := range staticLabels {
		labels[label] = labelValue
	}
	labels["query"] = qName
	labels["status"] = status
	labels["datname"] = dbName
	queryStatusCounterVec.With(labels).Inc()
}

func IncMonStateEntriesCount() {
	if *disabledSelfMonitor {
		return
	}
	monStateEntriesCounterVec.With(staticLabels).Inc()
}
