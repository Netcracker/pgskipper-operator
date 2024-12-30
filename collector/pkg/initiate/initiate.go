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

package initiate

import (
	"context"
	b64 "encoding/base64"
	"fmt"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/postgres"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	logger = util.GetLogger()
	ctx    = context.Background()
)

func InitMetricCollector() {

	logger.Info("Will run preparation scripts")

	clusterName := util.GetEnv("PGCLUSTER", "patroni")
	monitoringRole := util.GetEnv("MONITORING_USER", "monitoring_role")
	monitoringPassword := util.GetEnv("MONITORING_PASSWORD", "monitoring_password")
	pgHost := util.GetEnv("POSTGRES_HOST", "pg-patroni")
	pgPort := util.GetEnvInt("POSTGRES_PORT", 5432)

	queries := append(make([]string, 0),
		"DROP TABLE if exists monitor_test",
		fmt.Sprintf("CREATE ROLE \"%s\" with login password '%s' ", monitoringRole, monitoringPassword),
		fmt.Sprintf("ALTER ROLE \"%s\" with login password '%s'", monitoringRole, monitoringPassword),
		fmt.Sprintf("GRANT pg_monitor to \"%s\"", monitoringRole),
		fmt.Sprintf("GRANT pg_read_all_data to \"%s\"", monitoringRole),
		"CREATE EXTENSION if not exists pg_stat_statements",
		fmt.Sprintf("GRANT CREATE ON SCHEMA public TO \"%s\"", monitoringRole),
		"DROP SEQUENCE if exists monitor_test_seq")

	securedViews := append(make([]string, 0),
		"pg_stat_replication", "pg_stat_statements", "pg_database", "pg_stat_activity",
	)

	user, password := getPGCredentials(clusterName)
	pc := postgres.NewConnectorForUser(pgHost, pgPort, user, password)
	err := pc.EstablishConn(ctx)
	if err != nil {
		panic("Error while init metric collector. Can not connect to postgres database")
	}
	defer pc.CloseConnection(ctx)
	for _, query := range queries {
		if _, err := pc.Exec(ctx, query); err != nil {
			logger.Error("Error while init metric collector. Can not execute query", zap.Error(err))
		}
	}
	for _, view := range securedViews {
		query := fmt.Sprintf("DROP FUNCTION IF EXISTS func_%s()", view)
		if _, err := pc.Exec(ctx, query); err != nil {
			logger.Error(fmt.Sprintf("Error while init metric collector. Can not execute query: %s", query), zap.Error(err))
		}
	}
	logger.Info("metric collector init completed")

}

func getPGCredentials(clusterName string) (user, password string) {

	namespace := util.GetEnv("NAMESPACE", "postgres-service")
	user = util.GetEnv("PG_ROOT_USER", "")
	password = util.GetEnv("PG_ROOT_PASSWORD", "")

	if user != "" || password != "" {
		return user, password
	}

	k8sClient, err := util.CreateClient()
	if err != nil {
		panic("Error while init metric collector. Can not create k8s client")
	}

	secretName := util.GetEnv("NAMESPACE", fmt.Sprintf("%s-pg-root-credentials", clusterName))
	foundSrv := &corev1.Secret{}
	err = k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: secretName, Namespace: namespace,
	}, foundSrv)
	if err != nil {
		panic(fmt.Sprintf("Error while init metric collector. Can not read secret: %s", secretName))
	}

	pwd := foundSrv.Data["password"]
	usr, ok := foundSrv.Data["user"]
	if !ok {
		usr = foundSrv.Data["username"]
	}
	pwdEnc := b64.StdEncoding.EncodeToString(pwd)
	usrEnc := b64.StdEncoding.EncodeToString(usr)

	return usrEnc, pwdEnc
}
