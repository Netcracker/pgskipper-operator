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
	"context"
	"fmt"
	"time"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/postgres"
	uuid "github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Scraper) GetEndpointsStatus() {
	logger.Info("Endpoints metric collection started")
	ctx := context.Background()
	pc := postgres.NewConnector()
	err := pc.EstablishConn(ctx)
	if err != nil {
		return
	}
	defer pc.CloseConnection(ctx)
	labels := gauges.DefaultLabels()

	status, err := pc.GetValue(ctx, "select 1;")
	if err == nil {
		s.performSmoketests(ctx, pc)
	} else {

		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_passed").withLabels(labels).setValue(0))
		insertLabels := gauges.DefaultLabels()
		insertLabels["action"] = "insert"
		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_check").withLabels(insertLabels).setValue(0))
		selectLabels := gauges.DefaultLabels()
		selectLabels["action"] = "select"
		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_check").withLabels(selectLabels).setValue(0))
		updateLabels := gauges.DefaultLabels()
		updateLabels["action"] = "update"
		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_check").withLabels(updateLabels).setValue(0))
		deleteLabels := gauges.DefaultLabels()
		deleteLabels["action"] = "delete"
		s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_check").withLabels(labels).setValue(0))
	}

	delete(labels, "action")
	labels["url"] = pc.GetHost()
	s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_running").withLabels(labels).setValue(status))
	logger.Info("Endpoints metric collection finished")
}

func (s *Scraper) performSmoketests(ctx context.Context, pc *postgres.PostgresConnector) {
	type param struct {
		query string
		err   string
	}
	var params = []param{
		{query: "create table if not exists monitor_test (id bigint primary key not null, value text not null);",
			err: "cannot execute create"},
		{query: "create sequence if not exists monitor_test_seq start 10001;",
			err: "cannot create sequence for smoketest"}}
	for _, p := range params {
		_, err := pc.Exec(ctx, p.query)
		if err != nil {
			logger.Error(p.err, zap.Error(err))
			return
		}
	}
	seqVal, err := pc.GetValue(ctx, "SELECT nextval('monitor_test_seq');")
	if err != nil {
		logger.Error("cannot select nextval('monitor_test_seq')", zap.Error(err))
		return
	}
	if seqVal.(int64) == int64(2147483647) {
		_, err = pc.Exec(ctx, "delete from monitor_test;")
		if err != nil {
			logger.Error("cannot delete from monitor_test", zap.Error(err))
			return
		}
		_, err = pc.Exec(ctx, "SELECT setval(monitor_test_seq, 10001);")
		if err != nil {
			logger.Error("cannot setval for monitor_test_seq", zap.Error(err))
			return
		}
	}

	newUUID := getUUID4()
	secondUUID := getUUID4()

	resultInsert := s.performSingleSmokeTest(ctx, pc, "insert", newUUID, fmt.Sprintf("insert into monitor_test values (%v, %v) returning id;", newUUID, secondUUID))

	passed := 0
	if resultInsert {
		resultSelect := s.performSingleSmokeTest(ctx, pc, "select", newUUID, fmt.Sprintf("select id from monitor_test where id=%v;", newUUID))
		resultUpdate := s.performSingleSmokeTest(ctx, pc, "update", newUUID, fmt.Sprintf("update monitor_test set value='%v' where id=%v returning id;", secondUUID, newUUID))
		resultDelete := s.performSingleSmokeTest(ctx, pc, "delete", newUUID, fmt.Sprintf("delete from monitor_test where id=%v returning id;", newUUID))

		logger.Debug(fmt.Sprintf("resultInsert: %v resultSelect: %v resultUpdate %v resultDelete %v", resultInsert, resultSelect, resultUpdate, resultDelete))
		if resultInsert && resultSelect && resultUpdate && resultDelete {
			passed = 1
		}
	}
	s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_passed").withLabels(gauges.DefaultLabels()).setValue(passed))
}

func (s *Scraper) performSingleSmokeTest(ctx context.Context, pc *postgres.PostgresConnector, action string, expectedOutput int64, query string, args ...interface{}) bool {
	resultCode := 0
	startTime := time.Now()
	output, err := pc.GetValue(ctx, query, args...)
	if err != nil {
		logger.Error(fmt.Sprintf("Error during process action %s", action), zap.Error(err))
		resultCode = -1
	}
	if output.(int64) == expectedOutput {
		resultCode = 1
	}
	execTime := int(time.Since(startTime).Milliseconds())
	labels := gauges.DefaultLabels()
	labels["action"] = action
	s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_check").withLabels(labels).setValue(resultCode))
	s.metrics = append(s.metrics, NewMetric("ma_pg_endpoints_cluster_smoketest_time_ms").withLabels(labels).setValue(execTime))
	if resultCode != 1 && output != nil {
		s.metrics = append(s.metrics, NewMetric("ma_pg_smoketest_output").withLabels(labels).setValue(output))
	}
	return resultCode == 1
}

func getUUID4() int64 {
	uuid := uuid.New()
	return int64(uuid.ID())
}
