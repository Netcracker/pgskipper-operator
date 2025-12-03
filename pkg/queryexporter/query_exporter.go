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

package queryexporter

import (
	"fmt"
	"strconv"
	"strings"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const CMName = "query-exporter-config"

var (
	logger              = util.GetLogger()
	queryExporterLabels = map[string]string{"name": "query-exporter", "app": "query-exporter"}
	preloadLibraries    = []string{"pg_stat_statements", "pgsentinel", "pg_stat_kcache", "pg_wait_sampling", "pgnodemx"}
	exporterExtensions  = []string{"pg_stat_statements", "pgsentinel", "pg_wait_sampling", "pg_buffercache", "pg_stat_kcache"}

	expSec = "query-exporter-user-credentials"
)

type QueryExporterCreds struct {
	username string
	password string
}

func NewQueryExporterDeployment(cr *v1.PatroniServices, sa string) *appsv1.Deployment {
	spec := cr.Spec.QueryExporter
	deploymentName := "query-exporter"
	dockerImage := spec.Image
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(queryExporterLabels, spec.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(queryExporterLabels, spec.PodLabels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(queryExporterLabels, spec.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes:            getVolumes(),
					ServiceAccountName: sa,
					Affinity:           &spec.Affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:    deploymentName,
							Image:   dockerImage,
							Command: []string{},
							Args:    []string{},
							Env:     getEnvVariables(spec),
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Name: "web", Protocol: corev1.ProtocolTCP},
							},
							VolumeMounts:    getVolumeMounts(),
							Resources:       spec.Resources,
							ImagePullPolicy: cr.Spec.ImagePullPolicy,
						},
					},
					SecurityContext: spec.SecurityContext,
				},
			},
		},
	}
	return dep
}

func getVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: CMName},
				},
			},
		},
	}
}

func getVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			MountPath: "/config",
			Name:      "config-volume",
		},
	}
}

func getEnvVariables(spec v1.QueryExporter) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "POSTGRES_HOST",
			Value: spec.PgHost,
		},
		{
			Name:  "POSTGRES_PORT",
			Value: strconv.Itoa(spec.PgPort),
		},
		{
			Name:  "AUTODISCOVERY",
			Value: "true",
		},
		{
			Name:  "QUERY_EXPORTER_EXTEND_QUERY_PATH",
			Value: "/config/config.yaml",
		},
		{
			Name:  "MAX_OPEN_CONNECTIONS_MASTER",
			Value: strconv.Itoa(spec.MaxMasterConnections),
		},
		{
			Name:  "MAX_OPEN_CONNECTIONS_LOGICAL",
			Value: strconv.Itoa(spec.MaxLogicalConnections),
		},
		{
			Name:  "MAX_QUERY_TIMEOUT",
			Value: strconv.Itoa(spec.QueryTimeout),
		},
		{
			Name:  "QUERY_EXPORTER_DISABLE_SELF_MONITOR",
			Value: strconv.FormatBool(spec.SelfMonitorDisabled),
		},
		{
			Name:      "POSTGRES_USER",
			ValueFrom: getSecretFieldEnv("username"),
		},
		{
			Name:      "POSTGRES_PASSWORD",
			ValueFrom: getSecretFieldEnv("password"),
		},
		{
			Name:  "EXCLUDED_QUERIES",
			Value: strings.Join(spec.ExcludeQueries, ","),
		},
		{
			Name:  "COLLECTION_INTERVAL",
			Value: spec.CollectionInterval,
		},
		{
			Name:  "MAX_FAILED_TIMEOUTS",
			Value: spec.MaxFailedTimeouts,
		},
	}
}

func getSecretFieldEnv(fieldName string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: expSec},
			Key:                  fieldName,
		},
	}
}

func UpdatePreloadLibraries(cr *patroniv1.PatroniCore) {
	helper.UpdatePreloadLibraries(cr, preloadLibraries)
}

func GetService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "query-exporter",
			Namespace: util.GetNameSpace(),
			Labels:    queryExporterLabels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "web",
				Protocol:   corev1.ProtocolTCP,
				Port:       8080,
				TargetPort: intstr.IntOrString{IntVal: 8080},
			}},
			Selector: queryExporterLabels,
		},
		Status: corev1.ServiceStatus{},
	}
}

func CreateQueryExporterExtensions(pgHost, pgDatabase string) {
	client := pgClient.GetPostgresClient(pgHost)
	helper.CreateExtensionsForDB(client, pgDatabase, exporterExtensions)
}

func EnsureQueryExporterUser(pgHost string) error {
	creds, err := getExporterCreds()
	if err != nil {
		return err
	}
	client := pgClient.GetPostgresClient(pgHost)
	if helper.IsUserExist(creds.username, client) {
		if err = helper.AlterUserPassword(client, creds.username, creds.password); err != nil {
			return err
		}
	} else {
		if err = createExporterUser(creds, client); err != nil {
			return err
		}
	}
	if err = grantExporterUser(creds, client); err != nil {
		return err
	}
	return nil
}

func createExporterUser(creds QueryExporterCreds, client *pgClient.PostgresClient) error {
	logger.Info(fmt.Sprintf("Creation of \"%s\" user for query-exporter", creds.username))
	if err := client.Execute(fmt.Sprintf("CREATE ROLE \"%s\" LOGIN PASSWORD '%s';", creds.username, creds.password)); err != nil {
		logger.Error("cannot create Query Exporter user", zap.Error(err))
		return err
	}
	return nil
}

func grantExporterUser(creds QueryExporterCreds, pg *pgClient.PostgresClient) error {
	if err := pg.Execute(fmt.Sprintf("GRANT pg_read_all_data, pg_monitor TO \"%s\";", creds.username)); err != nil {
		logger.Error("cannot modify Query Exporter user", zap.Error(err))
		return err
	}
	return nil
}

func getExporterCreds() (QueryExporterCreds, error) {
	foundSecret, err := helper.GetHelper().GetSecret(expSec)
	if err != nil {
		return QueryExporterCreds{}, err
	}
	username := pgClient.EscapeString(string(foundSecret.Data["username"]))
	password := pgClient.EscapeString(string(foundSecret.Data["password"]))
	return QueryExporterCreds{username: username, password: password}, nil
}
