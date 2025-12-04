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

package scheduler

import (
	"flag"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/Netcracker/qubership-query-exporter/config"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

var autodiscoveryInterval = flag.String("autodiscovery-interval", utils.GetEnv("AUTODISCOVERY_INTERVAL", "1m"), "Specifies autodiscovery interval")
var defaultInterval = flag.String("collection-interval", utils.GetEnv("COLLECTION_INTERVAL", ""), "Specifies autodiscovery interval")

func Schedule(croniter *cron.Cron, appConfig *config.Config, executeQueryAndUpdateMetrics func(string), updateDatabases func()) []string {
	if utils.IsAutodiscoveryEnabled() {
		croniter.AddFunc("@every "+*autodiscoveryInterval, func() { updateDatabases() })
	}
	nonScheduledQueries := addQueriesExecutionsToCron(croniter, appConfig, executeQueryAndUpdateMetrics)
	croniter.Start()
	log.Infof("Croniter started")
	return nonScheduledQueries
}

func addQueriesExecutionsToCron(croniter *cron.Cron, appConfig *config.Config, executeQueryAndUpdateMetrics func(string)) []string {
	nonScheduledQueries := make([]string, 0)
	parser := utils.GetCronParser()
	for qName, qCfg := range appConfig.Queries {
		qName := qName
		qCfg := qCfg //  Get a fresh version of the variable with the same name, deliberately shadowing the loop variable locally but unique to each goroutine.
		if qCfg.Croniter != "" {
			_, parseErr := parser.Parse(qCfg.Croniter)
			if parseErr != nil {
				log.Errorf("Query %v is not scheduled, because it has invalid croniter expression: %v ; %+v", qName, qCfg.Croniter, parseErr)
			} else {
				go executeQueryAndUpdateMetrics(qName)
				croniter.AddFunc(qCfg.Croniter, func() { executeQueryAndUpdateMetrics(qName) })
				log.Infof("Croniter %v registered for query %v", qCfg.Croniter, qName)
			}
		} else if qCfg.Interval != "" {
			scheduleWithInterval(qName, croniter, parser, qCfg.Interval, executeQueryAndUpdateMetrics)
		} else if len(*defaultInterval) > 0 {
			scheduleWithInterval(qName, croniter, parser, *defaultInterval, executeQueryAndUpdateMetrics)
		} else {
			log.Warnf("Query %v is not scheduled, because neither croniter nor interval are defined for it. Query will be executed on scrape event", qName)
			nonScheduledQueries = append(nonScheduledQueries, qName)
		}
	}
	return nonScheduledQueries
}

func scheduleWithInterval(qName string, croniter *cron.Cron, parser cron.Parser, interval string, executeQueryAndUpdateMetrics func(string)) {
	_, parseErr := parser.Parse("@every " + interval)
	if parseErr != nil {
		log.Errorf("Query %v is not scheduled, because it has invalid interval expression: %v ; %+v", qName, interval, parseErr)
	} else {
		go executeQueryAndUpdateMetrics(qName)
		croniter.AddFunc("@every "+interval, func() { executeQueryAndUpdateMetrics(qName) })
		log.Infof("Interval @every %v registered for query %v", interval, qName)
	}
}
