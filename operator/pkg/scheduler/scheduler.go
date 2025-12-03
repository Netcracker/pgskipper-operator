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
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
	"time"
)

const (
	cronEnv  = "CRON_EXPR"
	cronExpr = "*/1 * * * *"
)

var (
	s          = gocron.NewScheduler(time.UTC)
	logger     = util.GetLogger()
	pgHost     string
	patroniUrl string
)

func scheduleIgnoreSlotsUpdate() {
	_, err := s.Cron(getCronExpr()).Do(updateIgnoredReplicationSlots)
	if err != nil {
		logger.Error("Error during scheduling cron job", zap.Error(err))
		panic(err)
	}
}

func StartScheduler(cr *qubershipv1.PatroniCore) {
	if !s.IsRunning() {
		logger.Info("Starting scheduler")
		initVariables(cr)
		scheduleIgnoreSlotsUpdate()
		s.StartAsync()
	}
}

func initVariables(cr *qubershipv1.PatroniCore) {
	pgClSettings := util.GetPatroniClusterSettings(cr.Spec.Patroni.ClusterName)
	pgHost = pgClSettings.PgHost
	patroniUrl = pgClSettings.PatroniUrl
	ignoreSlotsPrefix = cr.Spec.Patroni.IgnoreSlotsPrefix
}

func StopAndClear() {
	logger.Info("Stopping scheduler")
	s.Stop()
	s.Clear()
}

func getCronExpr() string {
	return util.GetEnv(cronEnv, cronExpr)
}
