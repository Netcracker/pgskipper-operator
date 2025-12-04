// // Copyright 2024-2025 NetCracker Technology Corporation
// //
// // Licensed under the Apache License, Version 2.0 (the "License");
// // you may not use this file except in compliance with the License.
// // You may obtain a copy of the License at
// //
// //     http://www.apache.org/licenses/LICENSE-2.0
// //
// // Unless required by applicable law or agreed to in writing, software
// // distributed under the License is distributed on an "AS IS" BASIS,
// // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// // See the License for the specific language governing permissions and
// // limitations under the License.

package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"strings"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/k8s"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"go.uber.org/zap"
)

const (
	replicaInfoQuery = "SELECT usename, application_name, client_addr::text, pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn)::text AS lag_in_bytes," +
		"COALESCE(replay_lag, '0') as reply_lag FROM pg_stat_replication where usename = 'replicator' and application_name like 'pg-%s-node%%';"
)

type ReplicaInfo struct {
	Usename         string  `json:"usename"`
	ApplicationName string  `json:"application_name"`
	IP              string  `json:"client_addr"`
	LagInBytes      string  `json:"lag_in_bytes"`
	LagInMs         float64 `json:"replay_lag"`
}

type ClusterInfo struct {
	Members []Member `json:"members"`
}

type Member struct {
	Role  string `json:"role"`
	State string `json:"state"`
}

func (s *Scraper) CollectDRMetrics() {
	logger.Info("DR metrics collection started")
	defer s.HandleMetricCollectorStatus()
	ctx := context.Background()

	isActive, err := s.IsCurrentSiteActive(ctx)
	if err != nil {
		logger.Error("Error, while getting cluster status", zap.Error(err))
		return
	}

	if !isActive {
		logger.Info("Current site is standby, skipping dr metrics collection ...")
		return
	}

	patroniPodsIP := getPatroniPodsIP(ctx)
	standbyInfo, err := getStandbyInfo(ctx, patroniPodsIP)
	if err != nil {
		logger.Error("Error, while getting standby info", zap.Error(err))
		return
	}

	s.metrics = append(s.metrics, NewMetric("ma_pg_standby_leader_count").withLabels(gauges.DefaultLabels()).setValue(len(standbyInfo)))

	for _, replica := range standbyInfo {
		labels := gauges.DefaultLabels()
		labels["replica_ip"] = replica.IP
		s.metrics = append(s.metrics, NewMetric("ma_pg_standby_replication_lag_in_bytes").withLabels(labels).setValue(replica.LagInBytes))
		s.metrics = append(s.metrics, NewMetric("ma_pg_standby_replication_lag_in_ms").withLabels(labels).setValue(replica.LagInMs))
	}

	logger.Info("DR metrics collection finished")
}

func getPatroniPodsIP(ctx context.Context) []string {
	nodes := k8s.GetPatroniNodes(ctx, clusterName)
	ips := make([]string, 0)
	for _, node := range nodes {
		ips = append(ips, node.IP)
	}
	return ips
}

func getStandbyInfo(ctx context.Context, patroniPodsIP []string) ([]ReplicaInfo, error) {
	standbyInfo := make([]ReplicaInfo, 0)

	err := pc.EstablishConn(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := pc.Query(ctx, fmt.Sprintf(replicaInfoQuery, clusterName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var usename string
		var applicationName string
		var ip string
		var lagBytesPt *string
		var lag time.Duration
		err = rows.Scan(&usename, &applicationName, &ip, &lagBytesPt, &lag)
		if err != nil {
			logger.Error("Error, while getting replica info", zap.Error(err))
			return nil, err
		}

		isReplica := false
		for _, patroniIP := range patroniPodsIP {
			if strings.Contains(ip, patroniIP) {
				isReplica = true
				break
			}
		}

		if !isReplica {
			lagInBytes := ""
			if lagBytesPt != nil {
				lagInBytes = *lagBytesPt
			}
			standbyInfo = append(standbyInfo, ReplicaInfo{
				Usename:         usename,
				ApplicationName: applicationName,
				IP:              ip,
				LagInBytes:      lagInBytes,
				LagInMs:         float64(lag.Milliseconds()),
			})
		}
	}
	return standbyInfo, nil
}

func (s *Scraper) IsCurrentSiteActive(ctx context.Context) (bool, error) {
	var response = ClusterInfo{}
	protocol, _ := util.GetProtocol()
	url := fmt.Sprintf("%spg-%s-api:8008/cluster", protocol, clusterName)

	status, body, err := util.ProcessHttpRequest(s.httpClient, url, s.token)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot collect backup status metric. url %v", url))
		return false, err
	}
	code := strings.Fields(status)[0]
	statusCode, err := strconv.Atoi(code)
	if statusCode != 200 || err != nil {
		logger.Warn(fmt.Sprintf("Cannot collect cluster status for dr metrics. Error code %v", statusCode))
		logger.Warn(fmt.Sprintf("Error: %v", err))
		return false, err
	}
	err = json.Unmarshal(body, &response)
	if err != nil {
		Log.Error(fmt.Sprintf("Process cluster info Unmarshal Error: %s", err))
		logger.Error(fmt.Sprintf("Error: %v", err))
		return false, err
	}
	for _, member := range response.Members {
		if member.Role == "leader" && member.State == "running" {
			return true, nil
		}
	}
	return false, nil
}
