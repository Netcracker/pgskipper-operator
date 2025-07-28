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

package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"
	k8sClient "github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/k8s"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/postgres"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/strings/slices"
)

var (
	pc                      = postgres.NewConnector()
	ctx                     = context.Background()
	DefaultClusterPgNodeQty = util.GetEnv("POSTGRES_NODES_QTY", "2")
	Log                     = util.GetLogger()
	shellMetrics            = map[string]string{
		"du": "du /var/lib/pgsql/data/ --max-depth=2 --exclude=/var/lib/pgsql/data/lost+found",
		"df": "df /var/lib/pgsql/data"}
	backupStatus = map[string](int){"SUCCESSFUL": 1, "IN PROGRESS": 2, "FAILED": 3, "CANCELED": 4}
)

type Scraper struct {
	client         *kubernetes.Clientset
	protocol       string
	httpClient     *http.Client
	port           string
	token          string
	metrics        []*Metric
	pgMajorVersion int
	pgFullVersion  float64
}

type BackupResponse map[string]([]BackupInfo)

type BackupInfo struct {
	BackupID       string `json:"backupId"`
	Namespace      string `json:"namespace"`
	Status         string `json:"status"`
	ExpirationDate string `json:"expirationDate"`
	Created        string `json:"created"`
}

func GetScraper(client *kubernetes.Clientset, httpClient *http.Client, protocol, port string) *Scraper {

	scraper := &Scraper{
		client:         client,
		protocol:       protocol,
		httpClient:     httpClient,
		port:           port,
		token:          util.GetToken(),
		metrics:        make([]*Metric, 0),
		pgMajorVersion: getVersionOfPGSQLServer(),
		pgFullVersion:  getFullVersionOfPGSQLServer(),
	}

	return scraper
}
func (s *Scraper) CollectMetrics() {
	logger.Info("Start to collect metrics")
	s.metrics = append(s.metrics, NewMetric("ma_status").withLabels(gauges.DefaultLabels()).setValue(0))
	defer s.HandleMetricCollectorStatus()
	startTime := time.Now()
	s.metrics = append(s.metrics, NewMetric("ma_collector_start").withLabels(gauges.DefaultLabels()).setValue(startTime.Nanosecond()))
	s.metrics = append(s.metrics, NewMetric("ma_collector_status").withLabels(gauges.DefaultLabels()).setValue(0))

	s.collectPodMetrics()
	labels := gauges.DefaultLabels()
	labels["host"] = util.GetEnv("HOSTNAME", "")
	s.metrics = append(s.metrics, NewMetric("ma_collector_duration").withLabels(labels).setValue(time.Now().Nanosecond()-startTime.Nanosecond()))
	logger.Info("Start to collect metrics finished")

}

func (s *Scraper) HandleMetricCollectorStatus() {
	logger.Info("Start Handle Metric Collector Status")

	if r := recover(); r != nil {
		logger.Error(fmt.Sprintf("Metric Collector stopped with error. Set Status 'PROBLEM'. Recovered in: %v", r))
		status := 6
		s.metrics = append(s.metrics, NewMetric("ma_status").withLabels(gauges.DefaultLabels()).setValue(status))
	}
}

func (s *Scraper) PrintMetrics() []string {
	logger.Info("Print gathered metrics to response")
	metrics := make([]string, 0)
	for _, metric := range s.metrics {
		result := fmt.Sprintf("%s{%s} %v", metric.name, mapToString(metric.labels), metric.value)
		logger.Debug(fmt.Sprintf("%v", result))
		metrics = append(metrics, result)
	}
	s.metrics = make([]*Metric, 0)
	return metrics
}

func mapToString(m labels) string {
	b := new(bytes.Buffer)
	for _, key := range m.sort() {
		fmt.Fprintf(b, "%s=\"%s\",", key, m[key])
	}
	str := b.String()
	return str[:len(str)-1]
}

func (s *Scraper) CollectBackupMetrics() {
	logger.Info("Collect backup metrics")
	var response = BackupResponse{}
	protocol, _ := util.GetProtocol()
	url := fmt.Sprintf("%s%s", protocol, "postgres-backup-daemon:9000/backup/info")

	status, body, err := util.ProcessHttpRequest(s.httpClient, url, s.token)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot collect backup status metric. url %v", url))
		return
	}
	code := strings.Fields(status)[0]
	statusCode, err := strconv.Atoi(code)
	if statusCode >= 400 || err != nil {
		logger.Warn(fmt.Sprintf("Cannot collect backup status metric. Error code %v", statusCode))
		logger.Warn(fmt.Sprintf("Error: %v", err))
		return
	}
	err = json.Unmarshal(body, &response)
	if err != nil {
		Log.Error(fmt.Sprintf("Process Config Map Unmarshal Error: %s", err))
		logger.Error(fmt.Sprintf("Error: %v", err))
		return
	}
	infos := response["granular"]
	for _, info := range infos {
		labels := gauges.DefaultLabels()
		labels["backupId"] = info.BackupID
		labels["space"] = info.Namespace
		labels["expirationDate"] = info.ExpirationDate
		labels["created"] = info.Created
		status, ok := backupStatus[strings.ToUpper(info.Status)]
		if !ok {
			Log.Warn(fmt.Sprintf("Backup status %s in undefined for %s", info.Status, info.BackupID))
			status = -1 //UNDEFINED
		}
		s.metrics = append(s.metrics, NewMetric("ma_granular_backups_info").withLabels(labels).setValue(status))
	}
}

func (s *Scraper) collectPodMetrics() {

	podData, _ := k8sClient.GetPodsByLabel(context.Background(), map[string]string{"app": clusterName, "pgcluster": clusterName})
	Log.Debug(fmt.Sprintf("pod info: %v", podData))

	podItems := podData.Items
	podIdentities := make([]string, 0)

	for _, pod := range podItems {
		phase := pod.Status.Phase
		deploymentInfo := pod.Status.ContainerStatuses
		if len(deploymentInfo) == 0 {
			Log.Info(fmt.Sprintf("\"Skipping pod with info: %v", pod))
			continue
		}
		Log.Debug(fmt.Sprintf("Deployment information returned %v", deploymentInfo))
		deploymentName := deploymentInfo[0].Name
		Log.Debug(fmt.Sprintf("Deployment Name returned from array %s", deploymentName))
		unavailableReplicas := s.getUnavailableReplicasFromDeployment(deploymentName)
		if phase == "Running" && unavailableReplicas != 1 { //TODO why unavailableReplicas != 1 ? may be < 1 ?
			podIdentity := s.collectGenericMetricsForPod(pod)
			podIdentities = append(podIdentities, podIdentity)
		} else {
			podName := pod.Name
			reason := pod.Status.Reason
			Log.Info(fmt.Sprintf("skipping pod: %s phase: %s reason: %s", podName, phase, reason))
		}
	}
	s.collectClusterStatusMetrics(podIdentities, podItems)
}

func (s *Scraper) gatherClasterInfo(podItems []v1.Pod) (masters []v1.Pod, replicaNames []string, masterName string, workingNodes int) {
	logger.Debug("Process data from pods - get working nodes count")
	workingNodes = 0
	masters = make([]v1.Pod, 0)
	replicaNames = make([]string, 0)
	masterName = ""
	for _, pod := range podItems {
		if util.GetPodStatus(pod) == "running" {
			workingNodes++
		}
		role := util.SafeGet(pod.Annotations["status"], append(make([]interface{}, 0), "role"), "")
		if role == "master" {
			masters = append(masters, pod)
			masterName = pod.Name
		} else if role == "replica" {
			replicaNames = append(replicaNames, pod.Name)
		}
	}
	return masters, replicaNames, masterName, workingNodes
}

func (s *Scraper) collectClusterStatusMetrics(podIdentities []string, podItems []v1.Pod) {
	masters, replicaNames, masterName, workingNodes := s.gatherClasterInfo(podItems)

	logger.Debug("Process data from pods - collect info about master")
	logger.Debug(fmt.Sprintf("Process master pod: %s", masterName))
	leaderRole := util.GetLeaderPod(s.httpClient, s.token, clusterName)
	isClusterActive := 1
	if util.SafeGet(leaderRole, append(make([]interface{}, 0), "name"), "").(string) == masterName &&
		util.SafeGet(leaderRole, append(make([]interface{}, 0), "role"), "").(string) == "standby_leader" {
		isClusterActive = 0
	}

	errorStatus := "None"
	errorMessage := "None"
	errorDescription := "None"
	var clusterState string
	clusterStatusCode := 0

	if len(masters) > 0 {
		logger.Debug("Master found. Starting smoke tests")
		master := masters[0]
		s.enrichMasterMetrics(master, replicaNames, isClusterActive)

		podIdentity := util.GetPodIdentity(master)

		for _, metric := range s.metrics {
			if metric.name == "ma_pg_metrics_xlog_location" && metric.labels["pg_node"] == podIdentity {

				util.StoreLastMasterAppearance(metric.getValue())
			}
		}
		logger.Debug(fmt.Sprintf("Master Status: %v", master.Status))

	} else {
		errorStatus, errorMessage, errorDescription = s.calculatePGClusterStatusForMissingMaster(podItems)
	}
	actualNodes := 0
	for _, podIdentity := range podIdentities {
		logger.Debug(fmt.Sprintf("Check Actual podIdentities : %v", podIdentities))
		if s.isXlogLocationActual(podIdentity) {
			actualNodes++
		}
	}

	masterWritable := s.getMetricValue(fmt.Sprintf("ma_pg_%s_cluster_master_writable", clusterName))

	if masterWritable == nil {
		masterWritable = float64(0)
	}

	// check standby cluster settings here and proceed with OK to prevent metrics rewriting.
	// It's expected that standby_leader is not writable.
	pgNodesQty, _ := strconv.Atoi(DefaultClusterPgNodeQty)
	logger.Debug(fmt.Sprintf("Cluster Status: masterWritable: %v, isClusterActive: %v, workingNodes: %v, DefaultClusterPgNodeQty: %v, actualNodes: %v", masterWritable, isClusterActive, workingNodes, DefaultClusterPgNodeQty, actualNodes))
	if (masterWritable == 1 || isClusterActive == 1) && workingNodes >= pgNodesQty && actualNodes >= pgNodesQty {
		clusterState = "OK"
		clusterStatusCode = 0
	} else if masterWritable == 1 || isClusterActive == 0 {
		clusterState = "DEGRADED"
		clusterStatusCode = 6
		if workingNodes != pgNodesQty {
			errorStatus = "WARNING"
			errorMessage = "One or more replicas does not have running postgresql."
			errorDescription = "Supposed action: Check logs and follow troubleshooting guide."
		} else {
			errorStatus = "WARNING"
			errorMessage = "One or more replicas cannot start replication."
			errorDescription = "Supposed action: Check logs and follow troubleshooting guide."
		}
	} else {
		clusterState = "ERROR"
		clusterStatusCode = 10
		if len(masters) > 0 {
			errorStatus, errorMessage, errorDescription = s.calculatePGClusterStatusForMissingMaster(podItems)
		}
	}

	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_endpoints_cluster_nodes", clusterName)).withLabels(gauges.DefaultLabels()).setValue(len(podItems)))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_working_nodes", clusterName)).withLabels(gauges.DefaultLabels()).setValue(workingNodes))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_actual_nodes", clusterName)).withLabels(gauges.DefaultLabels()).setValue(workingNodes))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_master_writable", clusterName)).withLabels(gauges.DefaultLabels()).setValue(masterWritable))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_status", clusterName)).withLabels(gauges.DefaultLabels()).setValue(clusterStatusCode))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_active", clusterName)).withLabels(gauges.DefaultLabels()).setValue(isClusterActive))
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_nodes", clusterName)).withLabels(gauges.DefaultLabels()).setValue(len(podItems)))

	// log cluster status
	logger.Debug(fmt.Sprintf("Cluster Info \n Status: %s\n Message: %s\n Description: %s\n State: %s", errorStatus, errorMessage, errorDescription, clusterState))
}

func (s *Scraper) isXlogLocationActual(podIdentity string) bool {
	lastMasterXlogLocation := util.GetPodValue("master", "last_master_xlog_location", "0")
	isRunning := float64(0)
	currentXlogLocation := float64(0)

	for _, metric := range s.metrics {
		if metric.name == "ma_pg_pg_metrics_running" {
			isRunning = metric.getValue()
		}
		if metric.name == "ma_pg_metrics_xlog_location" && metric.labels["pg_node"] == podIdentity {
			currentXlogLocation = metric.getValue()
		}
	}
	if isRunning == 1 {
		lastXlogLocationStr := fmt.Sprintf("%f", currentXlogLocation)
		lastXlogLocation := util.GetPodValue(podIdentity, "xlog_location", lastXlogLocationStr)
		currentXlogLocationStr := fmt.Sprintf("%f", currentXlogLocation)
		util.StorePodValue(podIdentity, "xlog_location", currentXlogLocationStr)
		logger.Debug(fmt.Sprintf("For %s: current_xlog_location=%s, last_xlog_locatioqn=%s, last_master_xlog_location=%s",
			podIdentity, currentXlogLocationStr, lastXlogLocation, lastMasterXlogLocation))
		lastMasterXlogLocationFloat, _ := strconv.ParseFloat(lastMasterXlogLocation, 64)
		lastXlogLocationFloat, _ := strconv.ParseFloat(lastXlogLocation, 64)
		delta1 := currentXlogLocation - lastXlogLocationFloat
		delta2 := currentXlogLocation - lastMasterXlogLocationFloat
		if delta2 < 0 && delta1 == 0 {
			logger.Debug(fmt.Sprintf("Node %s has zero progress for xlog_location while master is ahead of node. "+
				"Calculated metrics: delta1: %f, delta2: %F", podIdentity, delta1, delta2))
			return false
		}
	}
	return true
}

func (s *Scraper) calculatePGClusterStatusForMissingMaster(podItems []v1.Pod) (string, string, string) {
	logger.Warn("Master not found or in read only mode. Calculating cluster status.")

	errorStatus := "None"
	errorMessage := "None"
	errorDescription := "None"

	if len(podItems) < 1 {
		logger.Warn("Can not calculate metrics for missing master.")
		return errorStatus, errorMessage, errorDescription
	}
	pod := podItems[0]

	RTO, _ := strconv.ParseFloat(util.GetEnvValueFromPod(pod, "PATRONI_TTL", "60"), 64)
	RPO, _ := strconv.ParseFloat(util.GetEnvValueFromPod(pod, "PATRONI_MAXIMUM_LAG_ON_FAILOVER", "1048576"), 64)
	podIdentity := util.GetPodIdentity(pod)
	logger.Info(fmt.Sprintf("RTO: %f, RPO: %f", RTO, RPO))
	lma, lmxl := util.GetLatestMasterAppearance()
	maxLagLocation := float64(0)
	if len(podItems) > 0 {
		for _, metric := range s.metrics {
			if metric.name == "ma_pg_metrics_xlog_location" && metric.labels["pg_node"] == podIdentity {
				maxLagLocation = math.Max(maxLagLocation, metric.getValue())
			}
		}
	}
	if lma != "" {
		timeSinceMasterDisappear := float64(time.Now().Second()) - maxLagLocation
		lmxlFloat, _ := strconv.ParseFloat(lmxl, 64)
		minimumLag := lmxlFloat - maxLagLocation
		if timeSinceMasterDisappear < RTO && minimumLag > RPO {
			errorStatus = "WARNING"
			errorMessage = "No replica to promote but service is down less than RTO"
			errorDescription = fmt.Sprintf("Supposed action: check master state and restore if possible. Stats:"+
				" [ last_master_appearance: %s, ast_master_xlog_location: %s, max_xlog_location: %f ]", lma, lmxl, maxLagLocation)
		}
		if timeSinceMasterDisappear > RTO && minimumLag > RPO {
			errorStatus = "CRITICAL"
			errorMessage = "No replica to promote and service is down more than RTO"
			errorDescription = fmt.Sprintf("Supposed action: try to restore master manually if possible or perform failover. Stats:"+
				" [ last_master_appearance: %s, ast_master_xlog_location: %s, max_xlog_location: %f ]", lma, lmxl, maxLagLocation)
		}

	} else {
		errorStatus = "CRITICAL"
		errorMessage = "monitoring does not have record if master ever existed."
		errorDescription = fmt.Sprintf("Supposed action: Check cluster state. Ignore this message if cluster is staring up. Stats:"+
			" [ last_master_appearance: %s, ast_master_xlog_location: %s, max_xlog_location: %f ]", lma, lmxl, maxLagLocation)
	}
	return errorStatus, errorMessage, errorDescription
}

func (s *Scraper) enrichMasterMetrics(master v1.Pod, replicaNames []string, active int) {

	logger.Debug(fmt.Sprintf("Enrich metrics for master pod: %s", master.Name))

	isMasterRunning := util.SafeGet(master.Annotations["status"], append(make([]interface{}, 0), "state"), "").(string)
	if strings.EqualFold(isMasterRunning, "running") {
		if active == 1 {
			logger.Debug("Starting smoke tests")
			s.performSmokeTest()
			// print metrics from smoke tests here
		} else {
			logger.Debug("Cluster is in standby mode. Skip smoke tests")
		}
		s.collectReplicationData(replicaNames)
		s.collectArchiveData()
	} else {
		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_passed").withLabels(gauges.DefaultLabels()).setValue(0))
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_master_writable", clusterName)).withLabels(gauges.DefaultLabels()).setValue(0))
	}
	for _, metric := range s.metrics {

		if metric.name == "ma_pg_endpoints_cluster_smoketest_passed" {
			passed := metric.getValue()
			s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_cluster_master_writable", clusterName)).withLabels(gauges.DefaultLabels()).setValue(passed))
		}
	}
	s.collectReplicationLag()

}
func (s *Scraper) performSmokeTest() {
	// using already rewrote method for smoke tests
	s.GetEndpointsStatus()
}

func (s *Scraper) collectReplicationLag() {

	url := fmt.Sprintf("http://pg-%s-api:8008/cluster", clusterName)

	status, response, err := util.ProcessHttpRequest(s.httpClient, url, s.token)
	if err != nil {
		logger.Error(err.Error())
	}
	statusCode, _ := strconv.Atoi(status)
	if statusCode >= 400 {
		logger.Error(fmt.Sprintf("Cannot collect replication lag data. Error code: %v", statusCode))
		return
	}
	var res map[string]interface{}
	err = json.Unmarshal(response, &res)
	if err != nil {
		logger.Error("Can not collect replication lag metric. Response parsing error")
	} else {
		members := util.SafeGet(res, append(make([]interface{}, 0), "members"), make(map[string]interface{}, 0)).([]interface{})
		for _, member := range members {
			role := util.SafeGet(member, append(make([]interface{}, 0), "role"), "").(string)
			if role == "leader" || role == "standby_leader" {
				continue
			}
			state := util.SafeGet(member, append(make([]interface{}, 0), "state"), "").(string)
			if state == "running" || state == "streaming" {
				lag := util.SafeGet(member, append(make([]interface{}, 0), "lag"), "")
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_replication_lag", clusterName)).withLabels(gauges.DefaultLabels()).setValue(lag))
			}
		}
	}
}

func (s *Scraper) collectReplicationData(replicaNames []string) {

	logger.Debug("Collect data about replication")

	if s.pgMajorVersion > 0 {
		err := pc.EstablishConn(ctx)
		if err != nil {
			Log.Error("Can not establish connection to postgresql to collect replication data")
		} else {
			defer pc.CloseConnection(ctx)
			_, rows := pc.GetData(ctx, GetMetricsTypeByVersion("replication_data", s.pgMajorVersion))
			replicasMatch := len(replicaNames) == len(rows)
			replicationStates := make([]string, 0)
			for _, row := range rows {

				appName := strings.Replace(postgres.GetStringValue(row, "application_name", "empty"), " ", "_", -1)
				sentReplayLag, _ := util.GetFloatValue(row["sent_replay_lag"], float64(0))
				sentLag, _ := util.GetFloatValue(row["sent_lag"], float64(0))

				sentLagMetricName := fmt.Sprintf("ma_pg_%s_replication_state_sent_lag", clusterName)
				sentReplayLagmetricName := fmt.Sprintf("ma_pg_%s_replication_state_sent_replay_lag", clusterName)

				labels := gauges.DefaultLabels()
				labels["hostname"] = appName

				s.metrics = append(s.metrics, NewMetric(sentLagMetricName).withLabels(labels).setValue(sentLag))
				s.metrics = append(s.metrics, NewMetric(sentReplayLagmetricName).withLabels(labels).setValue(sentReplayLag))

				replicationStates = append(replicationStates, appName)
			}
			if util.GetEnv("SITE_MANAGER", "") == "on" {
				for _, name := range replicaNames {
					if slices.Contains(replicationStates, name) && replicasMatch {
						smReplicationState := 0
						metricName := fmt.Sprintf("ma_pg_%s_replication_state_sm_replication_state", clusterName)
						s.metrics = append(s.metrics, NewMetric(metricName).withLabels(gauges.DefaultLabels()).setValue(smReplicationState))
					}
				}
			}
		}
	}
}

func (s *Scraper) collectArchiveData() {

	logger.Debug("Collect data of wal archive")
	baseName := fmt.Sprintf("ma_pg_%s_pg_metrics_archive", clusterName)
	err := pc.EstablishConn(ctx)
	if err != nil {
		Log.Error("Can not establish connection to postgresql to collect archive data")
	} else {
		defer pc.CloseConnection(ctx)
		_, rows := pc.GetData(ctx, ArchiveDataQuery)
		for _, row := range rows {
			mode := postgres.GetStringValue(row, "setting", "off")
			if mode == "on" {
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "mode")).withLabels(gauges.DefaultLabels()).setValue(1))
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "mode_prom")).withLabels(gauges.DefaultLabels()).setValue(1))
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "archived_count")).withLabels(gauges.DefaultLabels()).setValue(row["archived_count"]))
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "failed_count")).withLabels(gauges.DefaultLabels()).setValue(row["failed_count"]))
				delay := postgres.GetFloatValue(row, "extract", 0.0)
				if delay != 0.0 {
					s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "delay")).withLabels(gauges.DefaultLabels()).setValue(delay))
				}
			} else {
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "mode")).withLabels(gauges.DefaultLabels()).setValue(0))
				s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("%s_%s", baseName, "mode_prom")).withLabels(gauges.DefaultLabels()).setValue(0))
			}
		}
	}
}

func (s *Scraper) getUnavailableReplicasFromDeployment(deploymentName string) int32 {
	dcInfo := k8sClient.GetDeploymentInfo(deploymentName)
	if dcInfo != nil {
		status := &dcInfo.Status
		unavailableReplicas := &status.UnavailableReplicas
		return *unavailableReplicas
	}
	return 0
}

func (s *Scraper) collectGenericMetricsForPod(pod v1.Pod) string {

	podId := pod.Name
	podIp := pod.Status.PodIP
	Log.Debug(fmt.Sprintf("Start metric collection for %s [%s]", podId, podIp))

	podIdentity := util.GetPodIdentity(pod)

	Log.Debug(fmt.Sprintf("pod_identity: %s", podIdentity))

	podLabels := map[string]string{
		"namespace": namespace,
		"name":      podId,
		"ip":        podIp,
		"role":      util.DetermineRole(pod),
		"status":    string(pod.Status.Phase),
		"startedAt": pod.Status.StartTime.String(),
		"pg_node":   podIdentity,
	}

	// #scrapemetric
	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_pod", clusterName)).withLabels(podLabels).setValue(0))

	state := util.GetPodStatus(pod)
	Log.Debug(fmt.Sprintf("state: %s", state))

	if state == "running" {
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_patroni_status", clusterName)).withLabels(podLabels).setValue(1))
	} else {
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_patroni_status", clusterName)).withLabels(podLabels).setValue(0))
	}

	if s.pgMajorVersion > 0 {

		pgConnector := postgres.NewConnectorForUser(podIp, postgres.PgPort, postgres.PgUser, postgres.PgPass)

		s.collectPGMetrics(podIdentity, pgConnector)
		s.collectShellMetrics(pod, podIdentity, s.pgMajorVersion)
	}

	return podIdentity
}

func getFullVersionOfPGSQLServer() float64 {
	err := pc.EstablishConn(ctx)
	if err != nil {
		Log.Debug("Can not establish connection to postgresql to check version")
		return 0
	}
	defer pc.CloseConnection(ctx)
	if version, err := pc.GetValue(ctx, "SHOW SERVER_VERSION"); err == nil {
		if result, err := strconv.ParseFloat(strings.Split(version.(string), " ")[0], 64); err == nil {
			return result
		} else {
			Log.Debug("Can not read postgresql version")
			return 0
		}
	} else {
		Log.Debug("Can not read postgresql version")
		return 0
	}
}

func getVersionOfPGSQLServer() int {
	result := getFullVersionOfPGSQLServer()
	return int(result)
}

func (s *Scraper) collectPGMetrics(podIdentity string, pc *postgres.PostgresConnector) {

	startTime := time.Now()

	// common pg metrics will be collected in  collector/pkg/metrics/common_metrics.go:collectMetrics
	// disable to avoid metrics duplication
	//s.collectCommonPGMetrics(podIdentity, pc)
	s.collectReplicationSlotsData(podIdentity, pc)

	Log.Debug(fmt.Sprintf("Pg metrics loading time %v", time.Since(startTime)))

}

func (s *Scraper) collectReplicationSlotsData(podIdentity string, pgConnector *postgres.PostgresConnector) {

	Log.Debug(fmt.Sprintf("Collecting replication slots data for pgsql server %v", s.pgMajorVersion))

	err := pgConnector.EstablishConn(ctx)
	if err != nil {
		Log.Error("Can not establish connection to postgresql to collect replication slots data")
	} else {
		defer pgConnector.CloseConnection(ctx)
		labels := gauges.DefaultLabels()
		labels["pg_node"] = podIdentity
		_, rows := pgConnector.GetData(ctx, GetMetricsTypeByVersion("replication_slots", s.pgMajorVersion))
		for _, row := range rows {
			serviceName := strings.Replace(row["slot_name"].(string), " ", "_", -1)
			if val, ok := row["restart_lsn_lag"]; ok {
				if restartLsnLagValue, err := util.GetFloatValue(val, float64(0)); err != nil {
					Log.Warn(fmt.Sprintf("Can not parse replication_slots metric with value %v", row["restart_lsn_lag"]))
				} else {
					metricName := fmt.Sprintf("ma_pg_%s_pg_metrics_replication_slots_%s_restart_lsn_lag", clusterName, serviceName)
					s.metrics = append(s.metrics, NewMetric(metricName).withLabels(labels).setValue(restartLsnLagValue))
				}
			}

			if confirmedFlushLsnLagValue, err := util.GetFloatValue(row["confirmed_flush_lsn_lag"], float64(0)); err != nil {
				Log.Warn(fmt.Sprintf("Can not parse replication_slots metric with value %v", row["confirmed_flush_lsn_lag"]))
			} else {
				metricName := fmt.Sprintf("ma_pg_%s_pg_metrics_replication_slots_%s_confirmed_flush_lsn_lag", clusterName, serviceName)
				s.metrics = append(s.metrics, NewMetric(metricName).withLabels(labels).setValue(confirmedFlushLsnLagValue))
			}
		}
	}
	Log.Debug(fmt.Sprintf("Collecting replication slots data for pgsql server %v Completed", s.pgMajorVersion))

}

// TODO: there are can be several metrics with the same name
func (s *Scraper) getMetricValue(metricName string) any {
	for _, metric := range s.metrics {
		if metric.name == fmt.Sprint(metricName) {
			return metric.getValue()
		}
	}
	return nil
}

func (s *Scraper) collectShellMetrics(pod v1.Pod, podIdentity string, pgVersion int) {

	startTime := time.Now()
	s.collectDiskMetricsOnPod(pod, podIdentity, pgVersion)
	logger.Debug(fmt.Sprintf("Shell metrics loading time: %v", int(time.Since(startTime).Milliseconds())))
}

func GetContainerNameForPatroniPod(pod v1.Pod) string {
	for _, c := range pod.Spec.Containers {
		if strings.HasPrefix(c.Name, "pg-patroni") {
			return c.Name
		}
	}
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return ""
}

func (s *Scraper) collectDiskMetricsOnPod(pod v1.Pod, podIdentity string, pgVersion int) {
	logger.Debug("Collect disk metrics on pod")
	containerName := GetContainerNameForPatroniPod(pod)
	for metric, command := range shellMetrics {
		res, _, err := util.ExecCmdOnPod(s.client, pod.Name, pod.Namespace, command, containerName)
		if err != nil {
			return
		}
		if metric == "du" {
			s.duProcessor(res, podIdentity, pgVersion)
		}
		if metric == "df" {
			s.dfProcessor(res, podIdentity)
		}
	}
}

func (s *Scraper) duProcessor(result string, podIdentity string, pgVersion int) {

	logger.Debug(fmt.Sprintf("Run du Processor for node: %s", podIdentity))
	var values = map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(result, "\n"), "\n") {

		str := strings.Fields(line)
		values[str[1]] = str[0]

	}
	labels := gauges.DefaultLabels()
	labels["pg_node"] = podIdentity

	total, ok := values["/var/lib/pgsql/data/"]
	if ok {
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_du_%s", clusterName, "total")).withLabels(labels).setValue(total))
	}
	pg_xlog, ok := values[fmt.Sprintf(GetMetricsTypeByVersion("pg_xlog", pgVersion), podIdentity)]
	if ok {
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_du_%s", clusterName, "pg_xlog")).withLabels(labels).setValue(pg_xlog))
	}
	base, ok := values[fmt.Sprintf("/var/lib/pgsql/data/postgresql_%s/base", podIdentity)]
	if ok {
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_du_%s", clusterName, "base")).withLabels(labels).setValue(base))
	}

	totalFloat, err := util.GetFloatValue(total, float64(0))
	if err != nil {
		Log.Warn(fmt.Sprintf("Can't parse disk etric value. Error: %v", err))
		return
	}
	pg_xlogFloat, err := util.GetFloatValue(pg_xlog, float64(0))
	if err != nil {
		Log.Warn(fmt.Sprintf("Can't parse disk etric value. Error: %v", err))
		return
	}
	baseFloat, err := util.GetFloatValue(base, float64(0))
	if err != nil {
		Log.Warn(fmt.Sprintf("Can't parse disk etric value. Error: %v", err))
		return
	}

	other := totalFloat - pg_xlogFloat - baseFloat

	s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_du_other", clusterName)).withLabels(labels).setValue(other))
}

func (s *Scraper) dfProcessor(result string, podIdentity string) {
	logger.Debug(fmt.Sprintf("Run df Processor for node: %s result \n%s", podIdentity, result))

	for i, line := range strings.Split(strings.TrimSuffix(result, "\n"), "\n") {
		// skip 1st info line from command output
		if i == 0 {
			continue
		}
		values := strings.Fields(line)
		avail := values[3]
		pcent := values[4]
		pcent = pcent[:len(pcent)-1]

		labels := gauges.DefaultLabels()
		labels["pg_node"] = podIdentity
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_df_avail", clusterName)).withLabels(labels).setValue(avail))
		s.metrics = append(s.metrics, NewMetric(fmt.Sprintf("ma_pg_%s_metrics_df_pcent", clusterName)).withLabels(labels).setValue(pcent))
	}
}
