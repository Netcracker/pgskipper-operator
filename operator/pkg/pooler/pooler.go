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

package pooler

import (
	"context"
	"fmt"
	"sort"
	"strings"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	DeploymentName = "connection-puller"
	configMapName  = "pooler-config"
	secretName     = "pgbouncer-secret"
	configName     = "pgbouncer.ini"
	userList       = "userlist.txt"
)

var (
	labels = map[string]string{"app": "pg-bouncer"}
	logger = util.GetLogger()
)

type PgBouncerCreds struct {
	username string
	password string
}

func NewPoolerDeployment(cr *v1.PatroniServices, sa string) *appsv1.Deployment {
	spec := cr.Spec.Pooler
	deploymentName := DeploymentName
	dockerImage := spec.Image
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: util.GetNameSpace(),
			Labels:    util.Merge(labels, spec.PodLabels),
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: util.Merge(labels, spec.PodLabels),
			},
			Replicas: spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: util.Merge(labels, spec.PodLabels),
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "auth-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: secretName,
								},
							},
						},
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapName,
									},
								},
							},
						},
					},
					ServiceAccountName: sa,
					Affinity:           &spec.Affinity,
					InitContainers:     []corev1.Container{},
					Containers: []corev1.Container{
						{
							Name:            deploymentName,
							ImagePullPolicy: cr.Spec.ImagePullPolicy,
							Image:           dockerImage,
							Env:             []corev1.EnvVar{},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 6432, Name: "pg", Protocol: corev1.ProtocolTCP},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/pgbouncer/pgbouncer.ini",
									Name:      "config-volume",
									SubPath:   "pgbouncer.ini",
								},
								{
									MountPath: "/etc/pgbouncer/userlist.txt",
									Name:      "auth-volume",
									SubPath:   "userlist.txt",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.IntOrString{IntVal: 6432},
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								FailureThreshold:    20,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.IntOrString{IntVal: 6432},
									},
								},
								InitialDelaySeconds: 20,
								PeriodSeconds:       10,
								FailureThreshold:    20,
								TimeoutSeconds:      5,
								SuccessThreshold:    1,
							},
							Resources: spec.Resources,
						},
					},
					SecurityContext: spec.SecurityContext,
				},
			},
		},
	}
	return dep
}

func GetConfigMap(pooler v1.Pooler) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:              configMapName,
			Namespace:         util.GetNameSpace(),
			CreationTimestamp: metav1.Time{},
			Labels:            labels,
		},
		Data: createPoolerConfigMapData(pooler.Config),
	}
}

func createPoolerConfigMapData(inputData map[string]map[string]string) map[string]string {
	dataString := ""
	sections := sortMapKeys(inputData)
	for _, sectionKey := range sections {
		dataString = fmt.Sprintf("%s[%s]\n", dataString, sectionKey)
		sectionItems := inputData[sectionKey]
		keys := sortStringMapKeys(sectionItems)
		for _, paramKey := range keys {
			dataString = dataString + fmt.Sprintf("%s=%s\n", paramKey, sectionItems[paramKey])
		}
	}
	return map[string]string{configName: dataString}
}

func sortMapKeys(target map[string]map[string]string) []string {
	keylist := make([]string, 0, len(target))
	for k := range target {
		keylist = append(keylist, k)
	}
	sort.Strings(keylist)
	return keylist
}

func sortStringMapKeys(target map[string]string) []string {
	keylist := make([]string, 0, len(target))
	for k := range target {
		keylist = append(keylist, k)
	}
	sort.Strings(keylist)
	return keylist
}

func SetUpDatabase(creds *PgBouncerCreds, newPatroniName string) error {
	logger.Info("Database preparation for connection pooler started")
	client := pgClient.GetPostgresClientForHost(newPatroniName)
	if !helper.IsUserExist(creds.username, client) {
		logger.Info("Pooler user is not exist, creation...")
		if err := createPoolerUser(client, creds); err != nil {
			return err
		}
	}

	if err := createAuthFunctions(client, creds); err != nil {
		return err
	}
	return nil
}

func GetPgBouncerCreds() (*PgBouncerCreds, error) {
	foundSecret := &corev1.Secret{}
	k8sClient, err := util.GetClient()
	if err != nil {
		logger.Error("can't get k8sClient", zap.Error(err))
		return nil, err
	}
	err = k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: secretName, Namespace: util.GetNameSpace(),
	}, foundSecret)
	if err != nil {
		logger.Error(fmt.Sprintf("can't find the secret %s", secretName), zap.Error(err))
		return nil, err
	}
	data := foundSecret.Data[userList]
	// Should parse the creds from pgBouncer format
	dataStr := strings.ReplaceAll(string(data), "\"", "")
	parsCreds := strings.Split(dataStr, " ")
	return &PgBouncerCreds{
		username: parsCreds[0],
		password: parsCreds[1],
	}, nil
}

func createAuthFunctions(client *pgClient.PostgresClient, creds *PgBouncerCreds) error {
	conn, err := client.GetConnection()
	if err != nil {
		return err
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), "SELECT datname FROM pg_database;")
	if err != nil {
		logger.Error("cannot get database list", zap.Error(err))
		return err
	}
	defer rows.Close()

	databases := make([]string, 0)
	for rows.Next() {
		var db string
		if err = rows.Scan(&db); err != nil {
			logger.Error("cannot read database from databases list", zap.Error(err))
			return err
		}
		databases = append(databases, db)
	}
	for _, d := range databases {
		if d == "template0" {
			continue
		}
		if err = client.ExecuteForDB(d, "CREATE OR REPLACE FUNCTION public.lookup ("+
			"   INOUT p_user     name,"+
			"   OUT   p_password text"+
			") RETURNS record "+
			"LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS "+
			"$$SELECT usename, passwd FROM pg_shadow WHERE usename = p_user$$;"); err != nil {
			logger.Error(fmt.Sprintf("cannot create auth function for db %s", d), zap.Error(err))
			return err
		}
		if err = client.ExecuteForDB(d, "REVOKE EXECUTE ON FUNCTION public.lookup(name) FROM PUBLIC;"); err != nil {
			logger.Error(fmt.Sprintf("cannot create auth function for db %s", d), zap.Error(err))
			return err
		}
		username := pgClient.EscapeString(creds.username)
		if err = client.ExecuteForDB(d, fmt.Sprintf("GRANT EXECUTE ON FUNCTION public.lookup(name) TO \"%s\";", username)); err != nil {
			logger.Error(fmt.Sprintf("cannot create auth function for db %s", d), zap.Error(err))
			return err
		}
		if err = client.ExecuteForDB(d, fmt.Sprintf("ALTER FUNCTION public.lookup(name) OWNER TO %s;", client.GetUser())); err != nil {
			logger.Error(fmt.Sprintf("cannot create auth function for db %s", d), zap.Error(err))
			return err
		}
	}
	return nil
}

func createPoolerUser(client *pgClient.PostgresClient, creds *PgBouncerCreds) error {
	username := pgClient.EscapeString(creds.username)
	password := pgClient.EscapeString(creds.password)
	if err := client.ExecuteForDB(client.GetDatabase(), fmt.Sprintf("CREATE ROLE \"%s\" LOGIN PASSWORD '%s' ;", username, password)); err != nil {
		logger.Error("Cannot create Pooler user", zap.Error(err))
		return err
	}
	return nil
}

func UpdatePatroniService(hp *helper.Helper, postgresServiceName string) error {

	oldSrv := hp.GetService(postgresServiceName, util.GetNameSpace())
	oldSrv.Spec.Selector = labels
	oldSrv.Spec.Ports = []corev1.ServicePort{{
		Name:       "pg",
		Protocol:   corev1.ProtocolTCP,
		Port:       5432,
		TargetPort: intstr.IntOrString{IntVal: 6432},
	}}
	if err := hp.UpdateService(oldSrv); err != nil {
		return err
	}
	return nil
}
